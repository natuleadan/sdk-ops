#!/bin/bash
# yuga-bare validate — check the bare-metal node (or cluster) is healthy:
# yugabyted status + YSQL write/read. Exits non-zero on failure.
set -e

YB_INSTALL_DIR="${YB_INSTALL_DIR:-/opt/yugabyte}"
YB_RELEASE="${YB_RELEASE:-2026.1.1.1}"
YB_DATA_DIR="${YB_DATA_DIR:-/var/lib/yugabyte}"
YB_USER="${YB_USER:-dev}"
YB_PASSWORD="${YB_PASSWORD:-devpass}"
YB_DB="${YB_DB:-yugabyte}"
ADDR="${YB_ADVERTISE:-127.0.0.1}"
YUGABYTED="$YB_INSTALL_DIR/yugabyte-$YB_RELEASE/bin/yugabyted"
PASS=0
FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

echo "=== yuga-bare validate ($ADDR) ==="

# 1. yugabyted status via the binary.
if su -s /bin/sh yugabyte -c "$YUGABYTED status --base_dir=$YB_DATA_DIR" 2>&1 | grep -q "Running"; then
  ok "yugabyted running"
else
  bad "yugabyted not running"
fi

# 2. YSQL write/read via the app role (ysqlsh under the yugabyte bin).
YSQLSH="$YB_INSTALL_DIR/yugabyte-$YB_RELEASE/bin/ysqlsh"
if su -s /bin/sh yugabyte -c "$YSQLSH -h $ADDR -p 5433 -U $YB_USER -d $YB_DB -v ON_ERROR_STOP=1 -c \"
  CREATE TABLE IF NOT EXISTS validate(id INT PRIMARY KEY, v TEXT);
  INSERT INTO validate VALUES (1, 'ok') ON CONFLICT (id) DO UPDATE SET v='ok';
  SELECT v FROM validate WHERE id=1;
\"" 2>&1 | grep -q ok; then
  ok "YSQL write/read"
else
  bad "YSQL write/read"
fi

echo
echo "result: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
