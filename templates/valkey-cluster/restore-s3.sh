#!/bin/bash
# valkey-cluster restore-s3 — whole-cluster recovery from per-shard S3 dumps.
#
# A cluster-native failover would promote a replica as soon as a primary goes
# away, so restarting primaries ONE BY ONE would just resync them from their
# (empty) promoted replicas. Instead this performs a controlled full-cluster
# restore:
#   1. map each RDB (backup pod name) to the pod that CURRENTLY owns its slots
#      (via the slot token recorded in manifest.txt — survives failovers
#      between the backup and the restore),
#   2. scale the StatefulSet to 0,
#   3. stage each RDB into the target PVC and wipe the stale AOFs (helper pods),
#   4. scale back up: every primary boots from its snapshot and the replicas
#      full-sync from them.
# Snapshot semantics: keys written after the BGSAVE are gone.
#
# Usage: restore-s3.sh [--yes] [YYYY-MM-DD-HHMMSS]
set -u

NS="${VK_K8S_NAMESPACE:-valkey}"
NAME="${VK_K8S_NAME:-valkey}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/valkey-cluster"
[ -f "$DIR/.env" ] && . "$DIR/.env"
PASSWORD="${VK_PASSWORD:-valkey}"

S3_BUCKET="${S3_BUCKET:-valkey-backups}"
S3_PREFIX="${S3_PREFIX:-valkey}"
RESTORE_DIR=""
YES=false
STAGE="$(mktemp -d /tmp/vk-restore-XXXXXX)"

usage() {
  echo "Usage: restore-s3.sh [--yes] [YYYY-MM-DD-HHMMSS]"
  echo "Restores the latest dump set when no date is given."
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes|-y) YES=true; shift ;;
    --help|-h) usage ;;
    *) RESTORE_DIR="$1"; shift ;;
  esac
done

ensure_s3cfg() {
  [ -f "$HOME/.s3cfg" ] && return 0
  [ -n "${S3_ENDPOINT:-}" ] || { echo "ERROR: S3_ENDPOINT not set"; exit 1; }
  cat > "$HOME/.s3cfg" <<EOF
[default]
access_key = $S3_ACCESS_KEY
secret_key = $S3_SECRET_KEY
host_base = $S3_ENDPOINT
host_bucket = %(bucket)s.$S3_ENDPOINT
use_https = True
EOF
  chmod 600 "$HOME/.s3cfg"
}

POD() { $KUBECTL -n "$NS" exec "$1" -c valkey -- valkey-cli -a "$PASSWORD" --no-auth-warning "${@:2}" 2>/dev/null; }

# stage_in <pod> [file] — helper pod on data-<pod>: optionally write the RDB via
# kubectl cp, then drop the AOF (and the RDB when wiping). Deterministic (no
# stdin attach race), verified by checksum.
stage_in() {
  pod="$1"; shift
  file=""
  [ $# -gt 0 ] && file="$1"
  helper="vk-stage-$pod"
  override="{\"spec\":{\"containers\":[{\"name\":\"stage\",\"image\":\"busybox:1.36\",\"command\":[\"sleep\",\"3600\"],\"volumeMounts\":[{\"name\":\"data\",\"mountPath\":\"/data\"}]}],\"volumes\":[{\"name\":\"data\",\"persistentVolumeClaim\":{\"claimName\":\"data-$pod\"}}]}}"
  $KUBECTL -n "$NS" delete pod "$helper" --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
  $KUBECTL -n "$NS" run "$helper" --restart=Never --image=busybox:1.36 --overrides="$override" >/dev/null 2>&1 || true
  $KUBECTL -n "$NS" wait --for=condition=Ready "pod/$helper" --timeout=180s >/dev/null 2>&1 || { echo "    $helper did not start"; return 1; }

  if [ -n "$file" ]; then
    $KUBECTL -n "$NS" cp "$file" "$NS/$helper:/data/dump.rdb" >/dev/null 2>&1 || { echo "    cp -> $helper failed"; $KUBECTL -n "$NS" delete pod "$helper" --force --grace-period=0 >/dev/null 2>&1; return 1; }
    $KUBECTL -n "$NS" exec "$helper" -- rm -rf /data/appendonlydir /data/appendonly.aof >/dev/null 2>&1
    want="$(md5sum "$file" | awk '{print $1}')"
    got="$($KUBECTL -n "$NS" exec "$helper" -- md5sum /data/dump.rdb 2>/dev/null | awk '/^[0-9a-f]{32} /{print $1; exit}')"
    okr=0; [ "$got" = "$want" ] && okr=1
  else
    $KUBECTL -n "$NS" exec "$helper" -- rm -rf /data/appendonlydir /data/appendonly.aof /data/dump.rdb >/dev/null 2>&1
    got="$($KUBECTL -n "$NS" exec "$helper" -- ls /data/dump.rdb 2>/dev/null | wc -l | tr -d ' ')"
    okr=0; [ "$got" = "0" ] && okr=1
  fi
  $KUBECTL -n "$NS" delete pod "$helper" --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
  [ "$okr" = "1" ]
}

echo "=== valkey-cluster restore-s3 ==="
ensure_s3cfg

# 1. Pick the dump set and download it (RDBs + manifest).
if [ -z "$RESTORE_DIR" ]; then
  RESTORE_DIR="$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | awk '/ DIR /{print $2}' | sort -r | head -1 | sed 's:/*$::' | xargs -r basename)"
  [ -n "$RESTORE_DIR" ] || { echo "ERROR: no dump sets in s3://$S3_BUCKET/$S3_PREFIX/"; exit 1; }
fi
echo "  dump set: $RESTORE_DIR"

keys="$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/$RESTORE_DIR/" 2>/dev/null | awk '{print $4}' || true)"
rdbs="$(echo "$keys" | grep '\.rdb$' || true)"
[ -n "$rdbs" ] || { echo "ERROR: no .rdb files in s3://$S3_BUCKET/$S3_PREFIX/$RESTORE_DIR/"; exit 1; }
echo "--- Download ---"
for key in $keys; do
  f="$(basename "$key")"
  s3cmd get --force --quiet "$key" "$STAGE/$f" 2>&1
done
ls "$STAGE" | sed 's/^/  /'

manifest="$STAGE/manifest.txt"
[ -f "$manifest" ] || { echo "ERROR: manifest.txt missing from the dump set"; exit 1; }

# 2. Map: backup pod -> slot token; live slot token -> current owner pod.
# Owners are resolved by the announced HOSTNAME (stable across restarts — the
# ips churn on every pod recreation).
declare -A back_tok live_pod
have_map=0
while read -r pod _ip tok; do
  case "$pod" in vk-*|valkey-*) back_tok["$pod"]="$tok"; have_map=1 ;; esac
done < <(grep -E '^[a-z0-9-]+ ' "$manifest")
[ "$have_map" = "1" ] || { echo "ERROR: manifest has no pod/slot map (old-format backup)"; exit 1; }

live="$(POD valkey-0 cluster nodes | awk '$3 ~ /master/ {print $9, $2}')"
[ -n "$live" ] || { echo "ERROR: cluster is not answering CLUSTER NODES"; exit 1; }
while read -r tok ep; do
  host="${ep#*,}"
  [ "$host" = "$ep" ] && continue
  live_pod["$tok"]="${host%%.*}"
done <<< "$live"

# 3. Resolve every RDB to its current shard owner.
stage_list=""
for f in "$STAGE"/*.rdb; do
  [ -e "$f" ] || continue
  bpod="$(basename "$f" .rdb)"
  tok="${back_tok[$bpod]:-}"
  target="${live_pod[$tok]:-}"
  if [ -z "$target" ]; then
    echo "ERROR: cannot map $bpod.rdb (slot ${tok:-?}) to a current primary — aborting"
    exit 1
  fi
  echo "  [$bpod.rdb] -> $target (slots $tok)"
  stage_list="$stage_list $target=$f"
done

if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: FULL cluster downtime. Every shard is replaced with the $RESTORE_DIR snapshot."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

# 4. Full-cluster restore.
replicas="$($KUBECTL -n "$NS" get sts valkey -o jsonpath='{.spec.replicas}' 2>/dev/null)"
[ -n "$replicas" ] || { echo "ERROR: statefulset valkey not found"; exit 1; }

echo "--- Scale down ($replicas pods) ---"
$KUBECTL -n "$NS" scale statefulset valkey --replicas=0 >/dev/null 2>&1
$KUBECTL -n "$NS" wait --for=delete pod -l app=valkey --timeout=180s >/dev/null 2>&1

echo "--- Stage snapshots ---"
for kv in $stage_list; do
  target="${kv%%=*}"; file="${kv#*=}"
  stage_in "$target" "$file" && echo "  [OK] $target <- $(basename "$file")" || { echo "  [FAIL] stage $target"; exit 1; }
done
# Replicas: wipe so they full-sync cleanly from their restored primary.
for i in $(seq 0 $((replicas - 1))); do
  pod="valkey-$i"
  case " $stage_list " in
    *" ${pod}="*) continue ;;
  esac
  stage_in "$pod" && echo "  [OK] $pod wiped (replica, will full-sync)" || echo "  [WARN] wipe $pod failed"
done

echo "--- Scale up ---"
$KUBECTL -n "$NS" scale statefulset valkey --replicas="$replicas" >/dev/null 2>&1

# Full restarts churn pod IPs: re-introduce the nodes by their current IP so
# the gossip merges the fresh endpoints (the announced hostnames stay stable).
echo "--- Re-merge cluster ---"
rdeadline=$((SECONDS + 300)); ready=0
while [ "$SECONDS" -lt "$rdeadline" ]; do
  ready="$($KUBECTL -n "$NS" get pods -l app=valkey --no-headers 2>/dev/null | awk '$2 == "1/1"' | wc -l | tr -d ' ')"
  [ "${ready:-0}" -ge "$replicas" ] && break
  sleep 5
done
ipmap="$($KUBECTL -n "$NS" get pods -l app=valkey -o jsonpath='{range .items[*]}{.status.podIP}{"\n"}{end}' 2>/dev/null)"
for ip in $ipmap; do
  POD valkey-0 cluster meet "$ip" 6379 >/dev/null 2>&1 || true
done

deadline=$((SECONDS + 600))
state=""; slots=0
while [ "$SECONDS" -lt "$deadline" ]; do
  ready="$($KUBECTL -n "$NS" get pods -l app=valkey --no-headers 2>/dev/null | awk '$2 == "1/1"' | wc -l | tr -d ' ')"
  state="$(POD valkey-0 cluster info | grep -oE 'cluster_state:[a-z]+' | cut -d: -f2)"
  slots="$(POD valkey-0 cluster info | grep -oE 'cluster_slots_assigned:[0-9]+' | cut -d: -f2)"
  [ "${ready:-0}" -ge "$replicas" ] && [ "$state" = "ok" ] && [ "${slots:-0}" = "16384" ] && break
  sleep 5
done

rm -rf "$STAGE"
echo "--- Per-shard DBSIZE ---"
for i in $(seq 0 $((replicas - 1))); do
  role="$(POD "valkey-$i" INFO replication | grep -oE 'role:(master|slave)' | cut -d: -f2)"
  [ "$role" = "master" ] && echo "  valkey-$i (master) DBSIZE=$(POD "valkey-$i" DBSIZE)"
done

echo ""
if [ "$state" = "ok" ] && [ "${slots:-0}" = "16384" ]; then
  echo "=== restore-s3 complete (state ok, 16384 slots) ==="
else
  echo "=== restore-s3 INCOMPLETE (state=${state:-?} slots=${slots:-0}) ==="
  exit 1
fi
echo "  verify: bash validate.sh && bash test/test.sh"
