#!/bin/bash
set -euo pipefail

# rps-libsql.sh — wrk benchmark for libsql-dockerized
# Usage (from VPS host):
#   docker exec libsql-dockerized-sqld-primary-1 bash /app/rps.sh
#   bash rps.sh              # if run inside sqld container

HOST="${1:-localhost}"
PORT="${2:-8080}"
LOGFILE="/tmp/libsql-bench-$(date +%Y%m%d-%H%M%S).log"

echo "============================================"
echo " libSQL wrk RPS — libsql-dockerized"
echo " Host: $HOST:$PORT  |  Clients: 50"
echo " Date:  $(date)"
echo "============================================"

# --- Init ---
echo ""
echo "--- Installing dependencies ---"
apt-get update -qq 2>&1 | tail -1
apt-get install -y -qq wrk curl 2>&1 | tail -1

echo "--- Creating table ---"
curl -s "http://$HOST:$PORT" -d '{"statements":["CREATE TABLE IF NOT EXISTS bench_kv(id INTEGER PRIMARY KEY, val TEXT)"]}' > /dev/null

# Lua scripts are at /tmp/ via Dockerfile COPY *.lua /tmp/

# --- 3 warmup ---
echo ""
echo "--- Warmup (3 rounds, 10s each) ---"
for i in 1 2 3; do
  echo "  Warmup $i/3..."
  wrk -t10 -c50 -d10s -s /tmp/sqld-read.lua "http://$HOST:$PORT" 2>&1 > /dev/null
done

# --- 5 SELECT ---
echo ""
echo "--- SELECT 1 (5 rounds, 15s each) ---"
READ_TPS=()
for i in 1 2 3 4 5; do
  printf "  Read %d/5: " $i
  rps=$(wrk -t10 -c50 -d15s -s /tmp/sqld-read.lua "http://$HOST:$PORT" 2>&1 | \
    tee -a "$LOGFILE" | grep "Requests/sec" | awk '{print $2}')
  READ_TPS+=("$rps")
  echo "$rps rps"
done

# --- 5 INSERT ---
echo ""
echo "--- INSERT (5 rounds, 15s each) ---"
WRITE_TPS=()
for i in 1 2 3 4 5; do
  printf "  Write %d/5: " $i
  rps=$(wrk -t10 -c50 -d15s -s /tmp/sqld-write.lua "http://$HOST:$PORT" 2>&1 | \
    tee -a "$LOGFILE" | grep "Requests/sec" | awk '{print $2}')
  WRITE_TPS+=("$rps")
  echo "$rps rps"
done

# --- Summary ---
echo ""
echo "============================================"
echo " RPS Summary — libsql-dockerized"
echo "============================================"

avg_read=$(printf '%s\n' "${READ_TPS[@]}" | awk '{s+=$1} END {printf "%.0f", s/NR}')
avg_write=$(printf '%s\n' "${WRITE_TPS[@]}" | awk '{s+=$1} END {printf "%.0f", s/NR}')

echo ""
echo " SELECT (avg of ${#READ_TPS[@]} rounds): $avg_read rps"
echo "   Rounds: ${READ_TPS[*]}"
echo ""
echo " INSERT (avg of ${#WRITE_TPS[@]} rounds): $avg_write rps"
echo "   Rounds: ${WRITE_TPS[*]}"
echo ""
echo " Log: $LOGFILE"
echo "============================================"
