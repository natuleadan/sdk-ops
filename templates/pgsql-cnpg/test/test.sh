#!/bin/bash
# pgsql-cnpg test — integration: write/read, streaming replication, real
# failover (primary pod deleted -> operator promotes a replica), data
# survivorship, and access through the -rw / -ro services as the app user.
set -u

NS="${PG_K8S_NAMESPACE:-pg}"
NAME="${PG_K8S_NAME:-pg}"
WANT="${PG_K8S_INSTANCES:-3}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
RCI="${RCI_IMAGE:-postgres:17-alpine}"

FAILED=0
ok()  { echo "  [OK] $1"; }
bad() { echo "  [FAIL] $1"; FAILED=1; }

PASS="$($KUBECTL -n "$NS" get secret "$NAME-app" -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)"

# PSQL runs as the local postgres superuser (peer auth inside the pod).
PSQL() { $KUBECTL -n "$NS" exec "$1" -- psql -U postgres -d app -tAc "$2" 2>/dev/null; }

# SVCPSQL runs a throwaway pod against a service as the app user.
SVCPSQL() {
  local svc="$1" sql="$2" pod="pg-test-$RANDOM"
  $KUBECTL -n "$NS" run "$pod" --restart=Never --image="$RCI" --command -- \
    psql "postgresql://app:$PASS@$svc.$NS.svc:5432/app" -tAc "$sql" >/dev/null 2>&1 || true
  local phase=""
  for i in $(seq 1 15); do
    phase="$($KUBECTL -n "$NS" get pod "$pod" -o jsonpath='{.status.phase}' 2>/dev/null)"
    [ "$phase" = "Succeeded" ] && break
    [ "$phase" = "Failed" ] && break
    sleep 2
  done
  $KUBECTL -n "$NS" logs "$pod" 2>/dev/null | tr -d '[:space:]'
  $KUBECTL -n "$NS" delete pod "$pod" --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
}

echo "=== pgsql-cnpg test ==="

if [ -z "$PASS" ]; then bad "app secret missing"; echo "=== pgsql-cnpg test FAILED ==="; exit 1; fi

primary="$($KUBECTL -n "$NS" get cluster "$NAME" -o jsonpath='{.status.currentPrimary}' 2>/dev/null)"
[ -n "$primary" ] || { bad "no primary"; echo "=== pgsql-cnpg test FAILED ==="; exit 1; }
echo "-- step 1: write pre-failover data on $primary --"
PSQL "$primary" "DROP TABLE IF EXISTS sdkops_test" >/dev/null
PSQL "$primary" "CREATE TABLE sdkops_test(id serial primary key, v text)" >/dev/null
PSQL "$primary" "INSERT INTO sdkops_test(v) VALUES ('alpha'),('bravo'),('charlie')" >/dev/null
# The table is created as the postgres superuser; grant the app user access so
# the -rw/-ro service reads work like a real application would.
PSQL "$primary" "GRANT ALL PRIVILEGES ON TABLE sdkops_test TO app" >/dev/null
PSQL "$primary" "GRANT USAGE, SELECT ON SEQUENCE sdkops_test_id_seq TO app" >/dev/null
rows="$(PSQL "$primary" "SELECT count(*) FROM sdkops_test")"
[ "$rows" = "3" ] && ok "3 rows written + read back" || bad "write/read (rows=$rows)"

echo "-- step 2: streaming replication --"
reps="$(PSQL "$primary" "SELECT count(*) FROM pg_stat_replication")"
[ "${reps:-0}" -ge "$((WANT - 1))" ] && ok "streaming replicas: $reps" || bad "streaming replicas: ${reps:-0}"

echo "-- step 3: failover (delete the primary pod) --"
$KUBECTL -n "$NS" delete pod "$primary" --force --grace-period=0 >/dev/null 2>&1
new="$primary"
deadline=$((SECONDS + 240))
while [ "$SECONDS" -lt "$deadline" ]; do
  new="$($KUBECTL -n "$NS" get cluster "$NAME" -o jsonpath='{.status.currentPrimary}' 2>/dev/null)"
  [ -n "$new" ] && [ "$new" != "$primary" ] && break
  sleep 5
done
if [ "$new" != "$primary" ]; then ok "failover: $primary -> $new"; else bad "no failover within 240s"; fi

echo "-- step 4: data survived on the new primary --"
rows="$(PSQL "$new" "SELECT count(*) FROM sdkops_test")"
[ "$rows" = "3" ] && ok "3 rows survived the failover" || bad "post-failover rows=$rows"

echo "-- step 5: -rw service points at the new primary --"
rw="$(SVCPSQL "$NAME-rw" "SELECT count(*) FROM sdkops_test")"
[ "$rw" = "3" ] && ok "-rw service read: $rw" || bad "-rw service read: ${rw:-none}"

echo "-- step 6: -ro service serves reads --"
ro="$(SVCPSQL "$NAME-ro" "SELECT count(*) FROM sdkops_test")"
[ "$ro" = "3" ] && ok "-ro service read: $ro" || bad "-ro service read: ${ro:-none}"

echo "-- step 7: cleanup --"
PSQL "$new" "DROP TABLE sdkops_test" >/dev/null
ok "test data cleaned"

echo ""
if [ "$FAILED" -eq 0 ]; then echo "=== pgsql-cnpg test PASSED ==="; else echo "=== pgsql-cnpg test FAILED ==="; fi
exit "$FAILED"
