#!/bin/bash
# crowdsec-dockerized init - run the CrowdSec engine as a container and wire
# the Traefik bouncer plugin into the sdk-ops host Traefik automatically:
#   - the plugin is enabled through the persistent Traefik creation template
#     (/opt/sdk-ops/traefik/install.sh) + a container recreate, so a vanished
#     container is rebuilt WITH the plugin by the watchdog;
#   - the middleware lives in the file provider (/etc/traefik/conf.d), so
#     domain changes never restart Traefik;
#   - the Traefik access log is enabled to a shared dir the engine parses.
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

# 1. The sdk-ops host Traefik is the enforcement point.
if ! sudo docker inspect traefik >/dev/null 2>&1; then
  fail "traefik container not found (this template needs the sdk-ops host Traefik)"
fi

# 2. Shared access-log dir + plugin enablement in the persistent template.
sudo mkdir -p /var/log/traefik
INSTALL=/opt/sdk-ops/traefik/install.sh
[ -f "$INSTALL" ] || fail "traefik install template missing ($INSTALL)"
PLUGIN_MODULE="github.com/maxlerebourg/crowdsec-bouncer-traefik-plugin"
PLUGIN_FLAGS="--experimental.plugins.crowdsec-bouncer.modulename=$PLUGIN_MODULE --experimental.plugins.crowdsec-bouncer.version=$PLUGIN_VERSION --accesslog.filepath=/var/log/traefik/access.log --entrypoints.web.http.middlewares=crowdsec@file --entrypoints.websecure.http.middlewares=crowdsec@file -v /var/log/traefik:/var/log/traefik"
if ! sudo grep -q "crowdsec-bouncer" "$INSTALL"; then
  log "patching the traefik creation template (plugin + access log)"
  sudo sed -i "s# traefik:v3.2 # $PLUGIN_FLAGS traefik:v3.2 #" "$INSTALL"
fi
if ! sudo docker inspect traefik 2>/dev/null | grep -q "crowdsec-bouncer"; then
  log "recreating traefik so the plugin loads"
  sudo docker rm -f traefik >/dev/null 2>&1 || true
  sudo bash "$INSTALL"
fi
for i in $(seq 1 20); do
  if sudo docker ps -q -f name=traefik >/dev/null 2>&1 && [ -n "$(sudo docker ps -q -f name=traefik)" ]; then break; fi
  sleep 2
done

# 3. Engine container (joins the shared docker network for the plugin).
cd "$DIR"
sudo docker compose up -d
for i in $(seq 1 30); do
  if sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1; then break; fi
  sleep 2
done
sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1 || fail "engine LAPI not answering"
for c in $COLLECTIONS; do
  sudo docker exec crowdsec cscli collections install "$c" >/dev/null 2>&1 || log "warn: collection $c not installed"
done

# 4. Mode wiring: bouncer key (standalone) or remote credentials (client).
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
  # The plugin talks to the CENTRAL LAPI (host:port from CS_LAPI_URL).
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
  case "$(sudo docker inspect traefik 2>/dev/null | grep -o '"NetworkMode": *"[^"]*"' | head -1 || true)" in
    *host*) LAPI_HOST="127.0.0.1:8080" ;;
    *)      LAPI_HOST="crowdsec:8080" ;;
  esac
  log "standalone mode: bouncer $BOUNCER registered (plugin -> $LAPI_HOST)"
fi

# 5. Middleware in the Traefik file provider (dynamic: no Traefik restart).
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
log "middleware crowdsec@file written (entrypoints web/websecure)"

# 6. Confirm the plugin actually loaded.
sleep 3
if sudo docker logs traefik --tail=400 2>&1 | grep -qi "Plugins are disabled"; then
  log "warn: plugin download failed this boot - recreating traefik once"
  sudo docker rm -f traefik >/dev/null 2>&1 || true
  sudo bash "$INSTALL"
  sleep 5
fi
if sudo docker logs traefik --tail=400 2>&1 | grep -qi "crowdsec-bouncer\|Plugins are enabled"; then
  log "plugin loaded"
else
  log "warn: plugin load not confirmed in the traefik log (egress 443 needed)"
fi

sudo docker ps --filter name=crowdsec --filter name=traefik
log "node ready ($BOUNCER)"
