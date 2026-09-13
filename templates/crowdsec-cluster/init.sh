#!/bin/bash
# crowdsec-cluster init - deploy CrowdSec (LAPI + agent) in k3s via the
# official helm chart, wire the Traefik bouncer plugin (stream mode + AppSec
# when the profile enables it) and apply the namespace NetworkPolicies.
# Runs ON the k3s server node: uses k3s kubectl + the helm installed by the
# fleet provision (pinned, verified here). Idempotent: re-running converges
# the release, reuses the bouncer key and re-applies the wiring.
set -e

NS="{{ .Namespace }}"
REL="{{ .Release }}"
BOUNCER="{{ .Bouncer }}"
PLUGIN_VER="{{ .PluginVersion }}"
HELM_VER="{{ .HelmVersion }}"
KUBECTL="sudo k3s kubectl"
# kubectl exec can occasionally hang (kubelet streaming flake) — bound it.
KEXEC() { for a in 1 2 3; do timeout -k 5 30 $KUBECTL exec "$@" && return 0; sleep 2; done; return 1; }
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/crowdsec-cluster"

log() { echo "[crowdsec-cluster] $1"; }
fail() { echo "[crowdsec-cluster] FAIL: $1"; exit 1; }

# 1. Helm is installed by the fleet provision on every k3s host (pinned
#    version) - verify it here instead of downloading per-template.
command -v helm >/dev/null 2>&1 || fail "helm not found - run the fleet provision (it installs helm on k3s hosts)"
helm version --short 2>/dev/null | grep -q "$HELM_VER" || log "warn: helm $HELM_VER expected, got $(helm version --short 2>/dev/null || echo none)"

# 2. Namespace + chart repo + release (all idempotent).
$KUBECTL get namespace "$NS" >/dev/null 2>&1 || $KUBECTL create namespace "$NS" >/dev/null
helm repo add crowdsec https://crowdsecurity.github.io/helm-charts >/dev/null 2>&1 || true
helm repo update >/dev/null 2>&1 || true
log "installing CrowdSec ($REL in $NS)"
helm upgrade --install "$REL" crowdsec/crowdsec -n "$NS" -f "$DIR/values.yaml" >/dev/null || fail "helm crowdsec"
log "waiting for LAPI rollout"
$KUBECTL -n "$NS" rollout status "deploy/$REL-lapi" --timeout=420s >/dev/null || fail "lapi rollout"
$KUBECTL -n "$NS" rollout status "daemonset/$REL-agent" --timeout=300s >/dev/null 2>&1 \
  || $KUBECTL -n "$NS" rollout status "deploy/$REL-agent" --timeout=300s >/dev/null 2>&1 \
  || log "warn: agent rollout not confirmed (continuing)"

# 3. Bouncer key (idempotent): reuse the key stored in the secret, create it
#    once otherwise.
LAPI_POD="$($KUBECTL -n "$NS" get pods --no-headers -o custom-columns=':.metadata.name' | grep -- "-lapi-" | head -1 || true)"
[ -n "$LAPI_POD" ] || fail "lapi pod not found in namespace $NS"
KEY="$($KUBECTL -n "$NS" get secret crowdsec-bouncer-key -o jsonpath='{.data.key}' 2>/dev/null | base64 -d 2>/dev/null || true)"
if [ -z "$KEY" ]; then
  KEY="$(KEXEC -n "$NS" "$LAPI_POD" -- cscli bouncers add "$BOUNCER" -o raw 2>/dev/null | tr -d '\r' | head -1 || true)"
  [ -n "$KEY" ] || fail "could not create bouncer $BOUNCER"
  $KUBECTL -n "$NS" create secret generic crowdsec-bouncer-key --from-literal=key="$KEY" --dry-run=client -o yaml | $KUBECTL apply -f - >/dev/null || fail "bouncer secret"
  log "bouncer $BOUNCER created (key stored in secret crowdsec-bouncer-key)"
else
  if ! KEXEC -n "$NS" "$LAPI_POD" -- cscli bouncers list -o json 2>/dev/null | grep -q "\"$BOUNCER\""; then
    KEXEC -n "$NS" "$LAPI_POD" -- cscli bouncers add "$BOUNCER" -k "$KEY" >/dev/null 2>&1 \
      || fail "bouncer $BOUNCER missing and could not be re-added with the stored key"
    log "bouncer $BOUNCER re-registered with the stored key"
  else
    log "bouncer $BOUNCER already registered"
  fi
fi

# 4. Traefik wiring. Native k3s addon: enable experimental plugins + attach
#    the middleware to both entrypoints via HelmChartConfig. Non-addon traefik:
#    warn (the operator enables the plugin in its own values).
if $KUBECTL get helmchart traefik -n kube-system >/dev/null 2>&1; then
  log "patching the k3s traefik addon (plugin crowdsec-bouncer $PLUGIN_VER)"
  $KUBECTL apply -f "$DIR/traefik-helmchartconfig.yaml" >/dev/null || fail "traefik helmchartconfig"
  $KUBECTL -n kube-system rollout status deploy/traefik --timeout=420s >/dev/null \
    || log "warn: traefik rollout not confirmed yet (the plugin loads on restart)"
  # The plugin downloads from plugins.traefik.io at traefik startup; a
  # transient timeout leaves "Plugins are disabled" until the pod restarts.
  # Idempotent retry: recreate the pod once and wait again.
  if $KUBECTL -n kube-system logs deploy/traefik --tail=300 2>/dev/null | grep -q "Plugins are disabled"; then
    log "warn: plugin download failed on this boot - recreating the traefik pod"
    $KUBECTL -n kube-system delete pod -l app.kubernetes.io/name=traefik --wait=false >/dev/null 2>&1 || true
    $KUBECTL -n kube-system rollout status deploy/traefik --timeout=420s >/dev/null \
      || log "warn: traefik rollout not confirmed after the plugin retry"
  fi
elif $KUBECTL get deploy traefik -A >/dev/null 2>&1 || $KUBECTL get daemonset traefik -A >/dev/null 2>&1; then
  log "warn: traefik is NOT the k3s addon - enable experimental.plugins.crowdsec-bouncer + the entrypoint middlewares in its values (see README)"
else
  log "warn: no traefik found in the cluster - crowdsec runs standalone (bouncer wiring skipped)"
fi

# 5. Middleware (only when the traefik CRDs exist).
if $KUBECTL get crd middlewares.traefik.io >/dev/null 2>&1; then
  cat <<EOF | $KUBECTL apply -f - >/dev/null || fail "crowdsec middleware"
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: $BOUNCER
  namespace: $NS
spec:
  plugin:
    crowdsec-bouncer:
      enabled: true
      crowdsecMode: stream
      updateIntervalSeconds: 15
      crowdsecLapiScheme: http
      crowdsecLapiHost: crowdsec-service.$NS.svc.cluster.local:8080
      crowdsecLapiPath: /
      crowdsecLapiKey: $KEY
{{ if .AppSecEnabled }}
      crowdsecAppsecEnabled: true
      crowdsecAppsecHost: crowdsec-appsec-service.$NS.svc.cluster.local:7422
      crowdsecAppsecFailureBlock: true
      crowdsecAppsecUnreachableBlock: false
{{ end }}
      forwardedHeadersTrustedIPs:
{{- range .TrustedCIDRs }}
        - {{ . }}
{{- end }}
EOF
  log "middleware $NS-$BOUNCER applied"
else
  log "warn: traefik CRDs absent - middleware not created"
fi

# 6. NetworkPolicies (default-deny ingress + restricted egress in the ns).
$KUBECTL apply -f "$DIR/netpol.yaml" >/dev/null || fail "netpols"
log "NetworkPolicies applied (namespace $NS)"

log "done: LAPI at crowdsec-service.$NS.svc.cluster.local:8080 (internal only)"
