#!/bin/bash
# crowdsec-cluster validate - LAPI/agent readiness, bouncer registration,
# traefik wiring (when present) and namespace NetworkPolicies.
NS="{{ .Namespace }}"
REL="{{ .Release }}"
BOUNCER="{{ .Bouncer }}"
NODEPORT="{{ .LapiNodePort }}"
KUBECTL="sudo k3s kubectl"
# kubectl exec can occasionally hang (kubelet streaming flake) — bound it.
KEXEC() { for a in 1 2 3; do timeout -k 5 30 $KUBECTL exec "$@" && return 0; sleep 2; done; return 1; }
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"

PASS=0; FAIL=0; SKIP=0
ok()   { echo "  [PASS] $1"; PASS=$((PASS+1)); }
bad()  { echo "  [FAIL] $1"; FAIL=$((FAIL+1)); }
skip() { echo "  [SKIP] $1"; SKIP=$((SKIP+1)); }

echo "=== crowdsec-cluster validate ==="

if command -v helm >/dev/null 2>&1; then
  ok "helm present ($(helm version --short 2>/dev/null || echo unknown))"
else
  bad "helm missing (the fleet provision installs it on k3s hosts)"
fi

if $KUBECTL get namespace "$NS" >/dev/null 2>&1; then
  ok "namespace $NS exists"
else
  bad "namespace $NS missing"
fi

if $KUBECTL -n "$NS" rollout status "deploy/$REL-lapi" --timeout=60s >/dev/null 2>&1; then
  ok "lapi rollout ready"
else
  bad "lapi rollout not ready"
fi

if $KUBECTL -n "$NS" rollout status "daemonset/$REL-agent" --timeout=60s >/dev/null 2>&1 \
  || $KUBECTL -n "$NS" rollout status "deploy/$REL-agent" --timeout=60s >/dev/null 2>&1; then
  ok "agent rollout ready"
else
  bad "agent rollout not ready"
fi

LAPI_POD="$($KUBECTL -n "$NS" get pods --no-headers -o custom-columns=':.metadata.name' 2>/dev/null | grep -- "-lapi-" | head -1 || true)"
if [ -n "$LAPI_POD" ]; then
  ok "lapi pod $LAPI_POD"
  if KEXEC -n "$NS" "$LAPI_POD" -- cscli lapi status >/dev/null 2>&1; then
    ok "cscli lapi status"
  else
    bad "cscli lapi status"
  fi
  if KEXEC -n "$NS" "$LAPI_POD" -- cscli bouncers list -o json 2>/dev/null | grep -q "\"$BOUNCER\""; then
    ok "bouncer $BOUNCER registered"
  else
    bad "bouncer $BOUNCER not registered"
  fi
else
  bad "lapi pod not found"
fi

if [ -n "$NODEPORT" ]; then
  if $KUBECTL -n "$NS" get svc "$REL-lapi-nodeport" >/dev/null 2>&1; then
    ok "lapi nodePort service (port $NODEPORT)"
  else
    bad "lapi nodePort service missing (CS_K8S_LAPI_NODEPORT=$NODEPORT)"
  fi
else
  skip "lapi nodePort (in-cluster LAPI only)"
fi

for np in default-deny-ingress allow-lapi-pods allow-egress-dns-https-lapi; do
  if $KUBECTL -n "$NS" get networkpolicy "$np" >/dev/null 2>&1; then
    ok "networkpolicy $np"
  else
    bad "networkpolicy $np missing"
  fi
done

if $KUBECTL get helmchart traefik -n kube-system >/dev/null 2>&1; then
  if $KUBECTL -n kube-system get helmchartconfig traefik >/dev/null 2>&1; then
    ok "traefik addon patched (HelmChartConfig)"
  else
    bad "traefik HelmChartConfig missing"
  fi
else
  skip "traefik is not the k3s addon (plugin wiring is operator-side)"
fi

if $KUBECTL get crd middlewares.traefik.io >/dev/null 2>&1; then
  if $KUBECTL -n "$NS" get middleware "$BOUNCER" >/dev/null 2>&1; then
    ok "middleware $NS-$BOUNCER"
  else
    bad "middleware $NS-$BOUNCER missing"
  fi
else
  skip "traefik CRDs absent (middleware not applicable)"
fi

echo "=== validate: $PASS pass, $FAIL fail, $SKIP skip ==="
[ "$FAIL" -eq 0 ]
