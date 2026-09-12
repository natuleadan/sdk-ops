#!/bin/bash
# df-cluster integration test — exercises the Dragonfly CR like an operator:
#   Step 0  env + ephemeral redis-cli helper
#   Step 1  services (pods Running, CR ready)
#   Step 2  data lifecycle: SET/GET via the master service
#   Step 3  replica read (replication state)
#   Step 4  S3 DR cycle (BGSAVE -> kubectl cp -> s3cmd; restore back) — needs S3 env
#   Step 5  failover: delete the master pod -> operator promotes a replica ->
#           the service follows the new master -> data survives
#   Step 6  summary + exit code
#
# Run on a k3s node:  bash test/test.sh
set -u

NS="${DF_K8S_NAMESPACE:-df}"
NAME="${DF_K8S_NAME:-df}"
WANT="${DF_K8S_REPLICAS:-3}"
DIR="$(cd "$(dirname "$0")/.." && pwd)"
[ -f "$DIR/.env" ] && . "$DIR/.env"
PASS="${DF_PASSWORD:-dragonfly}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
RCI="${RCI_IMAGE:-redis:7.0.10}"

FAILED=0
ok()  { echo "  [OK] $1"; }
bad() { echo "  [FAIL] $1"; FAILED=1; }
skip(){ echo "  [SKIP] $1"; }
workpod="df-test-$$"

# rc <args...> — run redis-cli against the master service via an ephemeral pod.
# run + logs (not `run --rm -i`: the stdin attach races the pod start).
rc() {
  $KUBECTL -n "$NS" delete pod "$workpod" --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
  $KUBECTL -n "$NS" run "$workpod" --restart=Never --image="$RCI" --command -- \
    redis-cli -h "$NAME.$NS.svc" -p 6379 -a "$PASS" --no-auth-warning "$@" >/dev/null 2>&1
  local ph=""
  for i in $(seq 1 20); do
    ph="$($KUBECTL -n "$NS" get pod "$workpod" -o jsonpath='{.status.phase}' 2>/dev/null)"
    [ "$ph" = "Succeeded" ] && break
    [ "$ph" = "Failed" ] && break
    sleep 2
  done
  $KUBECTL -n "$NS" logs "$workpod" 2>/dev/null | tr -d '\r'
  $KUBECTL -n "$NS" delete pod "$workpod" --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
}
rcpod() { # rcpod <pod> <args...> — redis-cli exec'd INSIDE a dragonfly pod (no image pull)
  local pod="$1"; shift
  $KUBECTL -n "$NS" exec "$pod" -- redis-cli -p 6379 -a "$PASS" --no-auth-warning "$@" 2>/dev/null
}
rcwait() { # wait until the service answers PING (roles re-assigned, endpoints ready)
  local d=$((SECONDS + 180))
  while [ "$SECONDS" -lt "$d" ]; do
    [ "$(rc PING)" = "PONG" ] && return 0
    sleep 5
  done
  return 1
}

echo "=== df-cluster test ==="

# --- Step 0: env -------------------------------------------------------------
echo "-- step 0: env --"
$KUBECTL -n "$NS" get dragonfly "$NAME" >/dev/null 2>&1 || { echo "  [FAIL] Dragonfly CR $NAME not found"; echo "=== df-cluster test FAILED ==="; exit 1; }
ok "Dragonfly CR + kubectl ready"

# --- Step 1: services --------------------------------------------------------
echo "-- step 1: services --"
running="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly" --field-selector=status.phase=Running -o name 2>/dev/null | wc -l | tr -d ' ')"
[ "${running:-0}" -ge "$WANT" ] && ok "pods running $running/$WANT" || bad "pods running ${running:-0}/$WANT"

# --- Step 2: data lifecycle --------------------------------------------------
echo "-- step 2: data lifecycle --"
if [ "$(rc SET cluster:probe hello)" = "OK" ] && [ "$(rc GET cluster:probe)" = "hello" ]; then
  ok "SET/GET via $NAME.$NS.svc"
else
  bad "SET/GET via service"
fi

# --- Step 3: replica read ----------------------------------------------------
echo "-- step 3: replication --"
info="$(rc INFO replication)"
if echo "$info" | grep -q "role:master" \
   && [ "$(echo "$info" | grep -oE 'connected_slaves:[0-9]+' | cut -d: -f2)" -ge "$((WANT - 1))" ]; then
  ok "master role + $((WANT - 1))+ replicas connected"
else
  bad "replication state"
fi

# --- Step 4: S3 DR cycle (native snapshot) ------------------------------------
echo "-- step 4: S3 DR cycle --"
if [ -n "${S3_BUCKET:-}" ] && [ -n "${S3_ENDPOINT:-}" ]; then
  S3DIR="s3://$S3_BUCKET/${S3_PREFIX:-df}/"
  rc SET cluster:dr keepme >/dev/null
  before="$(s3cmd ls "$S3DIR" 2>/dev/null | wc -l | tr -d ' ')"
  master="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly,role=master" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
  [ -z "$master" ] && master="$NAME-0"
  if rcpod "$master" SAVE >/dev/null 2>&1; then
    ok "SAVE on $master (native snapshot)"
    d=$((SECONDS + 180)); snap=""
    while [ "$SECONDS" -lt "$d" ]; do
      snap="$(s3cmd ls "$S3DIR" 2>/dev/null | awk '{print $NF}' | grep -v summary | grep '\.dfs$' | sort | tail -1)"
      after="$(s3cmd ls "$S3DIR" 2>/dev/null | wc -l | tr -d ' ')"
      if [ -n "$snap" ] && [ "${after:-0}" -gt "${before:-0}" ] \
         && s3cmd info "${snap%-0000.dfs}-summary.dfs" >/dev/null 2>&1; then break; fi
      snap=""
      sleep 5
    done
    if [ -n "$snap" ] && [ "${after:-0}" -gt "${before:-0}" ]; then
      ok "snapshot in S3: $(basename "$snap")"
      if [ "$(rc FLUSHALL)" = "OK" ] && [ "$(rc DBSIZE)" = "0" ]; then
        ok "disaster: FLUSHALL (dbsize 0)"
        # Rollback: down (shutdown saves land while down), drop every dump
        # newer than the good one, up (boot loads the good one — now newest).
        $KUBECTL -n "$NS" scale statefulset "$NAME" --replicas=0 >/dev/null 2>&1
        d=$((SECONDS + 300)); left=1
        while [ "$SECONDS" -lt "$d" ]; do
          left="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly" --no-headers 2>/dev/null | wc -l | tr -d ' ')"
          [ "${left:-1}" = "0" ] && break
          sleep 5
        done
        newer="$(s3cmd ls "$S3DIR" 2>/dev/null | awk '{print $NF}' | grep -v summary | grep '\.dfs$' | awk -v t="$snap" '$0 > t' || true)"
        for dd in $newer; do
          s3cmd del "$dd" >/dev/null 2>&1 || true
          s3cmd del "${dd%-0000.dfs}-summary.dfs" >/dev/null 2>&1 || true
        done
        $KUBECTL -n "$NS" scale statefulset "$NAME" --replicas="$WANT" >/dev/null 2>&1
        d=$((SECONDS + 600)); ready=0
        while [ "$SECONDS" -lt "$d" ]; do
          ready="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly" --no-headers 2>/dev/null | awk '$2 == "1/1"' | wc -l | tr -d ' ')"
          [ "${ready:-0}" -ge "$WANT" ] && break
          sleep 5
        done
        rcpwait=$((SECONDS + 180)); rcpolled=0
        while [ "$SECONDS" -lt "$rcpwait" ]; do
          [ "$(rc PING)" = "PONG" ] && { rcpolled=1; break; }
          sleep 5
        done
        [ "$rcpolled" = "1" ] || bad "service not answering after the rollback"
        got="$(rc GET cluster:dr)"
        [ "$got" = "keepme" ] && ok "boot loaded the S3 snapshot — data restored" || bad "restore verify (GET -> '${got}')"
      else
        bad "FLUSHALL disaster"
      fi
    else
      bad "no new snapshot in $S3DIR"
    fi
  else
    bad "SAVE on master pod"
  fi
else
  skip "S3 DR cycle (S3_* env not set)"
fi

# --- Step 5: master pod deletion (operator recovery) --------------------------
echo "-- step 5: master pod deletion (operator recovery) --"
master="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly,role=master" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
[ -z "$master" ] && master="$NAME-0"
rcwait || bad "service not answering before the master deletion"
if rc SET cluster:before failover >/dev/null 2>&1 \
   && $KUBECTL -n "$NS" delete pod "$master" --force --grace-period=0 >/dev/null 2>&1; then
  # The operator re-elects/reconciles the master role and reconfigures replicas.
  d=$((SECONDS + 600)); ready=0
  while [ "$SECONDS" -lt "$d" ]; do
    ready="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly" --no-headers 2>/dev/null | awk '$2 == "1/1"' | wc -l | tr -d ' ')"
    [ "${ready:-0}" -ge "$WANT" ] && break
    sleep 5
  done
  [ "${ready:-0}" -ge "$WANT" ] || bad "pods not ready after $master deletion ($ready/$WANT)"
  newmaster="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly,role=master" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
  ok "cluster recovered — master now: ${newmaster:-none} (deleted $master)"
  rcpwait=$((SECONDS + 180)); rcpolled=0
  while [ "$SECONDS" -lt "$rcpwait" ]; do
    [ "$(rc PING)" = "PONG" ] && { rcpolled=1; break; }
    sleep 5
  done
  if [ "$rcpolled" = "1" ] && [ "$(rc GET cluster:before)" = "failover" ] && [ "$(rc SET cluster:after ok)" = "OK" ]; then
    ok "service follows the master + data survives + writes OK"
  else
    bad "data/writes after master deletion"
  fi
else
  bad "master pod delete"
fi

# --- Step 6: summary -----------------------------------------------------------
echo "-- step 6: summary --"
if [ "$FAILED" -eq 0 ]; then
  echo "=== df-cluster test PASSED ==="
else
  echo "=== df-cluster test FAILED ==="
fi
exit "$FAILED"
