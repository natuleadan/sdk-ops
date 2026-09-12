#!/bin/bash
# etcd-cluster integration test - run ON the k3s server node (k3s kubectl).
# Steps: env, cluster health, cross-member consensus, snapshot + S3 upload,
# member disaster (pod deleted -> quorum 2/3 -> rejoin), quorum boundary and
# the full S3 DR cycle (snapshot -> S3 -> helper-pod restore verification).
# S3 steps run only when S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY are set.
set -u

NAMESPACE="${ETCD_K8S_NAMESPACE:-etcd}"
RELEASE="${ETCD_K8S_RELEASE:-etcd}"
REPLICAS="${ETCD_K8S_REPLICAS:-3}"
TAG="${ETCD_K8S_TAG:-3.5.15}"
ETCD_IMAGE="docker.io/bitnami/etcd:$TAG"
S3_BUCKET="${S3_BUCKET:-etcd-backups}"
S3_PREFIX="${S3_PREFIX:-etcd}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"

FAIL=0
PASS=0
SKIP=0
ok()   { echo "  [OK] $*";   PASS=$((PASS+1)); }
bad()  { echo "  [FAIL] $*"; FAIL=$((FAIL+1)); }
skip() { echo "  [SKIP] $*"; SKIP=$((SKIP+1)); }

export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
kctl() { k3s kubectl "$@"; }
ECTL() { kctl -n "$NAMESPACE" exec "$1" -- env ETCDCTL_API=3 /opt/bitnami/etcd/bin/etcdctl --command-timeout=8s "${@:2}"; }

if [[ "$RELEASE" == *etcd* ]]; then STS="$RELEASE"; else STS="${RELEASE}-etcd"; fi
HEADLESS="${STS}-headless"
NS_SVC="$NAMESPACE.svc.cluster.local"

# Dynamic member endpoint list (headless service DNS).
endpoints() {
  EP=""
  i=0
  while [ "$i" -lt "$REPLICAS" ]; do
    E="${STS}-${i}.${HEADLESS}.${NS_SVC}:2379"
    if [ -z "$EP" ]; then EP="$E"; else EP="$EP,$E"; fi
    i=$((i + 1))
  done
  echo "$EP"
}
ENDPOINTS=$(endpoints)
EP0="${STS}-0.${HEADLESS}.${NS_SVC}:2379"
EPLAST="${STS}-$((REPLICAS - 1)).${HEADLESS}.${NS_SVC}:2379"
EP2="${STS}-2.${HEADLESS}.${NS_SVC}:2379"
EP1="${STS}-1.${HEADLESS}.${NS_SVC}:2379"

cluster_healthy() {
  local h
  h=$(ECTL "${STS}-0" --endpoints="$ENDPOINTS" endpoint health 2>/dev/null | grep -c ' is healthy' || true)
  [ "$h" = "$REPLICAS" ]
}

echo "=== etcd-cluster integration test ==="
echo "  namespace: $NAMESPACE  release: $RELEASE  replicas: $REPLICAS"
echo "  endpoints: $ENDPOINTS"

# --- Step 0: environment + etcdctl -------------------------------------------
echo "--- Step 0: env + etcdctl available ---"
if command -v k3s >/dev/null 2>&1; then
  ok "k3s kubectl available on this node"
else
  bad "k3s not found (this test runs on the k3s node)"
fi
if [ -f /etc/rancher/k3s/k3s.yaml ]; then
  ok "k3s kubeconfig present"
else
  bad "/etc/rancher/k3s/k3s.yaml missing"
fi
if ECTL "${STS}-0" version >/dev/null 2>&1; then
  ok "etcdctl reachable inside pod ${STS}-0"
else
  bad "etcdctl not reachable in pod ${STS}-0 (deploy first: bash init.sh)"
fi

# --- Step 1: endpoint health (cluster formed) --------------------------------
echo "--- Step 1: endpoint health $REPLICAS/$REPLICAS ---"
if [ "$(ECTL "${STS}-0" --endpoints="$ENDPOINTS" endpoint health 2>/dev/null | grep -c ' is healthy' || true)" = "$REPLICAS" ]; then
  ok "all $REPLICAS members healthy (cluster formed)"
else
  bad "cluster NOT fully healthy"
fi

# --- Step 2: consensus (put on member-0, get on last member) -----------------
echo "--- Step 2: consensus across members ---"
CK="/_sdkops-test/consensus-$(date +%s)"
CV="v-$(date +%s)"
if ECTL "${STS}-0" --endpoints="$EP0" put "$CK" "$CV" >/dev/null 2>&1 &&
   [ "$(ECTL "${STS}-$((REPLICAS - 1))" --endpoints="$EPLAST" get "$CK" --print-value-only 2>/dev/null)" = "$CV" ]; then
  ok "put on member-0 read back on member-$((REPLICAS - 1)) ($CV)"
else
  bad "consensus put/get failed"
fi
ECTL "${STS}-0" --endpoints="$EP0" del "$CK" >/dev/null 2>&1 || true

# --- Step 3: DR marker + snapshot + S3 upload --------------------------------
echo "--- Step 3: dr marker + snapshot + S3 upload (skip without S3 env) ---"
DR_KEY="/_sdkops-test/dr-marker"
DR_VAL="dr-$(date +%s)"
if ECTL "${STS}-0" --endpoints="$EP0" put "$DR_KEY" "$DR_VAL" >/dev/null 2>&1; then
  ok "dr marker written: $DR_KEY=$DR_VAL"
else
  bad "dr marker put failed"
fi
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  SNAP="/tmp/etcd-test-snap.db"
  rm -f "$SNAP"
  if ECTL "${STS}-0" --command-timeout=120s snapshot save /tmp/etcd-test-snap.db >/dev/null 2>&1 &&
     kctl -n "$NAMESPACE" cp "${STS}-0:/tmp/etcd-test-snap.db" "$SNAP"; then
    ok "snapshot saved + copied out of the cluster"
  else
    bad "snapshot save/copy failed"
  fi
  ECTL "${STS}-0" rm -f /tmp/etcd-test-snap.db >/dev/null 2>&1 || true
  S3OPT=(--host="$S3_ENDPOINT" --host-bucket="%(bucket)s.$S3_ENDPOINT" --access_key="$S3_ACCESS_KEY" --secret_key="$S3_SECRET_KEY")
  UPLOAD_NAME="etcd-test-$(date +%F-%H%M%S).db"
  if command -v s3cmd >/dev/null 2>&1 && s3cmd "${S3OPT[@]}" put "$SNAP" "s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME" --no-progress >/dev/null 2>&1; then
    ok "snapshot uploaded to S3 ($UPLOAD_NAME)"
  else
    bad "S3 upload failed"
  fi
  rm -f "$SNAP"
else
  skip "S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY not set"
fi

# --- Step 4: disaster - delete member-1, quorum 2/3 keeps working ------------
echo "--- Step 4: disaster (delete pod ${STS}-1) ---"
if kctl -n "$NAMESPACE" delete pod "${STS}-1" --wait=false >/dev/null 2>&1; then
  ok "pod ${STS}-1 deleted"
else
  bad "could not delete pod ${STS}-1"
fi
sleep 5
OK2="/_sdkops-test/outage-$(date +%s)"
OV2="during-outage"
if ECTL "${STS}-0" --endpoints="$EP0" put "$OK2" "$OV2" >/dev/null 2>&1 &&
   [ "$(ECTL "${STS}-0" --endpoints="$EP0" get "$OK2" --print-value-only 2>/dev/null)" = "$OV2" ]; then
  ok "writes work with one member down (quorum 2/3)"
else
  bad "writes failed during single-member outage"
fi
ECTL "${STS}-0" --endpoints="$EP0" del "$OK2" >/dev/null 2>&1 || true
echo -n "  waiting for ${STS}-1 to rejoin..."
if kctl -n "$NAMESPACE" wait --for=condition=Ready "pod/${STS}-1" --timeout=300s >/dev/null 2>&1; then
  echo ""
  ok "${STS}-1 recreated and Ready"
else
  echo ""
  bad "${STS}-1 not Ready within 300s"
fi
if cluster_healthy; then
  ok "cluster back to $REPLICAS/$REPLICAS healthy"
else
  bad "cluster health degraded after rejoin"
fi

# --- Step 5: quorum boundary - second member down ----------------------------
echo "--- Step 5: quorum boundary ---"
if [ "$REPLICAS" -ge 3 ]; then
  kctl -n "$NAMESPACE" delete pod "${STS}-0" --wait=false >/dev/null 2>&1
  sleep 5
  FK="/_sdkops-test/failover-$(date +%s)"
  FV="still-up-2of3"
  if ECTL "${STS}-2" --endpoints="$EP2" put "$FK" "$FV" >/dev/null 2>&1 &&
     [ "$(ECTL "${STS}-2" --endpoints="$EP2" get "$FK" --print-value-only 2>/dev/null)" = "$FV" ]; then
    ok "second member down: writes still work (2/3)"
  else
    bad "writes failed with 2/3 members up"
  fi
  ECTL "${STS}-2" --endpoints="$EP2" del "$FK" >/dev/null 2>&1 || true
  kctl -n "$NAMESPACE" delete pod "${STS}-2" --wait=false >/dev/null 2>&1
  sleep 5
  BK="/_sdkops-test/quorum-lost-$(date +%s)"
  if ECTL "${STS}-1" --endpoints="$EP1" put "$BK" "should-fail" >/dev/null 2>&1; then
    bad "writes succeeded with a single member (quorum NOT enforced)"
  else
    ok "quorum enforced - writes rejected with 1/3 members"
  fi
  echo -n "  waiting for ${STS}-0 and ${STS}-2 to come back..."
  if kctl -n "$NAMESPACE" wait --for=condition=Ready "pod/${STS}-0" --timeout=300s >/dev/null 2>&1 &&
     kctl -n "$NAMESPACE" wait --for=condition=Ready "pod/${STS}-2" --timeout=300s >/dev/null 2>&1; then
    echo ""
    ok "members recreated and Ready"
  else
    echo ""
    bad "members not Ready within 300s"
  fi
  if cluster_healthy; then
    ok "full quorum restored ($REPLICAS/$REPLICAS)"
  else
    bad "quorum not restored"
  fi
else
  skip "REPLICAS < 3 (single-member mode)"
fi

# --- Step 6: S3 DR cycle (skip without S3 env) -------------------------------
echo "--- Step 6: S3 DR cycle (snapshot -> S3 -> helper-pod restore) ---"
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  SNAP2="/tmp/etcd-test-dr.db"
  rm -f "$SNAP2"
  if ECTL "${STS}-0" --command-timeout=120s snapshot save /tmp/etcd-test-dr.db >/dev/null 2>&1 &&
     kctl -n "$NAMESPACE" cp "${STS}-0:/tmp/etcd-test-dr.db" "$SNAP2"; then
    ok "dr snapshot saved + copied out (contains the step-3 marker)"
  else
    bad "dr snapshot save/copy failed"
  fi
  ECTL "${STS}-0" rm -f /tmp/etcd-test-dr.db >/dev/null 2>&1 || true
  DR_UPLOAD="etcd-dr-$(date +%F-%H%M%S).db"
  if s3cmd "${S3OPT[@]}" put "$SNAP2" "s3://$S3_BUCKET/$S3_PREFIX/$DR_UPLOAD" --no-progress >/dev/null 2>&1; then
    ok "dr snapshot uploaded ($DR_UPLOAD)"
  else
    bad "dr snapshot S3 upload failed"
  fi

  HELPOD="etcd-test-dr-verify"
  HE() { kctl -n "$NAMESPACE" exec "$HELPOD" -- env ETCDCTL_API=3 /opt/bitnami/etcd/bin/etcdctl --command-timeout=8s "$@"; }
  cleanup_pod() { kctl -n "$NAMESPACE" delete pod "$HELPOD" --ignore-not-found --wait=false >/dev/null 2>&1 || true; }
  trap cleanup_pod EXIT
  kctl -n "$NAMESPACE" delete pod "$HELPOD" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  if kctl -n "$NAMESPACE" run "$HELPOD" --image="$ETCD_IMAGE" --restart=Never --command -- sleep 1800 >/dev/null 2>&1 &&
     kctl -n "$NAMESPACE" wait --for=condition=Ready "pod/$HELPOD" --timeout=180s >/dev/null 2>&1 &&
     kctl -n "$NAMESPACE" cp "$SNAP2" "$HELPOD:/tmp/snapshot.db" >/dev/null 2>&1; then
    ok "helper pod ready + snapshot injected"
  else
    bad "helper pod setup failed"
  fi
  if HE snapshot status /tmp/snapshot.db 2>/dev/null | grep -q "totalKey"; then
    ok "snapshot status readable (integrity)"
  else
    bad "snapshot status failed in helper pod"
  fi
  if HE snapshot restore /tmp/snapshot.db \
       --name etcd-verify \
       --initial-cluster etcd-verify=http://127.0.0.1:2380 \
       --initial-advertise-peer-urls http://127.0.0.1:2380 \
       --data-dir /tmp/verify-data >/dev/null 2>&1 &&
     kctl -n "$NAMESPACE" exec "$HELPOD" -- sh -c 'nohup /opt/bitnami/etcd/bin/etcd \
       --name etcd-verify --data-dir /tmp/verify-data \
       --listen-client-urls http://127.0.0.1:2379 --advertise-client-urls http://127.0.0.1:2379 \
       --listen-peer-urls http://127.0.0.1:2380 --initial-advertise-peer-urls http://127.0.0.1:2380 \
       --initial-cluster etcd-verify=http://127.0.0.1:2380 --initial-cluster-state new \
       >/tmp/etcd.log 2>&1 &' >/dev/null 2>&1; then
    ok "snapshot restored + single-node etcd started"
  else
    bad "snapshot restore/boot failed in helper pod"
  fi
  RDR_OK=0
  j=0
  while [ "$j" -lt 30 ]; do
    if HE --endpoints=127.0.0.1:2379 endpoint health 2>/dev/null | grep -q ' is healthy'; then
      RDR_OK=1
      break
    fi
    j=$((j + 1))
    sleep 2
  done
  if [ "$RDR_OK" -eq 1 ]; then
    ok "restored single-node etcd healthy"
  else
    bad "restored single-node etcd not healthy"
  fi
  if [ "$(HE --endpoints=127.0.0.1:2379 get "$DR_KEY" --print-value-only 2>/dev/null)" = "$DR_VAL" ]; then
    ok "step-3 dr marker intact after restore ($DR_VAL)"
  else
    bad "dr marker missing after restore"
  fi
  VK="/_sdkops-test/restore-probe-$(date +%s)"
  VV="restored-$(date +%s)"
  if HE --endpoints=127.0.0.1:2379 put "$VK" "$VV" >/dev/null 2>&1 &&
     [ "$(HE --endpoints=127.0.0.1:2379 get "$VK" --print-value-only 2>/dev/null)" = "$VV" ]; then
    ok "put/get on restored data works ($VV)"
  else
    bad "put/get on restored data failed"
  fi
  HE --endpoints=127.0.0.1:2379 del "$VK" >/dev/null 2>&1 || true
  cleanup_pod
  trap - EXIT
  rm -f "$SNAP2"
else
  skip "S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY not set"
fi
ECTL "${STS}-0" --endpoints="$EP0" del "$DR_KEY" >/dev/null 2>&1 || true

# --- Step 7: summary ----------------------------------------------------------
echo ""
echo "--- Step 7: summary ---"
echo "  passed: $PASS  failed: $FAIL  skipped: $SKIP"
if [ "$FAIL" -eq 0 ]; then
  echo "=== etcd-cluster test PASSED ==="
  exit 0
fi
echo "=== etcd-cluster test FAILED ==="
exit 1
