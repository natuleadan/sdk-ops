#!/bin/bash
# nats-dockerized integration test - exercises the node like an operator:
#   Step 0  env + binaries (nats CLI, app account)
#   Step 1  connectivity + server identity (name/cluster from nats.conf)
#   Step 2  JetStream lifecycle (add stream, publish, verify, read, delete)
#   Step 3  KV lifecycle (add bucket, put/get, delete)
#   Step 4  cluster health (peer count, jetstream replicas, peer probes)
#   Step 5  failover simulation (stop/start the local server)
#   Step 6  S3 DR cycle (seal -> S3 -> delete -> unseal -> restore)
#   Step 7  summary + exit code
#
# Run on a node as root/operator:  bash test/test.sh
# Full S3 DR requires S3_ENDPOINT/S3_BUCKET/S3_ACCESS_KEY/S3_SECRET_KEY plus,
# for the unseal half, the operator recipient NKey (NATS_RECIPIENT_NK or
# NATS_UNSEAL_RECIPIENT_NK, see restore.sh) - without it the unseal half is
# [SKIP]ped (the recipient NKey never leaves the operator's machine).
#
# The test uses the `app` account exactly like validate.sh (same APP=(...)
# invocation, incl. --tlsfirst --tlsca --tlscert --tlskey --user --password).
# The app account can publish/subscribe `demo.>`, `$KV.>`, `$JS.API.>` etc.
# (see cmd/sdk-ops/services.go natsRenderData) so every test subject lives
# under `demo.>` / `$KV.>`. `server info`/`server list` need the SYS account,
# so identity comes from the rendered nats.conf instead.
set -u

DIR="${NATS_DIR:-/opt/sdk-ops/services/nats}"
BIN="$DIR/nats"
ENV="$DIR/.env"
[ -f "$ENV" ] && . "$ENV"

# --- Step 0: env + binaries -------------------------------------------------
echo "=== nats test ==="
echo "-- step 0: env + binaries --"
if [ ! -x "$BIN" ]; then
  echo "  [FAIL] nats CLI not found at $BIN"
  echo "=== nats test FAILED ==="
  exit 1
fi
echo "  [OK] nats CLI: $BIN"

: "${NATS_URL:=tls://127.0.0.1:4222}" : "${NATS_USER:=app}" : "${NATS_PASSWORD:?}"
for f in "$DIR/certs/ca.pem" "$DIR/certs/app-cert.pem" "$DIR/certs/app-key.pem"; do
  if [ ! -f "$f" ]; then
    echo "  [FAIL] missing $f"
    echo "=== nats test FAILED ==="
    exit 1
  fi
done
echo "  [OK] app account env ($NATS_URL, user=$NATS_USER, certs present)"

APP=(--server "$NATS_URL" --tlsfirst --tlsca "$DIR/certs/ca.pem" --tlscert "$DIR/certs/app-cert.pem" --tlskey "$DIR/certs/app-key.pem" --user "$NATS_USER" --password "$NATS_PASSWORD")
# `nats stream restore` in 0.4.0 mis-parses space-separated global flags (it
# consumes the server URL as the <file> arg), so the restore call uses the
# `--flag=value` form in a separate array.
RAPP=(--server="$NATS_URL" --tlsfirst --tlsca="$DIR/certs/ca.pem" --tlscert="$DIR/certs/app-cert.pem" --tlskey="$DIR/certs/app-key.pem" --user="$NATS_USER" --password="$NATS_PASSWORD")
# Peer probes pass the peer's URL via `server check connection --server=`; the
# global `--server` would collide ("flag 'server' cannot be repeated"), so the
# peer array carries only the TLS + user auth flags (no --server).
PAPP=(--tlsfirst --tlsca "$DIR/certs/ca.pem" --tlscert "$DIR/certs/app-cert.pem" --tlskey "$DIR/certs/app-key.pem" --user "$NATS_USER" --password "$NATS_PASSWORD")

# Compose service name comes from docker-compose.yml (host networking, one
# service). Read it defensively: fall back to `nats` if the grep fails.
COMPOSE_FILE="$DIR/docker-compose.yml"
SERVICE_NAME="nats"
if [ -f "$COMPOSE_FILE" ]; then
  svc="$(awk '/^services:/{f=1;next} f&&/^  [a-zA-Z0-9_-]+:/{print $1; exit}' "$COMPOSE_FILE" 2>/dev/null)"
  [ -n "$svc" ] && SERVICE_NAME="${svc%:}"
fi
echo "  [OK] compose service: $SERVICE_NAME"

FAILED=0
WARNED=0

# --- Step 1: connectivity + server identity --------------------------------
echo "-- step 1: connectivity + server identity --"
if "$BIN" server check connection "${APP[@]}" >/dev/null 2>&1; then
  echo "  [OK] server check connection"
else
  echo "  [FAIL] server check connection"
  FAILED=1
fi

# Identity: the app account cannot query server info (SYS-only), so read the
# rendered config - it is what the running server was started with.
CONF="$DIR/nats.conf"
SERVER_NAME="?"
CLUSTER_NAME="?"
if [ -f "$CONF" ]; then
  SERVER_NAME="$(grep -E '^server_name:' "$CONF" | head -1 | awk '{print $2}')"
  CLUSTER_NAME="$(awk '/^cluster[[:space:]]*\{/{f=1} f&&/name:/{print $2; exit}' "$CONF" 2>/dev/null)"
  [ -n "$SERVER_NAME" ] || SERVER_NAME="?"
  [ -n "$CLUSTER_NAME" ] || CLUSTER_NAME="?"
fi
echo "  [OK] server name: $SERVER_NAME"
echo "  [OK] cluster name: $CLUSTER_NAME"
if [ "$SERVER_NAME" = "?" ] || [ "$CLUSTER_NAME" = "?" ]; then
  echo "  [WARN] could not parse identity from $CONF"
  WARNED=1
fi

# --- Step 2: JetStream lifecycle -------------------------------------------
echo "-- step 2: JetStream lifecycle --"
# Wait for JetStream to be ready (bounded; a freshly restarted node re-inits
# the store before answering JS API calls).
js_ready=0
for _i in $(seq 1 20); do
  if "$BIN" server check jetstream --timeout=3s "${APP[@]}" >/dev/null 2>&1; then
    js_ready=1
    break
  fi
  sleep 1
done
if [ "$js_ready" -ne 1 ]; then
  echo "  [FAIL] JetStream not ready after 20s"
  FAILED=1
fi
STREAM="SDKOPS_TEST"
# Idempotent: remove a leftover stream from a previous (crashed) run first.
"$BIN" stream rm "$STREAM" -f "${APP[@]}" >/dev/null 2>&1
if ! "$BIN" stream add "$STREAM" '--subjects=demo.>' --storage=file --replicas=1 --retention=limits --max-age=1h --discard=old --defaults "${APP[@]}" >/dev/null 2>&1; then
  echo "  [FAIL] stream add $STREAM"
  FAILED=1
else
  echo "  [OK] stream add $STREAM (file, R1, limits, 1h, discard old)"
  if "$BIN" pub 'demo.one' a1 "${APP[@]}" >/dev/null 2>&1 \
     && "$BIN" pub 'demo.two' a2 "${APP[@]}" >/dev/null 2>&1 \
     && "$BIN" pub 'demo.three' a3 "${APP[@]}" >/dev/null 2>&1; then
    echo "  [OK] published 3 messages (demo.one/two/three)"
  else
    echo "  [FAIL] publish to $STREAM"
    FAILED=1
  fi
  msgs="$("$BIN" stream info "$STREAM" "${APP[@]}" 2>/dev/null | awk '/^[[:space:]]*Messages:/{print $2}' | head -1)"
  if [ "$msgs" = "3" ]; then
    echo "  [OK] stream info: 3 messages"
  else
    echo "  [FAIL] stream info: expected 3 messages, got '${msgs:-none}'"
    FAILED=1
  fi
  if "$BIN" stream get "$STREAM" 1 "${APP[@]}" 2>/dev/null | grep -q '^a1$' \
     && "$BIN" stream get "$STREAM" 3 "${APP[@]}" 2>/dev/null | grep -q '^a3$'; then
    echo "  [OK] stream get: read back a1 (seq 1) and a3 (seq 3)"
  else
    echo "  [FAIL] stream get: could not read published payloads"
    FAILED=1
  fi
  if "$BIN" stream rm "$STREAM" -f "${APP[@]}" >/dev/null 2>&1; then
    echo "  [OK] stream rm $STREAM"
  else
    echo "  [FAIL] stream rm $STREAM"
    FAILED=1
  fi
fi

# --- Step 3: KV lifecycle ---------------------------------------------------
echo "-- step 3: KV lifecycle --"
BUCKET="sdkops_test_kv"
# Idempotent: drop a leftover bucket from a previous run.
"$BIN" kv del "$BUCKET" -f "${APP[@]}" >/dev/null 2>&1
if ! "$BIN" kv add "$BUCKET" --replicas=1 "${APP[@]}" >/dev/null 2>&1; then
  echo "  [FAIL] kv add $BUCKET"
  FAILED=1
else
  echo "  [OK] kv add $BUCKET (R1)"
  if "$BIN" kv put "$BUCKET" k1 v1 "${APP[@]}" >/dev/null 2>&1; then
    echo "  [OK] kv put k1 v1"
  else
    echo "  [FAIL] kv put"
    FAILED=1
  fi
  v="$("$BIN" kv get "$BUCKET" k1 --raw "${APP[@]}" 2>/dev/null)"
  if [ "$v" = "v1" ]; then
    echo "  [OK] kv get k1 -> v1"
  else
    echo "  [FAIL] kv get: expected v1, got '${v:-none}'"
    FAILED=1
  fi
  if "$BIN" kv del "$BUCKET" k1 -f "${APP[@]}" >/dev/null 2>&1; then
    echo "  [OK] kv del k1"
  else
    echo "  [FAIL] kv del k1"
    FAILED=1
  fi
  if "$BIN" kv del "$BUCKET" -f "${APP[@]}" >/dev/null 2>&1; then
    echo "  [OK] kv del $BUCKET (bucket removed)"
  else
    echo "  [FAIL] kv del $BUCKET"
    FAILED=1
  fi
fi

# --- Step 4: cluster health -------------------------------------------------
echo "-- step 4: cluster health --"
# Peer count from the rendered nats.conf `routes:` list (each route is one
# peer). NATS_ROUTES/NATS_PEERS env overrides when present (comma-separated).
PEERS=""
if [ -n "${NATS_ROUTES:-}" ]; then
  PEERS="$NATS_ROUTES"
elif [ -n "${NATS_PEERS:-}" ]; then
  PEERS="$NATS_PEERS"
elif [ -f "$CONF" ]; then
  PEERS="$(grep -oE 'nats://(\[[0-9a-fA-F:]+\]|[0-9A-Za-z.-]+)' "$CONF" 2>/dev/null | sed 's#nats://##; s/^\[//; s/\]$//' | tr '\n' ',' | sed 's/,$//')"
fi
if [ -n "$PEERS" ]; then
  n=$(echo "$PEERS" | tr ',' '\n' | grep -c .)
  echo "  [OK] peers detected from routes: $n ($(echo "$PEERS" | tr ',' ' '))"
else
  echo "  [SKIP] no peers detected (single node?)"
fi

# `nats server check cluster` does not exist in the pinned CLI 0.4.0
# (subcommands: connection stream consumer message meta request jetstream
# server kv credential) - SKIP it defensively instead of failing.
if "$BIN" server check cluster "${APP[@]}" >/dev/null 2>&1; then
  echo "  [OK] server check cluster"
else
  if "$BIN" server check --help 2>&1 | grep -q "check cluster"; then
    echo "  [FAIL] server check cluster"
    FAILED=1
  else
    echo "  [SKIP] server check cluster (not in this CLI version)"
  fi
fi

# JetStream cluster signal visible to the app account: `server check jetstream`
# reports replicas_ok=N from the account's streams.
js="$("$BIN" server check jetstream "${APP[@]}" 2>&1)"
if echo "$js" | grep -q "OK JetStream"; then
  echo "  [OK] server check jetstream"
  reps="$(echo "$js" | grep -oE 'replicas_ok=[0-9]+' | head -1)"
  [ -n "$reps" ] && echo "  [OK] $reps"
else
  echo "  [FAIL] server check jetstream"
  FAILED=1
fi

# Peer probes: only advisory. Peers may firewall the app account or the 4222
# port from this host, so a failure is [WARN], never [FAIL]. A peer entry may
# carry a port already (env NATS_ROUTES) or be a bare IP (routes in nats.conf
# are host:6222 mesh URLs - probe the client port 4222).
PEER_CHECK() {
  local p="$1" url
  case "$p" in
    \[*\]:*) url="tls://$p" ;;           # already bracketed with port
    *:*:*)   url="tls://[$p]:4222" ;;    # bare IPv6
    *:*)     url="tls://$p" ;;           # host:port
    *)       url="tls://$p:4222" ;;      # bare host
  esac
  "$BIN" server check connection --timeout=3s --server="$url" "${PAPP[@]}" >/dev/null 2>&1
}
if [ -n "$PEERS" ]; then
  for peer in $(echo "$PEERS" | tr ',' ' '); do
    if PEER_CHECK "$peer"; then
      echo "  [OK] peer reachable: $peer"
    else
      echo "  [WARN] peer not reachable from this host (may be firewalled): $peer"
      WARNED=1
    fi
  done
fi

# --- Step 5: failover simulation (local server only) ------------------------
echo "-- step 5: failover simulation --"
if ! command -v docker >/dev/null 2>&1; then
  echo "  [SKIP] docker not available on this host"
else
  if ! docker compose -f "$COMPOSE_FILE" stop "$SERVICE_NAME" >/dev/null 2>&1; then
    echo "  [FAIL] docker compose stop $SERVICE_NAME"
    FAILED=1
  else
    echo "  [OK] local server stopped ($SERVICE_NAME)"
    # While the local server is down, a peer must still serve the app account.
    if [ -n "$PEERS" ]; then
      peer_ok=0
      for peer in $(echo "$PEERS" | tr ',' ' '); do
        if PEER_CHECK "$peer"; then
          peer_ok=1
          break
        fi
      done
      if [ "$peer_ok" -eq 1 ]; then
        echo "  [OK] peer still reachable during local outage"
      else
        echo "  [WARN] no peer reachable during local outage (single node or firewalled)"
        WARNED=1
      fi
    else
      echo "  [SKIP] no peers configured (single node)"
    fi
    if ! docker compose -f "$COMPOSE_FILE" start "$SERVICE_NAME" >/dev/null 2>&1; then
      echo "  [FAIL] docker compose start $SERVICE_NAME"
      FAILED=1
    else
      echo "  [OK] local server restarted ($SERVICE_NAME)"
      # Poll until the local server answers again (JetStream re-init can take
      # a few seconds). Timeout 60s. Each check is bounded with --timeout so a
      # half-up server cannot stall the loop.
      up=0
      for i in $(seq 1 30); do
        if "$BIN" server check connection --timeout=2s "${APP[@]}" >/dev/null 2>&1 \
           && "$BIN" server check jetstream --timeout=3s "${APP[@]}" >/dev/null 2>&1; then
          up=1
          break
        fi
        sleep 2
      done
      if [ "$up" -eq 1 ]; then
        echo "  [OK] local server healthy again (polled ${i} rounds)"
      else
        echo "  [FAIL] local server did not recover within 60s"
        FAILED=1
      fi
      if "$BIN" pub 'demo.postfailover' ok "${APP[@]}" >/dev/null 2>&1; then
        echo "  [OK] pub/sub works after failover"
      else
        echo "  [FAIL] pub/sub broken after failover"
        FAILED=1
      fi
    fi
  fi
fi

# --- Step 6: S3 DR cycle -----------------------------------------------------
echo "-- step 6: S3 DR cycle --"
# Full DR needs S3 creds; the unseal half additionally needs the operator
# recipient NKey (present only on the operator side - see restore.sh). Without
# creds the whole step is skipped; with creds but no NKey we verify seal+S3
# upload only and document the unseal half as [SKIP] (still PASS).
if [ -z "${S3_BUCKET:-}" ] || [ -z "${S3_ACCESS_KEY:-}" ] || [ -z "${S3_SECRET_KEY:-}" ]; then
  echo "  [SKIP] S3 DR (S3_BUCKET/S3_ACCESS_KEY/S3_SECRET_KEY not set)"
elif ! command -v s3cmd >/dev/null 2>&1; then
  echo "  [SKIP] S3 DR (s3cmd required for upload/download/ls)"
else
  : "${S3_ENDPOINT:=s3.us-east-005.backblazeb2.com}"
  # Scheme-aware endpoint: a bare host defaults to https (production S3); an
  # explicit scheme (http:// or https://) is kept as-is (local/MinIO testing).
  case "$S3_ENDPOINT" in
    *://*) S3_URL="$S3_ENDPOINT" ;;
    *) S3_URL="https://$S3_ENDPOINT" ;;
  esac
  : "${S3_PREFIX:=nats}"
  DR_STREAM="SDKOPS_DR_TEST"
  TS="$(date +%Y%m%d-%H%M%S)"
  WORK="/tmp/nats-dr-$TS"
  mkdir -p "$WORK"
  # Idempotent: drop a leftover DR stream from a previous run.
  "$BIN" stream rm "$DR_STREAM" -f "${APP[@]}" >/dev/null 2>&1
  # S3 via the host s3cmd (minio/mc is not on Docker Hub anymore): the
  # work dir is local, no container mount needed.
  # S3 key layout mirrors backup.sh: s3://$S3_BUCKET/$S3_PREFIX/$stream/$TS.nkey
  S3_KEY="$S3_PREFIX/$DR_STREAM/$TS.nkey"
  DR_OK=0

  if ! "$BIN" stream add "$DR_STREAM" '--subjects=demo.dr.>' --storage=file --replicas=1 --retention=limits --max-age=1h --discard=old --defaults "${APP[@]}" >/dev/null 2>&1; then
    echo "  [FAIL] DR stream add $DR_STREAM"
    FAILED=1
  else
    echo "  [OK] DR stream add $DR_STREAM (2 msgs)"
    "$BIN" pub demo.dr.one d1 "${APP[@]}" >/dev/null 2>&1
    "$BIN" pub demo.dr.two d2 "${APP[@]}" >/dev/null 2>&1

    # Seal path (mirrors backup.sh for a single stream): stream backup to a
    # local dir, tar it, seal with the sender NKey for the recipient public
    # key, then upload the sealed blob with mc (docker).
    if "$BIN" stream backup "$DR_STREAM" "$WORK/$DR_STREAM" --consumers "${APP[@]}" >/dev/null 2>&1 \
       && tar czf "$WORK/$DR_STREAM.tar.gz" -C "$WORK" "$DR_STREAM" \
       && [ -n "${NATS_SEAL_SENDER_NK:-}" ] && [ -n "${NATS_SEAL_RECIPIENT_PUB:-}" ] \
       && "$BIN" auth nkey seal "$WORK/$DR_STREAM.tar.gz" "$NATS_SEAL_SENDER_NK" "$(cat "$NATS_SEAL_RECIPIENT_PUB")" --output "$WORK/$DR_STREAM.nkey" >/dev/null 2>&1; then
      echo "  [OK] stream backup + tar + nkey seal"
      if [ -s "$WORK/$DR_STREAM.nkey" ]; then
        echo "  [OK] sealed backup present locally ($(wc -c < "$WORK/$DR_STREAM.nkey") bytes)"
      else
        echo "  [FAIL] sealed backup empty"
        FAILED=1
      fi
      if s3cmd put "$WORK/$DR_STREAM.nkey" "s3://$S3_BUCKET/$S3_KEY" >/dev/null 2>&1; then
        echo "  [OK] sealed backup uploaded to s3://$S3_BUCKET/$S3_KEY"
        DR_OK=1
      else
        echo "  [FAIL] s3cmd upload of sealed backup"
        FAILED=1
      fi
    else
      echo "  [FAIL] DR seal path (stream backup/tar/nkey seal)"
      FAILED=1
    fi

    # Delete the stream: the disaster.
    if "$BIN" stream rm "$DR_STREAM" -f "${APP[@]}" >/dev/null 2>&1; then
      echo "  [OK] stream deleted (disaster simulated)"
    else
      echo "  [FAIL] DR stream rm"
      FAILED=1
    fi

    # Unseal + restore. The recipient NKey lives on the operator's machine, so
    # on the node this is either wired in (NATS_RECIPIENT_NK /
    # NATS_UNSEAL_RECIPIENT_NK) or we verify the upload landed and stop.
    RECIPIENT_NK="${NATS_RECIPIENT_NK:-${NATS_UNSEAL_RECIPIENT_NK:-}}"
    # The sender's public key for unseal is derived from the sender NKey that
    # lives on this node (nats auth nkey show prints the public key).
    SENDER_NK="${NATS_SEAL_SENDER_NK:-}"
    SENDER_PUB=""
    [ -n "$SENDER_NK" ] && [ -f "$SENDER_NK" ] && SENDER_PUB="$("$BIN" auth nkey show "$SENDER_NK" 2>/dev/null)"
    if [ "$DR_OK" -eq 1 ] && [ -n "$RECIPIENT_NK" ] && [ -f "$RECIPIENT_NK" ]; then
      if [ -n "$SENDER_PUB" ]; then
        if s3cmd get "s3://$S3_BUCKET/$S3_KEY" "$WORK/backup.nkey" >/dev/null 2>&1 \
           && "$BIN" auth nkey unseal "$WORK/backup.nkey" "$RECIPIENT_NK" "$SENDER_PUB" --output "$WORK/backup.tar.gz" >/dev/null 2>&1 \
           && mkdir -p "$WORK/restore" \
           && tar xzf "$WORK/backup.tar.gz" -C "$WORK/restore" \
           && "$BIN" stream restore "$WORK/restore/$DR_STREAM" "${RAPP[@]}" >/dev/null 2>&1; then
          echo "  [OK] download + unseal + restore"
          restored="$("$BIN" stream info "$DR_STREAM" "${APP[@]}" 2>/dev/null | awk '/^[[:space:]]*Messages:/{print $2}' | head -1)"
          if [ "$restored" = "2" ]; then
            echo "  [OK] DR stream restored with 2 messages"
            DR_OK=2
          else
            echo "  [FAIL] DR stream restored but message count is '${restored:-none}'"
            FAILED=1
          fi
        else
          echo "  [FAIL] DR download + unseal + restore"
          FAILED=1
        fi
      else
        echo "  [SKIP] unseal (sender public key not derivable on this host)"
      fi
    else
      # Partial verification: the sealed blob is local and the upload landed.
      echo "  [SKIP] unseal (operator NKey not on this host)"
      if s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/$DR_STREAM/" 2>/dev/null | grep -q "$TS"; then
        echo "  [OK] S3 upload confirmed (s3cmd ls s3://$S3_BUCKET/$S3_PREFIX/$DR_STREAM/$TS.nkey)"
      else
        echo "  [FAIL] S3 upload not found via s3cmd ls"
        FAILED=1
      fi
    fi

    # Cleanup: delete the DR stream (if restored) and the S3 test object.
    if "$BIN" stream info "$DR_STREAM" "${APP[@]}" >/dev/null 2>&1; then
      "$BIN" stream rm "$DR_STREAM" -f "${APP[@]}" >/dev/null 2>&1 && echo "  [OK] DR stream cleaned up"
    fi
    if s3cmd del "s3://$S3_BUCKET/$S3_KEY" >/dev/null 2>&1; then
      echo "  [OK] S3 test object removed"
    fi
    rm -rf "$WORK"
  fi
fi

# --- Step 7: summary + exit code --------------------------------------------
echo "-- step 7: summary --"
echo "  failures: $FAILED"
echo "  warnings: $WARNED"
if [ "$FAILED" -ne 0 ]; then
  echo "=== nats test FAILED ==="
  exit 1
fi
echo "=== nats test PASSED ==="
exit 0
