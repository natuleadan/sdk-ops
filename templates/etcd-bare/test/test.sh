#!/bin/bash
# etcd-bare integration test - quorum, consensus, failover, S3 snapshot cycle.
# Native etcdctl on the host (no Docker). Run ON one member node (as root -
# the failover step stops/starts the local systemd unit). For the full
# 3-member set ETCD_PEERS (client endpoints, comma separated):
#   ETCD_PEERS="http://198.51.100.2:2379,http://198.51.100.3:2379,http://198.51.100.4:2379" bash test/test.sh
# Without ETCD_PEERS the test runs in single-member mode (cluster steps [SKIP]).
set -u

CTL="${ETCDCTL:-/usr/local/bin/etcdctl}"
ETCD_PEERS="${ETCD_PEERS:-}"
S3_BUCKET="${S3_BUCKET:-etcd-backups}"
S3_PREFIX="${S3_PREFIX:-etcd}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
S3_CFG="${S3_CFG:-$HOME/.s3cfg}"
SNAP="/tmp/etcd-test-snap-$(date +%s).db"
FAIL=0
PASS=0

ok()   { echo "  [OK] $*";   PASS=$((PASS + 1)); }
bad()  { echo "  [FAIL] $*"; FAIL=$((FAIL + 1)); }
skip() { echo "  [SKIP] $*"; }

export ETCDCTL_API=3
# Local etcdctl against this member (native binary, no container).
ECTL_LOCAL() { "$CTL" --endpoints=127.0.0.1:2379 --command-timeout=6s "$@"; }
# Cross-host etcdctl: same host binary against remote client endpoints.
ECTL_REMOTE() { "$CTL" --command-timeout=6s "$@"; }

echo "=== etcd-bare integration test ==="
echo "  ETCD_PEERS: ${ETCD_PEERS:-<unset - single-member mode>}"

echo "--- Step 0: Environment ---"
if [ -x "$CTL" ]; then
  ok "etcdctl installed ($CTL)"
else
  bad "etcdctl not found at $CTL (run init.sh first)"
  exit 1
fi
if systemctl is-active --quiet etcd 2>/dev/null; then
  ok "systemd unit etcd active"
else
  bad "systemd unit etcd not active (run init.sh first)"
  exit 1
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
  MEMBER_JSON=$(ECTL_REMOTE --endpoints="$ETCD_PEERS" member list -w json 2>/dev/null || true)
  MEMBER_COUNT=$(printf '%s' "$MEMBER_JSON" | grep -oE '"name":"[^"]+"' | wc -l | tr -d ' ')
  if [ "$MEMBER_COUNT" = "3" ]; then
    ok "3 members registered"
  else
    bad "expected 3 members, got $MEMBER_COUNT"
  fi
  printf '%s' "$MEMBER_JSON" | grep -oE '"name":"[^"]+"' | while IFS= read -r m; do
    echo "  member: ${m#\"name\":\"}" | sed 's/"$//'
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
  ECTL_REMOTE --endpoints="$FIRST_EP" del "$TEST_KEY" >/dev/null 2>&1 || true
else
  if ECTL_LOCAL put "$TEST_KEY" "$TEST_VAL" >/dev/null 2>&1 &&
     [ "$(ECTL_LOCAL get "$TEST_KEY" --print-value-only 2>/dev/null)" = "$TEST_VAL" ]; then
    ok "local put/get ($TEST_VAL)"
  else
    bad "local put/get failed"
  fi
  ECTL_LOCAL del "$TEST_KEY" >/dev/null 2>&1 || true
fi

echo "--- Step 4: Failover (stop local member, quorum survives) ---"
if [ -n "$ETCD_PEERS" ]; then
  systemctl stop etcd >/dev/null 2>&1 || true
  TEST_KEY2="/_sdkops-test/failover-$(date +%s)"
  TEST_VAL2="during-outage"
  if ECTL_REMOTE --endpoints="$ETCD_PEERS" put "$TEST_KEY2" "$TEST_VAL2" >/dev/null 2>&1; then
    ok "writes work with one member down (quorum 2/3)"
  else
    bad "writes failed during member outage"
  fi
  ECTL_REMOTE --endpoints="${ETCD_PEERS%%,*}" del "$TEST_KEY2" >/dev/null 2>&1 || true
  systemctl start etcd >/dev/null 2>&1 || true
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
  command -v s3cmd >/dev/null 2>&1 || { bad "s3cmd not installed"; }
  if [ "$FAIL" -eq 0 ]; then
    ensure_s3cfg() {
      if [ -s "$S3_CFG" ]; then return 0; fi
      umask 077
      cat > "$S3_CFG" <<EOF
[default]
access_key = $S3_ACCESS_KEY
secret_key = $S3_SECRET_KEY
host_base = $S3_ENDPOINT
host_bucket = %(bucket)s.$S3_ENDPOINT
use_https = True
EOF
    }
    if ECTL_LOCAL --command-timeout=120s snapshot save "$SNAP" >/dev/null 2>&1 &&
       ECTL_LOCAL snapshot status "$SNAP" 2>/dev/null | grep -q totalKey; then
      ok "snapshot saved + status readable"
    else
      bad "snapshot save/status failed"
    fi
    UPLOAD_NAME="etcd-$(hostname | tr -d '.')-$(date +%F-%H%M%S).db"
    ensure_s3cfg
    if s3cmd -c "$S3_CFG" mb "s3://$S3_BUCKET" >/dev/null 2>&1 || true; then :; fi
    if s3cmd -c "$S3_CFG" --no-progress put "$SNAP" "s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME" >/dev/null 2>&1 &&
       s3cmd -c "$S3_CFG" ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | grep -q "$UPLOAD_NAME"; then
      ok "snapshot uploaded + verified in listing ($UPLOAD_NAME)"
    else
      bad "S3 upload/verify failed"
    fi
    rm -f "$SNAP"
  fi
else
  skip "S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY not set"
fi

echo ""
echo "  passed: $PASS  failed: $FAIL"
if [ "$FAIL" -eq 0 ]; then
  echo "=== etcd-bare test PASSED ==="
  exit 0
fi
echo "=== etcd-bare test FAILED ==="
exit 1
