#!/bin/bash
# df-cluster restore-s3 — roll the S3 snapshot store back to a chosen dump and
# cycle the cluster so the nodes boot from it.
#
# Dragonfly writes a snapshot on every shutdown (SIGTERM), so simply deleting
# the pods would leave junk snapshots newer than the one you want, and the
# booting nodes always load the NEWEST one. The clean procedure is:
#   1. scale the StatefulSet to 0 (the shutdown saves land while it is down),
#   2. delete every dump newer than the target (+ its summary),
#   3. scale back up — the boot loads the target.
#
# Usage: restore-s3.sh [-y|--yes] [dump name]
set -u

NS="${DF_K8S_NAMESPACE:-df}"
NAME="${DF_K8S_NAME:-df}"
DIR="/opt/sdk-ops/services/df-cluster"
[ -f "$DIR/.env" ] && . "$DIR/.env"
PASS="${DF_PASSWORD:-dragonfly}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
: "${S3_BUCKET:?}" : "${S3_ENDPOINT:-}" : "${S3_ACCESS_KEY:-}" : "${S3_SECRET_KEY:?}"
: "${S3_PREFIX:=df}"
FORCE=0
SEL=""

usage() {
  echo "Usage: restore-s3.sh [-y|--yes] [dump name]"
  echo "Rolls the cluster back to the most recent snapshot unless a name is given."
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    -y|--yes) FORCE=1; shift ;;
    --help|-h) usage ;;
    *) SEL="$1"; shift ;;
  esac
done

fail() { echo "[df-restore] FAIL: $1"; exit 1; }
S3DIR="s3://$S3_BUCKET/$S3_PREFIX/"

[ -f "$HOME/.s3cfg" ] || { echo "ERROR: ~/.s3cfg missing (run backup first)"; exit 1; }

# 1. Pick the target snapshot.
if [ -z "$SEL" ]; then
  SEL="$(s3cmd ls "$S3DIR" 2>/dev/null | awk '{print $NF}' | grep '\.dfs$' | grep -v summary | sort | tail -1)"
  [ -n "$SEL" ] || fail "no snapshots in $S3DIR"
else
  case "$SEL" in
    s3://*) ;;
    *) SEL="$S3DIR$SEL" ;;
  esac
  s3cmd info "$SEL" >/dev/null 2>&1 || fail "snapshot not found: $SEL"
fi
echo "[df-restore] target snapshot: $SEL"

if [ "$FORCE" -eq 0 ]; then
  echo ""
  echo "WARNING: the cluster goes down and every snapshot NEWER than the target is deleted."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

replicas="$($KUBECTL -n "$NS" get dragonfly "$NAME" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo 3)"
[ -n "$replicas" ] || replicas=3

# 2. Down (shutdown saves land while there are no pods running).
echo "[df-restore] scaling down ($replicas -> 0)"
$KUBECTL -n "$NS" scale statefulset "$NAME" --replicas=0 >/dev/null 2>&1 || fail "scale down"
d=$((SECONDS + 300)); left=1
while [ "$SECONDS" -lt "$d" ]; do
  left="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly" --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  [ "${left:-1}" = "0" ] && break
  sleep 5
done
[ "${left:-1}" = "0" ] || fail "pods still running after scale down"

# 3. Drop every dump newer than the target (and its summary pair). Summaries are
# excluded from the comparison: `X-summary.dfs` sorts AFTER `X-0000.dfs`, so a
# naive filter would delete the target's OWN summary and the boot (which loads
# the newest summary) would fall back to an older, stale dump.
newer="$(s3cmd ls "$S3DIR" 2>/dev/null | awk '{print $NF}' | grep -v summary | grep '\.dfs$' | awk -v t="$SEL" '$0 > t' || true)"
for d in $newer; do
  echo "[df-restore] delete newer: $(basename "$d")"
  s3cmd del "$d" >/dev/null 2>&1 || true
  s3cmd del "${d%-0000.dfs}-summary.dfs" >/dev/null 2>&1 || true
done

# 4. Up — the boot loads the target (now the newest dump).
echo "[df-restore] scaling up (0 -> $replicas)"
$KUBECTL -n "$NS" scale statefulset "$NAME" --replicas="$replicas" >/dev/null 2>&1 || fail "scale up"

d=$((SECONDS + 600)); ready=0
while [ "$SECONDS" -lt "$d" ]; do
  ready="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly" --no-headers 2>/dev/null | awk '$2 == "1/1"' | wc -l | tr -d ' ')"
  [ "${ready:-0}" -ge "$replicas" ] && break
  sleep 5
done
[ "${ready:-0}" -ge "$replicas" ] || fail "pods not ready ($ready/$replicas)"

# 5. Report.
d=$((SECONDS + 180)); master=""
while [ "$SECONDS" -lt "$d" ]; do
  master="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly,role=master" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
  [ -n "$master" ] && break
  sleep 5
done
[ -n "$master" ] || master="$NAME-0"
size="$($KUBECTL -n "$NS" exec "$master" -- redis-cli -p 6379 -a "$PASS" --no-auth-warning DBSIZE 2>/dev/null | tr -d '[:space:]')"
echo "[df-restore] master=$master DBSIZE=${size:-?}"
echo "[df-restore] done — verify with validate.sh / test/test.sh"
