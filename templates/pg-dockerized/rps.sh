#!/bin/bash
set -euo pipefail

# rps-pg.sh — pgbench benchmark for pg-dockerized
# Usage (from VPS host):
#   docker exec pg-dockerized-pgdog-1 bash /app/rps.sh [scale]
#   bash rps.sh [scale]              # if run inside PgDog container
#
# Default scale=10 (~1M rows in pgbench_accounts)

SCALE="${1:-10}"
PGHOST="pgdog"; PGPORT="6432"; PGUSER="dev"; PGPASS="devpass"; PGDB="postgres"
LOGFILE="/tmp/pgbench-$(date +%Y%m%d-%H%M%S).log"
export PGPASSWORD="$PGPASS"

echo "============================================"
echo " pgbench RPS — pg-dockerized"
echo " Scale: $SCALE  |  Host: $PGHOST:$PGPORT"
echo " Date:  $(date)"
echo "============================================"

# --- Init ---
echo ""
echo "--- Initializing (scale=$SCALE) ---"
pgbench -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDB" -i -s "$SCALE" 2>&1 | tee -a "$LOGFILE"

# --- 3 warmup rounds (read-write) ---
echo ""
echo "--- Warmup (3 rounds, 10s each) ---"
for i in 1 2 3; do
  echo "  Warmup $i/3..."
  pgbench -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDB" \
    -c 50 -j 4 -T 10 -P 2 2>&1 | tee -a "$LOGFILE" | grep -E 'tps|latency' | head -1
done

# --- 5 read-only tests ---
echo ""
echo "--- Read-Only (5 rounds, 15s each) ---"
READ_TPS=()
for i in 1 2 3 4 5; do
  echo "  Read $i/5..."
  out=$(pgbench -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDB" \
    -c 50 -j 4 -T 15 -S 2>&1 | tee -a "$LOGFILE")
  tps=$(echo "$out" | grep 'tps =' | grep -v excluding | awk '{print $3}')
  READ_TPS+=("$tps")
  echo "    tps = $tps"
done

# --- 5 write tests ---
echo ""
echo "--- Write (5 rounds, 15s each) ---"
WRITE_TPS=()
for i in 1 2 3 4 5; do
  echo "  Write $i/5..."
  out=$(pgbench -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDB" \
    -c 50 -j 4 -T 15 2>&1 | tee -a "$LOGFILE")
  tps=$(echo "$out" | grep 'tps =' | grep -v excluding | awk '{print $3}')
  WRITE_TPS+=("$tps")
  echo "    tps = $tps"
done

# --- Summary (awk for math) ---
echo ""
echo "============================================"
echo " RPS Summary — pg-dockerized"
echo "============================================"

avg_read=$(printf '%s\n' "${READ_TPS[@]}" | awk '{s+=$1} END {printf "%.0f", s/NR}')
avg_write=$(printf '%s\n' "${WRITE_TPS[@]}" | awk '{s+=$1} END {printf "%.0f", s/NR}')

echo ""
echo " Read-only (avg of ${#READ_TPS[@]} rounds): $avg_read tps"
echo "   Rounds: ${READ_TPS[*]}"
echo ""
echo " Write (avg of ${#WRITE_TPS[@]} rounds): $avg_write tps"
echo "   Rounds: ${WRITE_TPS[*]}"
echo ""
echo " Log: $LOGFILE"
echo "============================================"
