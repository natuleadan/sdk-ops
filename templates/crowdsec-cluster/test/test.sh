#!/bin/bash
# crowdsec-cluster test - integration: validate + decision lifecycle + (when
# the traefik wiring is present) an end-to-end block through the ingress:
# whoami route -> 200, decision for the forwarded IP -> 403, remove -> 200.
NS="{{ .Namespace }}"
REL="{{ .Release }}"
BOUNCER="{{ .Bouncer }}"
TEST_NS="crowdsec-test"
TEST_IP="192.0.2.66"
KUBECTL="sudo k3s kubectl"
# kubectl exec can occasionally hang (kubelet streaming flake) — bound it.
KEXEC() { for a in 1 2 3; do timeout -k 5 30 $KUBECTL exec "$@" && return 0; sleep 2; done; return 1; }
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/crowdsec-cluster"

PASS=0; FAIL=0; SKIP=0
ok()   { echo "  [PASS] $1"; PASS=$((PASS+1)); }
bad()  { echo "  [FAIL] $1"; FAIL=$((FAIL+1)); }
skip() { echo "  [SKIP] $1"; SKIP=$((SKIP+1)); }

cleanup() {
  $KUBECTL delete namespace "$TEST_NS" --force --grace-period=0 >/dev/null 2>&1 || true
  KEXEC -n "$NS" "$LAPI_POD" -- cscli decisions delete --ip "$TEST_IP" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "=== crowdsec-cluster test ==="

echo "--- validate ---"
bash "$DIR/validate.sh" >/dev/null 2>&1 && ok "validate.sh" || bad "validate.sh"

LAPI_POD="$($KUBECTL -n "$NS" get pods --no-headers -o custom-columns=':.metadata.name' 2>/dev/null | grep -- "-lapi-" | head -1 || true)"
[ -n "$LAPI_POD" ] || { bad "lapi pod not found"; echo "=== test: $PASS pass, $FAIL fail, $SKIP skip ==="; exit 1; }

echo "--- decision lifecycle ---"
if KEXEC -n "$NS" "$LAPI_POD" -- cscli decisions add --ip "$TEST_IP" --duration 5m --reason "sdk-ops test" >/dev/null 2>&1; then
  ok "decision add"
else
  bad "decision add"
fi
if KEXEC -n "$NS" "$LAPI_POD" -- cscli decisions list -o json 2>/dev/null | grep -q "$TEST_IP"; then
  ok "decision visible in LAPI"
else
  bad "decision not listed"
fi
if KEXEC -n "$NS" "$LAPI_POD" -- cscli decisions delete --ip "$TEST_IP" >/dev/null 2>&1; then
  ok "decision delete"
else
  bad "decision delete"
fi

echo "--- end-to-end block through traefik ---"
TRAEFIK_IP="$($KUBECTL -n kube-system get svc traefik -o jsonpath='{.spec.clusterIP}' 2>/dev/null || true)"
MW_OK="no"
$KUBECTL -n "$NS" get middleware "$BOUNCER" >/dev/null 2>&1 && MW_OK="yes"

if [ -z "$TRAEFIK_IP" ] || [ "$MW_OK" != "yes" ]; then
  skip "traefik/middleware not present - e2e block not applicable"
else
  $KUBECTL create namespace "$TEST_NS" >/dev/null 2>&1 || true
  cat <<YAML | $KUBECTL apply -f - >/dev/null 2>&1
apiVersion: apps/v1
kind: Deployment
metadata:
  name: whoami
  namespace: $TEST_NS
spec:
  replicas: 1
  selector:
    matchLabels: {app: whoami}
  template:
    metadata:
      labels: {app: whoami}
    spec:
      containers:
        - name: whoami
          image: traefik/whoami:v1.10
          ports: [{containerPort: 80}]
---
apiVersion: v1
kind: Service
metadata:
  name: whoami
  namespace: $TEST_NS
spec:
  selector: {app: whoami}
  ports: [{port: 80, targetPort: 80}]
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: whoami
  namespace: $TEST_NS
spec:
  entryPoints: [web]
  routes:
    - match: Host(\`waf-test.local\`)
      kind: Rule
      services:
        - name: whoami
          port: 80
YAML
  if $KUBECTL -n "$TEST_NS" rollout status deploy/whoami --timeout=180s >/dev/null 2>&1; then
    ok "whoami test app ready"
  else
    bad "whoami test app not ready"
  fi

  # Client pod with a deterministic source IP: the ban targets its pod IP, so
  # the test works regardless of the node egress (no XFF games).
  cat <<YAML | $KUBECTL apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Pod
metadata:
  name: waf-cli
  namespace: $TEST_NS
spec:
  restartPolicy: Never
  containers:
    - name: curl
      image: curlimages/curl:8.10.1
      command: ['sleep', '3600']
YAML
  $KUBECTL -n "$TEST_NS" wait --for=condition=Ready pod/waf-cli --timeout=120s >/dev/null 2>&1
  CLIENT_IP="$($KUBECTL -n "$TEST_NS" get pod waf-cli -o jsonpath='{.status.podIP}' 2>/dev/null || true)"
  if [ -n "$CLIENT_IP" ]; then
    ok "client pod up ($CLIENT_IP)"
  else
    bad "client pod ip not available"
  fi

  req() {
    $KUBECTL -n "$TEST_NS" exec waf-cli -- curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
      -H 'Host: waf-test.local' "http://$TRAEFIK_IP/" 2>/dev/null || true
  }
  wait_code() {
    local want="$1" got=""
    for _ in $(seq 1 18); do
      got="$(req)"
      [ "$got" = "$want" ] && { echo "$got"; return 0; }
      sleep 5
    done
    echo "$got"
    return 1
  }

  got="$(wait_code 200)" && ok "route reachable without decision ($got)" || bad "route unreachable without decision (got $got)"

  [ -n "$CLIENT_IP" ] || { bad "no client ip - e2e block aborted"; echo "=== test: $PASS pass, $FAIL fail, $SKIP skip ==="; exit 1; }
  KEXEC -n "$NS" "$LAPI_POD" -- cscli decisions add --ip "$CLIENT_IP" --duration 2m --reason "sdk-ops e2e" >/dev/null 2>&1 || bad "e2e decision add"
  got="$(wait_code 403)" && ok "blocked while the decision is active ($got)" || bad "not blocked while banned (got $got)"

  KEXEC -n "$NS" "$LAPI_POD" -- cscli decisions delete --ip "$CLIENT_IP" >/dev/null 2>&1 || bad "e2e decision delete"
  got="$(wait_code 200)" && ok "unblocked after removing the decision ($got)" || bad "not unblocked after removing the decision (got $got)"
fi

echo "=== test: $PASS pass, $FAIL fail, $SKIP skip ==="
[ "$FAIL" -eq 0 ]
