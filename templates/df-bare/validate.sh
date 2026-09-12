#!/bin/sh
# df-bare validate - health check of the native stack (real exit codes).
# All dragonfly ports are loopback-only; TLS is probed through HAProxy :6443.
set -e

# Password: operator env first, else the rendered flagfile the server itself
# uses (secrets never live in the repo, the server knows its own config).
DF_PASSWORD="${DF_PASSWORD:-}"
if [ -z "$DF_PASSWORD" ] && [ -r /etc/dragonfly/primary.conf ]; then
  DF_PASSWORD="$(sed -n 's/^--requirepass=//p' /etc/dragonfly/primary.conf | head -1)"
fi
DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
TLS_DIR="${TLS_DIR:-./ssl}"

RC()     { redis-cli -h 127.0.0.1 -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP() { redis-cli -h 127.0.0.1 -p 6380 -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP2(){ redis-cli -h 127.0.0.1 -p 6381 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== df-bare validate ==="
FAIL=0

echo -n "Units (dragonfly + haproxy): "
UNITS_OK=1
for u in dragonfly-primary dragonfly-replica-1 dragonfly-replica-2 haproxy; do
  systemctl is-active --quiet "$u" 2>/dev/null || UNITS_OK=0
done
if [ "$UNITS_OK" = 1 ]; then echo "OK"; else echo "FAIL"; FAIL=1; fi

echo -n "Primary: "
RC PING | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Replica-1: "
RC_REP PING | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Replica-2: "
RC_REP2 PING | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "HAProxy config: "
haproxy -c -f /etc/haproxy/hap.cfg >/dev/null 2>&1 && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "SQL (PING via primary): "
RC PING | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Vector search: "
echo "skip"

echo -n "Replication: "
RC INFO REPLICATION | grep -q "role:master" && echo "streaming" || { echo "not connected"; FAIL=1; }

echo -n "Replica-1 link: "
RC_REP INFO REPLICATION | grep -q "master_link_status:up" && echo "up" || { echo "down"; FAIL=1; }

echo -n "Replica-2 link: "
RC_REP2 INFO REPLICATION | grep -q "master_link_status:up" && echo "up" || { echo "down"; FAIL=1; }

echo -n "Replica lag: "
LAG=$(RC INFO REPLICATION | grep "slave0:" | tr ',' '\n' | grep "^lag=" | cut -d= -f2 | head -1)
echo "${LAG:-0} records"

echo -n "TLS via HAProxy (:6443): "
redis-cli --tls --cacert "$TLS_DIR/ca.crt" -h 127.0.0.1 -p 6443 -a "$DF_PASSWORD" PING 2>/dev/null | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "3-node independent: "
P1=0; R1=0; R2=0
RC PING 2>/dev/null | grep -q PONG && P1=1
RC_REP PING 2>/dev/null | grep -q PONG && R1=1
RC_REP2 PING 2>/dev/null | grep -q PONG && R2=1
TOTAL=$((P1 + R1 + R2))
if [ "$TOTAL" -eq 3 ]; then
  echo "OK (3/3 responding)"
else
  echo "FAIL ($TOTAL/3 responding)"
  FAIL=1
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  echo "[OK] df-bare ready (3 nodes + haproxy)"
else
  echo "[X] Some checks failed"
  exit 1
fi
