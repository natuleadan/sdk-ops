#!/bin/bash
# df-cluster backup-s3 — native Dragonfly snapshot: SAVE on the master writes
# dump-*.dfs straight to the CR's snapshot dir (s3://$S3_BUCKET/$S3_PREFIX/).
# This script only triggers + verifies the artifact and prunes old snapshots.
# Usage: backup-s3.sh
set -u

NS="${DF_K8S_NAMESPACE:-df}"
NAME="${DF_K8S_NAME:-df}"
DIR="/opt/sdk-ops/services/df-cluster"
[ -f "$DIR/.env" ] && . "$DIR/.env"
PASS="${DF_PASSWORD:-dragonfly}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
: "${S3_BUCKET:?}" : "${S3_ENDPOINT:?}" : "${S3_ACCESS_KEY:?}" : "${S3_SECRET_KEY:?}"
: "${S3_PREFIX:=df}"
RETENTION="${RETENTION:-7}"

fail() { echo "[df-backup] FAIL: $1"; exit 1; }

S3DIR="s3://$S3_BUCKET/$S3_PREFIX/"

ensure_s3cfg() {
  [ -f "$HOME/.s3cfg" ] && return 0
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

ensure_s3cfg

master="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly,role=master" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
[ -n "$master" ] || master="$NAME-0"

before="$(s3cmd ls "$S3DIR" 2>/dev/null | wc -l | tr -d ' ')"
echo "[df-backup] master=$master — SAVE (native snapshot -> $S3DIR)"
$KUBECTL -n "$NS" exec "$master" -- redis-cli -p 6379 -a "$PASS" --no-auth-warning SAVE >/dev/null 2>&1 || fail "SAVE on $master"

deadline=$((SECONDS + 180))
latest=""
while [ "$SECONDS" -lt "$deadline" ]; do
  latest="$(s3cmd ls "$S3DIR" 2>/dev/null | awk '{print $NF}' | grep -v summary | grep '\.dfs$' | sort | tail -1)"
  after="$(s3cmd ls "$S3DIR" 2>/dev/null | wc -l | tr -d ' ')"
  if [ -n "$latest" ] && [ "${after:-0}" -gt "${before:-0}" ] \
     && s3cmd info "${latest%-0000.dfs}-summary.dfs" >/dev/null 2>&1; then break; fi
  latest=""
  sleep 5
done
[ -n "$latest" ] && [ "${after:-0}" -gt "${before:-0}" ] || fail "no new snapshot in $S3DIR"
echo "[df-backup] snapshot OK: $latest"

# Retention: keep the last $RETENTION dump sets (dump + summary pairs).
dumps="$(s3cmd ls "$S3DIR" 2>/dev/null | awk '{print $NF}' | grep -v summary | grep '\.dfs$' | sort -r)"
count="$(echo "$dumps" | grep -c . || true)"
if [ "$count" -gt "$RETENTION" ]; then
  echo "$dumps" | tail -n +$((RETENTION + 1)) | while read -r d; do
    [ -n "$d" ] || continue
    echo "[df-backup] prune: $d"
    s3cmd del "$d" >/dev/null 2>&1 || true
    s3cmd del "${d%-0000.dfs}-summary.dfs" >/dev/null 2>&1 || true
  done
fi

echo "[df-backup] done — $S3DIR"
