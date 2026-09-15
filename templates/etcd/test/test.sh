#!/bin/bash
# etcd-dockerized integration test — quorum, consensus, failover, S3 snapshot
# Run ON one member node. For the full 3-member cluster set ETCD_PEERS:
#   ETCD_PEERS="http://198.51.100.2:2379,http://198.51.100.3:2379,http://198.51.100.4:2379" bash test/test.sh
# Without ETCD_PEERS the test runs in single-member mode (cluster steps [SKIP]).
set -u

ETCD_IMAGE="quay.io/coreos/etcd:v3.5.15"
ETCD_PEERS="${ETCD_PEERS:-}"
S3_BUCKET="${S3_BUCKET:-etcd-backups}"
S3_PREFIX="${S3_PREFIX:-etcd}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
SNAP="/tmp/etcd-test-snap-$(date +%s).db"
FAIL=0

ok()   { echo "  [OK] $*"; }
bad()  { echo "  [FAIL] $*"; FAIL=1; }
skip() { echo "  [SKIP] $*"; }

# Resolve the local etcd container dynamically (never hardcode suffixes).
LOCAL_CONTAINER=$(docker ps --format '{{"{{"}}.Names{{"}}"}}' 2>/dev/null | grep -E 'etcd' | head -1)
ECTL_LOCAL() { docker exec "$LOCAL_CONTAINER" etcdctl --endpoints=127.0.0.1:2379 --command-timeout=6s "$@"; }
# Cross-host etcdctl via a throwaway container on the host network.
ECTL_REMOTE() { docker run --rm --network host "$ETCD_IMAGE" etcdctl --command-timeout=6s "$@"; }

echo "=== etcd integration test ==="
echo "  ETCD_PEERS: ${ETCD_PEERS:-<unset — single-member mode>}"

if [ -z "$LOCAL_CONTAINER" ]; then
  echo "  [FAIL] no local etcd container found"
  exit 1
fi
echo "  Local container: $LOCAL_CONTAINER"

# Auto-derive the peer list from the member list when ETCD_PEERS is unset
# (single-member etcd derives one endpoint and stays in single-member mode).
if [ -z "$ETCD_PEERS" ]; then
  ETCD_PEERS="$(docker exec "$LOCAL_CONTAINER" etcdctl member list -w json 2>/dev/null | grep -oE 'http://(\[[0-9a-fA-F:]+\]|[0-9.]+):2379' | sort -u | tr '\n' ',' | sed 's/,$//')"
  [ -n "$ETCD_PEERS" ] && echo "  ETCD_PEERS (auto): $ETCD_PEERS"
fi

echo "--- Step 1: Endpoint health ---"
if [ -n "$ETCD_PEERS" ]; then
  HEALTHY=0
  TOTAL=0
  IFS=','
  for ep in $ETCD_PEERS; do
    IFS=' '
    TOTAL=$((TOTAL + 1))
    if ECTL_REMOTE --endpoints="$ep" endpoint health 2>/dev/null | grep -q healthy; then
      HEALTHY=$((HEALTHY + 1))
    fi
  done
  unset IFS
  echo "  Healthy: $HEALTHY/$TOTAL"
  if [ "$HEALTHY" -ge 2 ] && [ "$TOTAL" -ge 3 ]; then
    ok "quorum healthy ($HEALTHY/$TOTAL)"
  elif [ "$HEALTHY" -eq "$TOTAL" ] && [ "$TOTAL" -eq 1 ]; then
    ok "single-member healthy"
  else
    bad "quorum NOT healthy ($HEALTHY/$TOTAL)"
  fi
else
  if ECTL_LOCAL endpoint health 2>/dev/null | grep -q healthy; then
    ok "local member healthy"
  else
    bad "local member unhealthy"
  fi
fi

echo "--- Step 2: Member list ---"
if [ -n "$ETCD_PEERS" ]; then
  MEMBER_COUNT=$(ECTL_REMOTE --endpoints="$ETCD_PEERS" member list 2>/dev/null | wc -l | tr -d ' ')
  if [ "$MEMBER_COUNT" = "3" ]; then
    ok "3 members registered"
  else
    bad "expected 3 members, got $MEMBER_COUNT"
  fi
  ECTL_REMOTE --endpoints="$ETCD_PEERS" member list 2>/dev/null | while IFS=, read -r id name peers state; do
    echo "  member: $name ($state)"
  done
else
  skip "single-member mode"
fi

echo "--- Step 3: Consensus (write on member-1, read on last member) ---"
TEST_KEY="/_sdkops-test/consensus-$(date +%s)"
TEST_VAL="v-$(date +%s)"
if [ -n "$ETCD_PEERS" ]; then
  FIRST_EP="${ETCD_PEERS%%,*}"
  LAST_EP="${ETCD_PEERS##*,}"
  if ECTL_REMOTE --endpoints="$FIRST_EP" put "$TEST_KEY" "$TEST_VAL" >/dev/null 2>&1 &&
     [ "$(ECTL_REMOTE --endpoints="$LAST_EP" get "$TEST_KEY" --print-value-only 2>/dev/null)" = "$TEST_VAL" ]; then
    ok "write on first member read back on last member ($TEST_VAL)"
  else
    bad "consensus write/read failed"
  fi
  ECTL_REMOTE --endpoints="$ETCD_PEERS" del "$TEST_KEY" >/dev/null 2>&1
else
  if ECTL_LOCAL put "$TEST_KEY" "$TEST_VAL" >/dev/null 2>&1 &&
     [ "$(ECTL_LOCAL get "$TEST_KEY" --print-value-only 2>/dev/null)" = "$TEST_VAL" ]; then
    ok "local put/get ($TEST_VAL)"
  else
    bad "local put/get failed"
  fi
  ECTL_LOCAL del "$TEST_KEY" >/dev/null 2>&1
fi

echo "--- Step 4: Failover (stop local member, quorum survives) ---"
if [ -n "$ETCD_PEERS" ]; then
  docker stop "$LOCAL_CONTAINER" >/dev/null 2>&1
  TEST_KEY2="/_sdkops-test/failover-$(date +%s)"
  TEST_VAL2="during-outage"
  if ECTL_REMOTE --endpoints="$ETCD_PEERS" put "$TEST_KEY2" "$TEST_VAL2" >/dev/null 2>&1; then
    ok "writes work with one member down (quorum 2/3)"
  else
    bad "writes failed during member outage"
  fi
  ECTL_REMOTE --endpoints="$ETCD_PEERS" del "$TEST_KEY2" >/dev/null 2>&1
  docker start "$LOCAL_CONTAINER" >/dev/null 2>&1
  echo -n "  Waiting for rejoin..."
  i=0
  while [ "$i" -lt 30 ]; do
    ECTL_LOCAL endpoint health 2>/dev/null | grep -q healthy && break
    i=$((i + 1))
    sleep 2
  done
  if ECTL_LOCAL endpoint health 2>/dev/null | grep -q healthy; then
    echo ""
    ok "local member rejoined and healthy"
  else
    echo ""
    bad "local member did not rejoin within 60s"
  fi
else
  skip "single-member mode"
fi

echo "--- Step 5: S3 snapshot cycle (skipped without S3 env) ---"
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  if ECTL_LOCAL snapshot save "$SNAP" >/dev/null 2>&1 &&
     ECTL_LOCAL snapshot status "$SNAP" -w json 2>/dev/null | grep -q "totalKey" &&
     docker cp "$LOCAL_CONTAINER:$SNAP" "$SNAP" >/dev/null 2>&1; then
    ok "snapshot saved + copied to host"
  else
    bad "snapshot save/status failed"
  fi
  # s3cfg: an existing one (operator-managed) wins; otherwise derive from env.
  if [ ! -s "$HOME/.s3cfg" ]; then
    umask 077
    cat > "$HOME/.s3cfg" <<EOF
[default]
access_key = $S3_ACCESS_KEY
secret_key = $S3_SECRET_KEY
host_base = $S3_ENDPOINT
host_bucket = %(bucket)s.$S3_ENDPOINT
use_https = True
EOF
  fi
  UPLOAD_NAME="etcd-$(hostname | tr -d '.')-$(date +%F-%H%M%S).db"
  if s3cmd put "$SNAP" "s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME" >/dev/null 2>&1 &&
     s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | grep -q "$UPLOAD_NAME"; then
    ok "snapshot uploaded + verified in listing ($UPLOAD_NAME)"
  else
    bad "S3 upload/verify failed"
  fi
  s3cmd del "s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME" >/dev/null 2>&1 || true
  docker exec "$LOCAL_CONTAINER" rm -f "$SNAP" >/dev/null 2>&1 || true
  rm -f "$SNAP"
else
  skip "S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY not set"
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  echo "=== etcd test PASSED ==="
  exit 0
fi
echo "=== etcd test FAILED ==="
exit 1
