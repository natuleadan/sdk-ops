# CrowdSec (WAF/IPS) — usage

CrowdSec is a **service** (not part of hardening): declare it in the fleet YAML
like any other datastore, one template per deployment mode. The engine is
installed from the **pinned** apt repo (`CS_VERSION`); the previous
`infra init --crowdsec` flag was **removed** — it installed the engine with no
bouncer (detection without enforcement) and was never YAML-driven.

| Template | Mode | Enforcement |
|---|---|---|
| `crowdsec-cluster` | k3s | L7 WAF via the Traefik bouncer plugin (stream, AppSec/CRS by profile) |
| `crowdsec-dockerized` | docker | L7 WAF: engine container + bouncer plugin auto-enabled on the sdk-ops Traefik |
| `crowdsec-bare` | any (host) | L3/L4: engine + nftables firewall bouncer |

Each template runs in **standalone** mode (local LAPI) or **client** mode
(`CS_LAPI_URL` set → the local agent reports to a remote LAPI and the local
bouncer consumes its decisions — the multi-host / VLAN layout).

- **Automatic**: the agent parses the proxy access logs into scenarios; LAPI
  stores decisions; the bouncer (plugin or nftables) blocks matching clients.
- **Manual**: `cscli` for ad-hoc decisions (see below).
- k3s fleets use the in-cluster Traefik as the enforcement point; host/docker
  fleets use the template that matches the mode.

> **Migration**: `infra init --crowdsec` no longer exists. Use a service
> declaration instead:
>
> ```yaml
> services:
>   crowdsec-bare:
>     profile: lite          # standalone (local LAPI + firewall bouncer)
> ```
>
> The old flag left a bare engine (`cscli`) with no bouncer and no acquisition
> config; to clean a host that got it, `sudo apt-get remove --purge crowdsec`
> (or redeclare the host — the provision uninstalls undeclared services).

## Day-to-day (cscli)

```bash
LAPI=$(sudo k3s kubectl -n crowdsec get pods -o name | grep lapi | head -1)

# list / add / delete decisions
sudo k3s kubectl -n crowdsec exec $LAPI -- cscli decisions list
sudo k3s kubectl -n crowdsec exec $LAPI -- cscli decisions add --ip 192.0.2.66 --duration 4h --reason "manual"
sudo k3s kubectl -n crowdsec exec $LAPI -- cscli decisions delete --ip 192.0.2.66

# alerts / scenarios / metrics
sudo k3s kubectl -n crowdsec exec $LAPI -- cscli alerts list
sudo k3s kubectl -n crowdsec exec $LAPI -- cscli metrics
sudo k3s kubectl -n crowdsec exec $LAPI -- cscli bouncers list
```

## Dashboards

CrowdSec has **no built-in web UI** — it is CLI-first. Options, from cheapest:

| Option | What | Notes |
|---|---|---|
| `cscli metrics` / `cscli alerts list` | ASCII metrics + alerts | zero setup, already available |
| **CrowdSec Console** (SaaS) | hosted fleet dashboard, free tier | enroll with `cscli console enroll <key>`; optional, needs egress 443 |
| **Metabase** (community) | `crowdsecurity/metabase-app` docker image + `crowdsec` driver | self-hosted dashboards on the LAPI DB |
| **Grafana** (community) | `crowdsecurity/grafana-dashboards` + Prometheus scraping LAPI `/metrics` | needs a Prometheus stack in the cluster |

LAPI exposes Prometheus metrics at `crowdsec-service.crowdsec.svc:6060/metrics`
(ClusterIP) for scraping.

## AppSec (WAF rules)

Profiles `normal`/`medium`/`large` enable the **AppSec** component with OWASP
CRS virtual patching. The bouncer middleware then also checks requests against
AppSec (blocking on CRS matches). `lite` stays stream-only (IP/behavior
decisions) to fit small nodes — AppSec adds ~200 MiB.

## Operations

- **Validate**: `sudo bash /opt/sdk-ops/services/crowdsec-cluster/validate.sh`
  (LAPI/agent/bouncer/middleware/netpols).
- **E2E test**: `sudo bash test/test.sh` (deploys a whoami route, bans the
  client pod IP, expects 403, unbans, expects 200).
- **Bouncer key**: stored in the `crowdsec-bouncer-key` secret; re-running
  init reuses it. The middleware references it (`crowdsecLapiKey`).
- **Uninstall**: standard service cleanup removes the release + namespace and
  drops the traefik addon override (restoring the default traefik).
- **Edge-first**: keep an edge WAF/CDN in front — the tiny nodes must not
  absorb a DDoS at the origin; CrowdSec decides per request (fail-open for
  availability).
