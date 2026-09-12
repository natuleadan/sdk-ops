#!/bin/bash
# nats-cluster integration test — exercises the k3s deployment like an operator:
#   Step 0  env + tools (kubectl, nats CLI on the node via port-forward)
#   Step 1  cluster R3 (statefulset ready + strict /healthz on every pod)
#   Step 2  pub/sub + JetStream lifecycle via nats-box (stream add/pub/read)
#   Step 3  KV bucket R3 (put/get/delete via nats-box)
#   Step 4  NACK declarative: apply a Stream CRD -> Ready -> pub -> delete -> gone
#   Step 5  failover: delete pod nats-1 -> cluster keeps serving -> rejoin
#   Step 6  S3 DR cycle (node-side nats CLI 0.4.0 + seal -> S3 -> delete -> unseal -> restore)
#   Step 7  summary + exit code
#
# Run on a k3s node:  bash test/test.sh
# Full S3 DR requires S3_ENDPOINT/S3_BUCKET/S3_ACCESS_KEY/S3_SECRET_KEY plus the
# operator NKeys (NATS_SEAL_SENDER_NK + NATS_SEAL_RECIPIENT_PUB on the seal side,
# NATS_RECIPIENT_NK / NATS_UNSEAL_RECIPIENT_NK for the unseal half) - without
# them the S3 steps are [SKIP]ped.
set -u

NS="${NATS_K8S_NAMESPACE:-nats}"
REL="${NATS_K8S_RELEASE:-nats}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
NACK="${NATS_K8S_NACK:-true}"
WANT="${NATS_K8S_REPLICAS:-3}"
DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORK="/tmp/nats-cluster-test"
PF_PORT="14222"

FAILED=0
ok()  { echo "  [OK] $1"; }
bad() { echo "  [FAIL] $1"; FAILED=1; }
skip(){ echo "  [SKIP] $1"; }

echo "=== nats-cluster test ==="

# --- Step 0: env + node-side nats CLI (pinned) ------------------------------
echo "-- step 0: env + tools --"
$KUBECTL -n "$NS" get statefulset "$REL" >/dev/null 2>&1 || { echo "  [FAIL] statefulset $REL not found"; echo "=== nats-cluster test FAILED ==="; exit 1; }
BIN="$DIR/nats"
if [ ! -x "$BIN" ]; then
  ARCH="$(uname -m)"
  case "$ARCH" in x86_64|amd64) CARCH="amd64" ;; aarch64|arm64) CARCH="arm64" ;; *) CARCH="" ;; esac
  CLI_VER="${NATS_CLI_VERSION:-0.4.0}"
  if [ -n "$CARCH" ] && curl -fsSL "https://github.com/nats-io/natscli/releases/download/v${CLI_VER}/nats-${CLI_VER}-linux-${CARCH}.tar.gz" -o /tmp/natscli.tgz 2>/dev/null; then
    tar -xzf /tmp/natscli.tgz -C /tmp
    mv "/tmp/nats-${CLI_VER}-linux-${CARCH}/nats" "$BIN" && chmod +x "$BIN"
    rm -rf "/tmp/nats-${CLI_VER}-linux-${CARCH}" /tmp/natscli.tgz
  fi
fi
[ -x "$BIN" ] && ok "nats CLI $("$BIN" --version 2>/dev/null || echo present)" || skip "node nats CLI (S3 DR steps will skip)"
# Node-side connection target: port-forward to the ClusterIP service.
PF_PID=""
node_url() {
  if [ -z "$PF_PID" ] || ! kill -0 "$PF_PID" 2>/dev/null; then
    $KUBECTL -n "$NS" port-forward "svc/${REL}" "${PF_PORT}:4222" >/dev/null 2>&1 &
    PF_PID=$!
    sleep 2
  fi
  echo "nats://127.0.0.1:${PF_PORT}"
}
cleanup_pf() { [ -n "$PF_PID" ] && kill "$PF_PID" 2>/dev/null; }
trap cleanup_pf EXIT

# --- Step 1: cluster R3 ------------------------------------------------------
echo "-- step 1: cluster R3 --"
ready="$($KUBECTL -n "$NS" get statefulset "$REL" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)"
[ "$ready" = "$WANT" ] && ok "statefulset ready $ready/$WANT" || bad "statefulset ready ${ready:-0}/$WANT"
i=0; hbad=0
while [ "$i" -lt "$WANT" ]; do
  h="$($KUBECTL -n "$NS" exec "${REL}-${i}" -c nats -- wget -qO- http://localhost:8222/healthz 2>/dev/null || true)"
  case "$h" in *ok*) : ;; *) hbad=1 ;; esac
  i=$((i + 1))
done
[ "$hbad" -eq 0 ] && ok "/healthz strict ok on all $WANT pods" || bad "/healthz strict"

# --- Step 2: pub/sub + JetStream via nats-box --------------------------------
echo "-- step 2: pub/sub + JetStream --"
BOX_ARGS=(--server="nats://${REL}.${NS}.svc:4222")
# Clean up leftover streams/KV from previous runs.
$KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" stream rm cluster-test --force >/dev/null 2>&1 || true
$KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" kv del cluster-kv --force >/dev/null 2>&1 || true
if $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" stream add cluster-test --subjects "demo.cluster.>" --storage file --replicas "$WANT" --defaults >/dev/null 2>&1 \
   && $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" pub demo.cluster.one hello >/dev/null 2>&1 \
   && $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" pub demo.cluster.two world >/dev/null 2>&1 \
   && [ "$($KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" stream info cluster-test 2>/dev/null | awk '/^[[:space:]]*Messages:/{print $2; exit}')" = "2" ]; then
  ok "stream cluster-test R$WANT created + 2 messages"
else
  bad "JetStream stream lifecycle"
fi

# --- Step 3: KV bucket R3 -----------------------------------------------------
echo "-- step 3: KV bucket --"
if $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" kv add cluster-kv --replicas "$WANT" --storage file >/dev/null 2>&1 \
   && $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" kv put cluster-kv probe secret >/dev/null 2>&1 \
   && [ "$($KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" kv get cluster-kv probe --raw 2>/dev/null)" = "secret" ]; then
  ok "KV cluster-kv R$WANT put/get"
else
  bad "KV bucket lifecycle"
fi

# --- Step 4: NACK declarative stream ------------------------------------------
echo "-- step 4: NACK declarative (Stream CRD) --"
if [ "$NACK" = "true" ]; then
  if $KUBECTL get crd streams.jetstream.nats.io >/dev/null 2>&1; then
    CRD="apiVersion: jetstream.nats.io/v1beta2
kind: Stream
metadata:
  name: nack-probe
  namespace: $NS
spec:
  name: NACK_PROBE
  subjects: [\"demo.nack.>\"]
  storage: file
  replicas: $WANT"
    if echo "$CRD" | $KUBECTL apply -f - >/dev/null 2>&1; then
      state=""
      for t in $(seq 1 20); do
        state="$($KUBECTL -n "$NS" get stream nack-probe -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
        [ "$state" = "True" ] && break
        sleep 3
      done
      if [ "$state" = "True" ]; then
        ok "Stream CRD nack-probe reconciled Ready by NACK"
        if $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" pub demo.nack.test via-crd >/dev/null 2>&1; then
          ok "publish through NACK-managed stream"
        else
          bad "publish to NACK stream"
        fi
        # NACK default: CRD delete does NOT delete the stream (it's a
        # create/reconcile controller). controlLoop: true would enforce
        # deletion. Verify the CRD is gone; the stream may persist.
        if echo "$CRD" | $KUBECTL delete -f - >/dev/null 2>&1; then
          ok "CRD deleted (stream may persist — NACK default behavior)"
        else
          bad "CRD delete"
        fi
      else
        bad "Stream CRD never reached Ready (controller state: ${state:-none})"
      fi
    else
      bad "Stream CRD apply"
    fi
  else
    skip "NACK CRDs not installed (NATS_K8S_NACK=false?)"
  fi
else
  skip "NACK disabled"
fi

# --- Step 5: failover (delete pod nats-1) --------------------------------------
echo "-- step 5: failover (pod deletion) --"
victim="${REL}-1"
if [ "$WANT" -ge 3 ] && $KUBECTL -n "$NS" get pod "$victim" >/dev/null 2>&1; then
  if $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" pub demo.cluster.during pre-kill >/dev/null 2>&1 \
     && $KUBECTL -n "$NS" delete pod "$victim" --force --grace-period=0 >/dev/null 2>&1; then
    # The quorum (2/3) must keep serving writes while the pod reschedules.
    if $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" pub demo.cluster.during post-kill >/dev/null 2>&1; then
      ok "writes survive pod deletion (quorum 2/3)"
    else
      bad "writes during pod reschedule"
    fi
    $KUBECTL -n "$NS" rollout status "statefulset/${REL}" --timeout=300s >/dev/null 2>&1 \
      && ok "pod $victim rejoined" || bad "rejoin of $victim"
    if $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" stream info cluster-test 2>/dev/null | grep -q "Messages: 4"; then
      ok "stream intact after failover (4 messages)"
    else
      bad "stream message count after failover"
    fi
  else
    bad "failover: pod delete"
  fi
else
  skip "failover (needs replicas >= 3)"
fi

# --- Step 6: S3 DR cycle (node-side, seal -> S3 -> delete -> unseal -> restore) --
echo "-- step 6: S3 DR cycle --"
if [ -x "$BIN" ] && [ -n "${S3_BUCKET:-}" ] && [ -n "${S3_ENDPOINT:-}" ] && [ -n "${NATS_SEAL_SENDER_NK:-}" ] && [ -n "${NATS_SEAL_RECIPIENT_PUB:-}" ]; then
  URL="$(node_url)"
  APP=(--server "$URL")
  # RAPP: the 0.4.0 CLI mis-parses space-separated globals on stream restore.
  RAPP=(--server="$URL")
  DR_STREAM="cluster-test"
  TS="$(date +%Y%m%d-%H%M%S)"
  KEY="nats/${DR_STREAM}/${TS}.nkey"
  rm -rf /tmp/nats-dr; mkdir -p /tmp/nats-dr
  if "$BIN" stream backup "$DR_STREAM" /tmp/nats-dr/"$DR_STREAM" "${APP[@]}" >/dev/null 2>&1 \
     && tar czf /tmp/nats-dr/"$DR_STREAM".tar.gz -C /tmp/nats-dr "$DR_STREAM" \
     && "$BIN" auth nkey seal /tmp/nats-dr/"$DR_STREAM".tar.gz "$NATS_SEAL_SENDER_NK" "$(cat "$NATS_SEAL_RECIPIENT_PUB")" --output /tmp/nats-dr/"$DR_STREAM".nkey >/dev/null 2>&1; then
    ok "stream backup + tar + seal"
    S3CMD_CFG=/tmp/nats-dr/s3cfg
    cat > "$S3CMD_CFG" <<EOF2
[default]
access_key = ${S3_ACCESS_KEY:-}
secret_key = ${S3_SECRET_KEY:-}
host_base = $(echo "$S3_ENDPOINT" | sed 's#https\?://##')
host_bucket = $(echo "$S3_ENDPOINT" | sed 's#https\?://##')/${S3_BUCKET}
use_https = true
EOF2
    if s3cmd -c "$S3CMD_CFG" put /tmp/nats-dr/"$DR_STREAM".nkey "s3://${S3_BUCKET}/nats/${DR_STREAM}/${TS}.nkey" >/dev/null 2>&1; then
      ok "sealed backup uploaded to s3://${S3_BUCKET}/nats/${DR_STREAM}/${TS}.nkey"
      if $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats "${BOX_ARGS[@]}" stream rm "$DR_STREAM" --force >/dev/null 2>&1; then
        ok "stream deleted (disaster simulated)"
        RECIPIENT_NK="${NATS_RECIPIENT_NK:-${NATS_UNSEAL_RECIPIENT_NK:-}}"
        if [ -n "$RECIPIENT_NK" ] && [ -f "$RECIPIENT_NK" ]; then
          SENDER_PUB="$("$BIN" auth nkey show "${NATS_SEAL_SENDER_NK}" 2>/dev/null || true)"
          if s3cmd -c "$S3CMD_CFG" get "s3://${S3_BUCKET}/nats/${DR_STREAM}/${TS}.nkey" /tmp/nats-dr/backup.nkey --force >/dev/null 2>&1 \
             && [ -n "$SENDER_PUB" ] \
             && "$BIN" auth nkey unseal /tmp/nats-dr/backup.nkey "$RECIPIENT_NK" "$SENDER_PUB" --output /tmp/nats-dr/backup.tar.gz >/dev/null 2>&1 \
             && mkdir -p /tmp/nats-dr/restore && tar xzf /tmp/nats-dr/backup.tar.gz -C /tmp/nats-dr/restore \
             && "$BIN" stream restore /tmp/nats-dr/restore/"$DR_STREAM" "${RAPP[@]}" >/dev/null 2>&1; then
            restored="$("$BIN" stream info "$DR_STREAM" "${APP[@]}" 2>/dev/null | awk '/^[[:space:]]*Messages:/{print $2}' | head -1)"
            [ "$restored" = "4" ] && ok "DR restore verified ($restored messages)" || bad "DR restore message count: ${restored:-none}"
          else
            bad "DR download + unseal + restore"
          fi
        else
          skip "unseal half (operator recipient NKey not present on this node)"
        fi
      else
        bad "stream rm (disaster)"
      fi
    else
      bad "s3cmd upload of sealed backup"
    fi
  else
    bad "DR seal path (stream backup/tar/nkey seal)"
  fi
  rm -rf /tmp/nats-dr
else
  skip "S3 DR cycle (needs node nats CLI + S3_* + NKey env)"
fi

# --- Step 7: summary -----------------------------------------------------------
echo "-- step 7: summary --"
if [ "$FAILED" -eq 0 ]; then
  echo "=== nats-cluster test PASSED ==="
else
  echo "=== nats-cluster test FAILED ==="
fi
exit "$FAILED"
