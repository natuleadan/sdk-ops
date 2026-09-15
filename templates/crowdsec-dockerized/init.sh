#!/bin/bash
# crowdsec-dockerized init - run the CrowdSec engine as a container and wire
# the Traefik bouncer plugin into the sdk-ops host Traefik automatically.
#
# The Traefik side is wired through the STATIC CONFIG FILE (/etc/traefik/traefik.yml):
# the plugin registry, the access log and the entrypoint middlewares. CLI flags
# are deliberately NOT used: the traefik image entrypoint re-injects `traefik`,
# so the container command becomes `traefik traefik --flags...` and Go stops
# parsing flags at the positional argument (silently ignoring all of them).
# The middleware itself lives in the file provider (/etc/traefik/conf.d).
#
# Standalone (local LAPI) or client mode (CS_LAPI_URL): in client mode the
# agent reports to a remote LAPI and the plugin consumes its decisions.
set -e

DIR="/opt/sdk-ops/services/crowdsec-dockerized"
IMAGE_TAG="{{ .ImageTag }}"
PLUGIN_VERSION="{{ .PluginVersion }}"
COLLECTIONS="{{ .Collections }}"
BOUNCER="{{ .BouncerName }}"
CLIENT="{{ if .Client }}1{{ else }}0{{ end }}"
# Secrets (client mode) written by the provision with umask 077.
# shellcheck disable=SC1091
[ -f "$DIR/.env" ] && . "$DIR/.env"

log() { echo "[crowdsec-dockerized] $1"; }
fail() { echo "[crowdsec-dockerized] FAIL: $1"; exit 1; }

echo "=== crowdsec-dockerized init ==="
log "image crowdsecurity/crowdsec:$IMAGE_TAG  plugin $PLUGIN_VERSION  client=$CLIENT"

# 1. Preconditions: the sdk-ops host Traefik is the enforcement point.
sudo mkdir -p /var/log/traefik
INSTALL=/opt/sdk-ops/traefik/install.sh
[ -f "$INSTALL" ] || fail "traefik install template missing ($INSTALL) - needs the sdk-ops host Traefik"
PLUGIN_MODULE="github.com/maxlerebourg/crowdsec-bouncer-traefik-plugin"
# Host-network Traefik reaches the published loopback LAPI; bridged Traefik
# reaches the engine container by name on the shared network.
if sudo grep -q -- "--network host" "$INSTALL"; then
  LAPI_HOST_DEFAULT="127.0.0.1:8080"; PROBE_BACKEND="http://127.0.0.1:8080/"
else
  LAPI_HOST_DEFAULT="crowdsec:8080"; PROBE_BACKEND="http://crowdsec:8080/"
fi

# 2. Engine container (joins the shared docker network for the plugin).
cd "$DIR"
sudo docker compose up -d
for i in $(seq 1 30); do
  if sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1; then break; fi
  sleep 2
done
sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1 || fail "engine LAPI not answering"
NEWCOLL=0
for c in $COLLECTIONS; do
  out="$(sudo docker exec crowdsec cscli collections install "$c" 2>&1)" || log "warn: collection $c not installed"
  echo "$out" | grep -qi "enabling" && NEWCOLL=1
done
if [ "$NEWCOLL" = 1 ]; then
  # The daemon loads parsers/scenarios at startup: restart once so the newly
  # enabled collections actually parse (otherwise lines stay unparsed).
  log "new collections enabled - restarting the engine to load parsers"
  sudo docker restart crowdsec >/dev/null
  for i in $(seq 1 30); do
    if sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1; then break; fi
    sleep 2
  done
  sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1 || fail "engine LAPI not answering after restart"
fi

# 3. Mode wiring: bouncer key (standalone) or remote credentials (client).
if [ "$CLIENT" = "1" ]; then
  LAPI_URL="${CS_LAPI_URL:-}"
  [ -n "$LAPI_URL" ] || fail "client mode needs CS_LAPI_URL"
  [ -n "${CS_BOUNCER_KEY:-}" ] || fail "client mode needs CS_BOUNCER_KEY (create on the central)"
  sudo docker exec crowdsec sh -c "cat > /etc/crowdsec/local_api_credentials.yaml" <<EOF
url: ${LAPI_URL}
login: ${CS_LAPI_USER:-}
password: ${CS_LAPI_PASSWORD:-}
EOF
  sudo docker restart crowdsec >/dev/null
  LAPI_KEY="$CS_BOUNCER_KEY"
  LAPI_SCHEME="$(printf '%s' "$LAPI_URL" | sed -E 's#^(https?)://.*#\1#')"
  LAPI_HOST="$(printf '%s' "$LAPI_URL" | sed -E 's#^https?://##; s#/$##')"
  log "client mode: agent + plugin -> $LAPI_URL"
else
  if sudo docker exec crowdsec cscli bouncers list -o json 2>/dev/null | grep -q "\"$BOUNCER\""; then
    LAPI_KEY="$(sudo cat "$DIR/bouncer.key" 2>/dev/null || true)"
    if [ -z "$LAPI_KEY" ]; then
      sudo docker exec crowdsec cscli bouncers delete "$BOUNCER" >/dev/null 2>&1 || true
      LAPI_KEY="$(sudo docker exec crowdsec cscli bouncers add "$BOUNCER" -o raw 2>/dev/null | tr -d '\r' | head -1 || true)"
    fi
  else
    LAPI_KEY="$(sudo docker exec crowdsec cscli bouncers add "$BOUNCER" -o raw 2>/dev/null | tr -d '\r' | head -1 || true)"
  fi
  [ -n "$LAPI_KEY" ] || fail "could not register bouncer $BOUNCER"
  sudo sh -c "umask 077; printf '%s' '$LAPI_KEY' > '$DIR/bouncer.key'"
  LAPI_SCHEME="http"
  LAPI_HOST="$LAPI_HOST_DEFAULT"
  log "standalone mode: bouncer $BOUNCER registered (plugin -> $LAPI_HOST)"
fi

# 4. Middleware in the Traefik file provider (dynamic: no Traefik restart).
sudo mkdir -p /etc/traefik/conf.d
sudo tee /etc/traefik/conf.d/01-crowdsec.yml > /dev/null <<EOF
http:
  middlewares:
    crowdsec:
      plugin:
        crowdsec-bouncer:
          enabled: true
          crowdsecMode: stream
          updateIntervalSeconds: 15
          crowdsecLapiScheme: $LAPI_SCHEME
          crowdsecLapiHost: $LAPI_HOST
          crowdsecLapiPath: /
          crowdsecLapiKey: $LAPI_KEY
          forwardedHeadersTrustedIPs:
{{- range .TrustedCIDRs }}
            - {{ . }}
{{- end }}
EOF
sudo chmod 0644 /etc/traefik/conf.d/01-crowdsec.yml
log "middleware crowdsec@file written"

# 5. Probe router: a plain web (:80) router so the entrypoint middleware is
#    exercised by the acceptance test without a TLS certificate (websecure has
#    no cert for the probe host). The host is a documentation TLD sent with
#    `curl -H Host: ...`; the backend is the engine LAPI (the middleware
#    answers first when the client is banned).
sudo tee /etc/traefik/conf.d/02-waf-probe.yml > /dev/null <<EOF
http:
  routers:
    waf-probe:
      rule: "Host(\"waf-probe.invalid\")"
      entryPoints:
        - web
      service: waf-probe
  services:
    waf-probe:
      loadBalancer:
        servers:
          - url: "$PROBE_BACKEND"
EOF
sudo chmod 0644 /etc/traefik/conf.d/02-waf-probe.yml
log "probe router written (web/waf-probe.invalid -> $PROBE_BACKEND)"

# 6. Wire the plugin into the static config (idempotent):
#    - run the binary directly (`--entrypoint traefik`) so --configFile and any
#      flag are honored instead of swallowed by the image entrypoint;
#    - register the plugin + enable the access log;
#    - attach the middleware to the web/websecure entrypoints.
YML=/etc/traefik/traefik.yml
CHANGED=0
# 6a. Normalize the creation template: drop any legacy CLI plugin flags (the
#     wiring now lives in traefik.yml), make sure the shared access-log volume
#     is mounted (the engine reads the file it writes) and run the binary
#     directly (`--entrypoint traefik`) so --configFile is honored.
BEFORE="$(sudo md5sum "$INSTALL" | awk '{print $1}')"
sudo sed -i -E \
  -e 's# --experimental\.plugins\.crowdsec-bouncer\.[a-z]+=[^ ]*##g' \
  -e 's# --accesslog\.filepath=[^ ]*##g' \
  -e 's# --entrypoints\.(web|websecure)\.http\.middlewares=[^ ]*##g' "$INSTALL"
if ! sudo grep -q -- "-v /var/log/traefik:/var/log/traefik" "$INSTALL"; then
  sudo sed -i "s# traefik:v3.2 # -v /var/log/traefik:/var/log/traefik traefik:v3.2 #" "$INSTALL"
fi
if ! sudo grep -q -- "--entrypoint traefik" "$INSTALL"; then
  log "traefik template: run the binary directly (entrypoint bypass)"
  sudo sed -i "s# -d --name traefik # -d --entrypoint traefik --name traefik #" "$INSTALL"
fi
[ "$BEFORE" != "$(sudo md5sum "$INSTALL" | awk '{print $1}')" ] && CHANGED=1
if ! sudo grep -q "crowdsec-bouncer" "$YML"; then
  log "traefik.yml: enabling the plugin + access log"
  sudo tee -a "$YML" >/dev/null <<EOF

experimental:
  plugins:
    crowdsec-bouncer:
      moduleName: $PLUGIN_MODULE
      version: $PLUGIN_VERSION
accessLog:
  filePath: /var/log/traefik/access.log
EOF
  CHANGED=1
fi
if ! sudo grep -q "crowdsec@file" "$YML"; then
  log "traefik.yml: attaching crowdsec@file to web/websecure"
  sudo sed -i '/^    address: ":80"$/a\    http:\n      middlewares:\n        - crowdsec@file' "$YML"
  sudo sed -i '/^    address: ":443"$/a\    http:\n      middlewares:\n        - crowdsec@file' "$YML"
  CHANGED=1
fi
if [ "$CHANGED" = 1 ] || ! sudo docker inspect traefik >/dev/null 2>&1; then
  log "recreating traefik so the plugin loads"
  sudo docker rm -f traefik >/dev/null 2>&1 || true
  sudo bash "$INSTALL"
fi
for i in $(seq 1 20); do
  if [ -n "$(sudo docker ps -q -f name=traefik)" ]; then break; fi
  sleep 2
done

# 7. Confirm the plugin loaded (egress 443 to plugins.traefik.io needed).
sleep 4
if sudo docker logs traefik --tail=400 2>&1 | grep -qi "Plugins are disabled"; then
  log "warn: plugin download failed this boot - recreating traefik once"
  sudo docker rm -f traefik >/dev/null 2>&1 || true
  sudo bash "$INSTALL"
  sleep 5
fi
if sudo docker logs traefik --tail=400 2>&1 | grep -qi "Plugins loaded\|crowdsec-bouncer"; then
  log "plugin loaded"
else
  log "warn: plugin load not confirmed in the traefik log"
fi

sudo docker ps --filter name=crowdsec --filter name=traefik
log "node ready ($BOUNCER)"
  format: json
# 6b. Migrate older nodes to the JSON access log: CLF drops the headers (no
#     User-Agent), so crowdsec parses the paths but never the bad-UA scenario.
#     JSON keeps every header; truncate so the file has a single format.
if ! sudo grep -q "format: json" "$YML"; then
  log "traefik.yml: switching the access log to JSON (headers captured)"
  sudo sed -i '/^accessLog:$/a\  format: json' "$YML"
  sudo truncate -s 0 /var/log/traefik/access.log 2>/dev/null || true
  CHANGED=1
fi
