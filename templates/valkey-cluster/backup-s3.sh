#!/bin/bash
# valkey-cluster backup-s3 — BGSAVE on every primary, copy each shard's RDB and
# upload to S3 as s3://$S3_BUCKET/$S3_PREFIX/<date>/<pod>.rdb + manifest.txt.
# RDB is a point-in-time snapshot per shard (AOF stays local for crash safety).
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
RETENTION="${RETENTION:-7}"
TS="$(date +%F-%H%M%S)"
STAGE="$(mktemp -d /tmp/vk-backup-XXXXXX)"

ensure_s3cfg() {
  [ -f "$HOME/.s3cfg" ] && return 0
  [ -n "${S3_ENDPOINT:-}" ] || { echo "ERROR: S3_ENDPOINT not set"; exit 1; }
  local ES3; ES3="$(echo "$S3_ENDPOINT" | sed 's#https\?://##; s#/*$##')"
  cat > "$HOME/.s3cfg" <<EOF
[default]
access_key = $S3_ACCESS_KEY
secret_key = $S3_SECRET_KEY
host_base = $ES3
host_bucket = %(bucket)s.$ES3
use_https = True
EOF
  chmod 600 "$HOME/.s3cfg"
}

POD() { $KUBECTL -n "$NS" exec "$1" -c valkey -- valkey-cli -a "$PASSWORD" --no-auth-warning "${@:2}" 2>/dev/null; }

echo "=== valkey-cluster backup-s3 ==="
ensure_s3cfg

# 1. Map pod IP -> pod name and identify the primaries.
ipmap="$($KUBECTL -n "$NS" get pods -l app=valkey -o jsonpath='{range .items[*]}{.status.podIP}={.metadata.name}{"\n"}{end}' 2>/dev/null)"
masters=""
for ip in $(POD valkey-0 cluster nodes | awk '$3 ~ /master/ {split($2,a,"@"); split(a[1],b,":"); print b[1]}'); do
  pod="$(echo "$ipmap" | awk -F= -v ip="$ip" '$1==ip {print $2}')"
  [ -n "$pod" ] && masters="$masters $pod"
done
[ -n "$masters" ] || { echo "ERROR: no primaries found"; exit 1; }
echo "  primaries:$masters"

# 2. BGSAVE on every primary and wait for the RDB.
echo "--- BGSAVE ---"
for pod in $masters; do
  LAST="$(POD "$pod" LASTSAVE)"
  POD "$pod" BGSAVE >/dev/null
  deadline=$((SECONDS + 120))
  while [ "$SECONDS" -lt "$deadline" ]; do
    now="$(POD "$pod" LASTSAVE)"
    status="$(POD "$pod" INFO persistence | grep -oE 'rdb_bgsave_in_progress:[01]' | cut -d: -f2)"
    if [ "${status:-1}" = "0" ] && [ "${now:-0}" -gt "${LAST:-0}" ]; then break; fi
    sleep 2
  done
  [ "${status:-1}" = "0" ] || { echo "ERROR: BGSAVE on $pod did not finish"; exit 1; }
  echo "  [$pod] BGSAVE ok"
done

# 3. Copy each shard's RDB to the host.
echo "--- Copy RDBs ---"
for pod in $masters; do
  $KUBECTL -n "$NS" cp "$NS/$pod:/data/dump.rdb" "$STAGE/$pod.rdb" -c valkey >/dev/null 2>&1 || { echo "ERROR: kubectl cp $pod"; exit 1; }
  echo "  $pod.rdb ($(du -h "$STAGE/$pod.rdb" | cut -f1))"
done

# 4. Manifest: raw cluster layout + pod/slot map. The restore uses the map to
# find which pod currently owns each RDB's slots (survives failovers between
# the backup and the restore).
CN="$(POD valkey-0 cluster nodes)"
{
  echo "# valkey-cluster backup $TS"
  echo "# primaries:$masters"
  echo "# map: pod ip first-slot"
  for pod in $masters; do
    ip="$(echo "$ipmap" | awk -F= -v p="$pod" '$2==p {print $1}')"
    tok="$(echo "$CN" | awk -v ip="$ip" '$2 ~ "^"ip":" {print $9}')"
    echo "$pod $ip $tok"
  done
  echo ""
  echo "$CN"
} > "$STAGE/manifest.txt"

# 5. Upload.
echo "--- Upload to S3 ---"
for pod in $masters; do
  s3cmd put --quiet "$STAGE/$pod.rdb" "s3://$S3_BUCKET/$S3_PREFIX/$TS/$pod.rdb" 2>&1
done
s3cmd put --quiet "$STAGE/manifest.txt" "s3://$S3_BUCKET/$S3_PREFIX/$TS/manifest.txt" 2>&1
echo "  uploaded: s3://$S3_BUCKET/$S3_PREFIX/$TS/"

# 6. Retention: keep the last $RETENTION dump sets.
echo "--- Retention (keep last $RETENTION) ---"
dirs="$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | awk '/ DIR /{print $2}' | sort -r)"
count=$(echo "$dirs" | grep -c . || true)
if [ "$count" -gt "$RETENTION" ]; then
  echo "$dirs" | tail -n +$((RETENTION + 1)) | while read -r d; do
    [ -n "$d" ] && echo "  removing: $d" && s3cmd del --recursive --quiet "$d" 2>/dev/null || true
  done
fi

rm -rf "$STAGE"
echo ""
echo "=== backup-s3 complete ==="
echo "  s3://$S3_BUCKET/$S3_PREFIX/$TS/"
