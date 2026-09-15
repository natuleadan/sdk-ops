# crowdsec-dockerized - CrowdSec + Traefik plugin (docker hosts)

Runs the **CrowdSec engine** as a container and enables the **Traefik bouncer
plugin on the sdk-ops host Traefik automatically** — no manual Traefik edit.
It is the L7 WAF for docker-mode fleets: the plugin asks the LAPI per request
(stream mode) and Traefik answers 403 on a decision.

## What the init does

1. Checks the sdk-ops host Traefik exists (it is the enforcement point).
2. Patches the persistent Traefik creation template
   (`/opt/sdk-ops/traefik/install.sh`): runs the binary directly
   (`--entrypoint traefik`, the image entrypoint otherwise swallows every
   flag), mounts the shared log dir. The plugin registry, the access log
   (JSON, so headers are captured) and the `crowdsec@file` entrypoint
   middlewares live in `/etc/traefik/traefik.yml` (config file, not CLI
   flags). Then recreates the container so it all applies (the watchdog
   rebuilds it the same way).
3. Brings the engine container up on the shared `sdk-ops-net` network and
   installs the hub collections (including `crowdsecurity/traefik`, the
   access-log parser — without it nothing is detected; the engine restarts
   once when new collections land so the parsers load).
4. Registers a bouncer (`cscli bouncers add <node>`) and writes the middleware
   into the Traefik file provider (`/etc/traefik/conf.d/01-crowdsec.yml`).
5. The engine parses the Traefik access log (acquisition shipped as
   `acquis-traefik.yaml` inside the container).

## Install (fleet YAML, granular)

```yaml
mode: docker
hosts:
  - name: web
    host: 192.0.2.10
    services:
      crowdsec-dockerized:
        profile: lite
```

Central LAPI + remote clients (the VLAN layout): the central publishes the
LAPI on its peer IP, clients report to it and consume its decisions.

```yaml
hosts:
  - name: central
    peer_ip: 192.0.2.10
    services:
      crowdsec-dockerized: { profile: lite, central: true }
  - name: web
    peer_ip: 192.0.2.11
    services:
      crowdsec-dockerized: { profile: lite }
peers:
  - { from: web, to: central, ports: [8080] }
```

```bash
# 1. provision once (central publishes 192.0.2.10:8080, clients standalone)
# 2. on the central: register each client machine + bouncer
sudo docker exec crowdsec cscli machines add web --password <pw> -f /tmp/x.yaml
sudo docker exec crowdsec cscli bouncers add web -o raw   # -> key
# 3. provision again with PER-HOST env (one run carries central + clients;
#    the init collapses CS_*_<HOST> onto the plain names at runtime)
export CS_LAPI_URL_WEB=http://192.0.2.10:8080 CS_LAPI_USER_WEB=web \
  CS_LAPI_PASSWORD_WEB=<pw> CS_BOUNCER_KEY_WEB=<key>
```

Raw commands (ON the node, under `/opt/sdk-ops/services/crowdsec-dockerized/`):
`bash init.sh` · `bash validate.sh` · `bash test/test.sh`.

## Profiles

| Profile | Engine | Notes |
|---|---|---|
| `lite` | 256M / 0.5c | stream IPS |
| `normal` | 512M / 1c | stream IPS |
| `large` | 1G / 2c | stream IPS |

AppSec (OWASP CRS) is available through the plugin's `crowdsecAppsecEnabled`
option; the cluster template enables it per profile (AppSec adds ~200 MiB).

## Gotchas

- **Requires the sdk-ops host Traefik**: this template wires the plugin into
  it. A custom/third-party proxy needs the plugin enabled in its own config.
- The middleware file is written atomically (temp + rename) and Traefik is
  recreated when its content changes: an in-place rewrite can be caught torn
  by the file provider watch, which drops the middleware until restart.
- The plugin downloads from `plugins.traefik.io` at Traefik startup: the host
  needs **egress 443**; without it Traefik logs "Plugins are disabled" and
  `init.sh` retries the container once.
- Scenarios count DISTINCT requests: hammering one URL never overflows a
  bucket. CrowdSec also whitelists loopback/RFC1918 by default, so node-local
  traffic can never trigger scenarios. The acceptance test removes the
  whitelist parser for its scan and restores it right after (trap-guarded;
  validate fails when it is missing).
- Re-provisioning rewrites `/etc/traefik/traefik.yml` and the creation
  template; the service init re-applies the plugin flags afterwards, so the
  order is always converge (services run after the traefik phase).
- The middleware is dynamic (file provider, `watch: true`): domain changes do
  not restart Traefik. Only the plugin load needs the container recreate.
- Uninstall removes the container + the middleware file; the plugin wiring in
  the creation template and `traefik.yml` is left in place (harmless) — re-run
  `infra init` to rebuild a clean template. Volumes (`cc-config`, `cc-data`),
  `.env` and `bouncer.key` persist: switching a node between standalone and
  client mode needs `docker compose down -v` + removing those files first,
  otherwise stale credentials survive.
