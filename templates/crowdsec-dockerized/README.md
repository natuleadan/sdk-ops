# crowdsec-dockerized - CrowdSec + Traefik plugin (docker hosts)

Runs the **CrowdSec engine** as a container and enables the **Traefik bouncer
plugin on the sdk-ops host Traefik automatically** — no manual Traefik edit.
It is the L7 WAF for docker-mode fleets: the plugin asks the LAPI per request
(stream mode) and Traefik answers 403 on a decision.

## What the init does

1. Checks the sdk-ops host Traefik exists (it is the enforcement point).
2. Patches the persistent Traefik creation template
   (`/opt/sdk-ops/traefik/install.sh`): enables the plugin
   (`experimental.plugins.crowdsec-bouncer`), turns on the access log to
   `/var/log/traefik/access.log`, attaches the `crowdsec@file` middleware to
   both entrypoints and mounts the shared log dir. Then recreates the
   container so the flags apply (the watchdog rebuilds it with the plugin too).
3. Brings the engine container up on the shared `sdk-ops-net` network.
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

Client of a central LAPI (the VLAN layout):

```bash
export CS_LAPI_URL=http://192.0.2.20:30080
export CS_LAPI_USER=web
export CS_LAPI_PASSWORD=...
export CS_BOUNCER_KEY=...        # cscli bouncers add web (on the central)
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
- The plugin downloads from `plugins.traefik.io` at Traefik startup: the host
  needs **egress 443**; without it Traefik logs "Plugins are disabled" and
  `init.sh` retries the container once.
- Re-provisioning rewrites `/etc/traefik/traefik.yml` and the creation
  template; the service init re-applies the plugin flags afterwards, so the
  order is always converge (services run after the traefik phase).
- The middleware is dynamic (file provider, `watch: true`): domain changes do
  not restart Traefik. Only the plugin load needs the container recreate.
- Uninstall removes the container + the middleware file; the plugin flags in
  the creation template are left in place (harmless) — re-run `infra init` to
  rebuild a clean template.
