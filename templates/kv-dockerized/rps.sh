#!/bin/bash
set -euo pipefail

# rps-kv.sh — redis-benchmark for kv-dockerized
# Usage (from VPS host):
#   docker exec kv-dockerized-dragonfly-primary-1 bash /app/rps.sh
#   bash rps.sh              # if run inside Dragonfly container

HOST="${1:-localhost}"
PORT="${2:-6379}"
PASS="${3:-dragonfly}"
NREQ="${4:-300000}"
LOGFILE="/tmp/redis-bench-$(date +%Y%m%d-%H%M%S).log"

echo "============================================"
echo " redis-benchmark RPS — kv-dockerized"
echo " Host: $HOST:$PORT  |  Requests: $NREQ  |  Clients: 50"
echo " Date:  $(date)"
echo "============================================"

get_rps() {
  local cmd=$1
  redis-benchmark -h "$HOST" -p "$PORT" -a "$PASS" -t "$cmd" -c 50 -n "$NREQ" 2>&1 | \
    tee -a "$LOGFILE" | grep "requests per second" | awk '{print $1}'
}

# --- 3 warmup ---
echo ""
echo "--- Warmup (3 rounds, 10s each) ---"
for i in 1 2 3; do
  echo "  Warmup $i/3..."
  redis-benchmark -h "$HOST" -p "$PORT" -a "$PASS" -t set,get -c 50 -n 10000 2>&1 > /dev/null
done

# --- 5 SET ---
echo ""
echo "--- SET (5 rounds) ---"
SET_TPS=()
for i in 1 2 3 4 5; do
  printf "  Set %d/5: " $i
  rps=$(get_rps set)
  SET_TPS+=("$rps")
  echo "$rps rps"
done

# --- 5 GET ---
echo ""
echo "--- GET (5 rounds) ---"
GET_TPS=()
for i in 1 2 3 4 5; do
  printf "  Get %d/5: " $i
  rps=$(get_rps get)
  GET_TPS+=("$rps")
  echo "$rps rps"
done

# --- 5 INCR ---
echo ""
echo "--- INCR (5 rounds) ---"
INCR_TPS=()
for i in 1 2 3 4 5; do
  printf "  Incr %d/5: " $i
  rps=$(get_rps incr)
  INCR_TPS+=("$rps")
  echo "$rps rps"
done

# --- Summary ---
echo ""
echo "============================================"
echo " RPS Summary — kv-dockerized"
echo "============================================"

avg_set=$(printf '%s\n' "${SET_TPS[@]}" | awk '{s+=$1} END {printf "%.0f", s/NR}')
avg_get=$(printf '%s\n' "${GET_TPS[@]}" | awk '{s+=$1} END {printf "%.0f", s/NR}')
avg_incr=$(printf '%s\n' "${INCR_TPS[@]}" | awk '{s+=$1} END {printf "%.0f", s/NR}')

echo ""
echo " SET  (avg of ${#SET_TPS[@]} rounds): $avg_set rps"
echo "   Rounds: ${SET_TPS[*]}"
echo ""
echo " GET  (avg of ${#GET_TPS[@]} rounds): $avg_get rps"
echo "   Rounds: ${GET_TPS[*]}"
echo ""
echo " INCR (avg of ${#INCR_TPS[@]} rounds): $avg_incr rps"
echo "   Rounds: ${INCR_TPS[*]}"
echo ""
echo " Log: $LOGFILE"
echo "============================================"
