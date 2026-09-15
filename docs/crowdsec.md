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
(`CS_LAPI_URL` set: the local agent reports to a remote LAPI and the local
bouncer consumes its decisions — the multi-host / VLAN layout).

- **Automatic**: the agent parses the proxy access logs into scenarios; LAPI
  stores decisions; the bouncer (plugin or nftables) blocks matching clients.
- **Manual**: `cscli` for ad-hoc decisions (see below).
- k3s fleets use the in-cluster Traefik as the enforcement point; host/docker
  fleets use the template that matches the mode.

## Deployment matrix (validated)

| Layout | Engine | Enforcement | Status |
|---|---|---|---|
| k3s, in-cluster ingress | `crowdsec-cluster` | L7 plugin (stream; AppSec/CRS by profile) | validated |
| host, no proxy | `crowdsec-bare` standalone | L3/L4 nftables | validated |
| docker host, host Traefik | `crowdsec-dockerized` | L7 plugin on the sdk-ops Traefik (stream + auto-detection E2E; AppSec/CRS in-band on normal+) | validated |
| one engine, many enforcers (VLAN) | central `crowdsec-cluster` + clients `crowdsec-bare` | L3/L4 per client (the central decides) | validated |
| one engine, many enforcers (VLAN, docker) | central `crowdsec-dockerized` (`central: true`) + clients `crowdsec-dockerized` | L7 plugin per client (the central decides) | validated |

## Distributed layout (one engine, many enforcers)

The central engine processes (machines report their logs over the VLAN); every
client consumes its decisions and blocks locally:

```yaml
# central (k3s server): expose the LAPI and open it to the peers
services:
  crowdsec-cluster:
    profile: lite
# provision env: CS_K8S_LAPI_NODEPORT=30080
peers:
  - { from: edge-02, to: cp1, ports: [30080] }
```

```bash
# on the central, mint the per-client credentials
cscli machines add edge-02 --password <pw>      # the agent logs in with this
cscli bouncers add edge-02 -o raw               # the local bouncer key
```

```yaml
# clients (any mode): consume the central's decisions
services:
  crowdsec-bare:
    profile: lite
# provision env (per-host overrides let one fleet carry several clients):
#   CS_LAPI_URL=http://<cp1-vlan-ip>:30080
#   CS_LAPI_USER_EDGE_02 / CS_LAPI_PASSWORD_EDGE_02 / CS_BOUNCER_KEY_EDGE_02
```

Validated on the fleet (2026-09): the central lists both machines heartbeating
and both bouncers registered, and a decision added at the central is enforced
by the clients' nftables within ~20 s.

### Dockerized variant (L7 on every client)

Same shape, L7 enforcement per client instead of nftables:

```yaml
hosts:
  - name: central
    peer_ip: 192.0.2.10
    services:
      crowdsec-dockerized: { profile: lite, central: true }  # LAPI on the VLAN
  - name: web
    peer_ip: 192.0.2.11
    services:
      crowdsec-dockerized: { profile: lite }
peers:
  - { from: web, to: central, ports: [8080] }
```

```bash
# 1. provision once (no client env: central publishes, clients standalone)
# 2. on the central, mint per-client credentials (note -f: it must NOT
#    overwrite the central's own local_api_credentials.yaml)
sudo docker exec crowdsec cscli machines add web --password <pw> -f /tmp/x.yaml
sudo docker exec crowdsec cscli bouncers add web -o raw   # -> key
# 3. provision again with per-host env (the init collapses CS_*_<HOST>
#    onto the plain names at runtime, so one run carries both roles)
export CS_LAPI_URL_WEB=http://192.0.2.10:8080 CS_LAPI_USER_WEB=web \
  CS_LAPI_PASSWORD_WEB=<pw> CS_BOUNCER_KEY_WEB=<key>
```

Validated on the fleet (2026-09): all three machines heartbeating on the
central; a central ban returns 403 on both clients; a scan against a client is
detected by its engine, reported to the central (crowdsec-kind alert),
auto-banned there and enforced back on the client (403). Restoring standalone
needs `docker compose down -v` + removing `.env`/`bouncer.key` first (volumes
and credentials persist — see the template README).

## Topology coverage and gaps

| Topology | Covered |
|---|---|
| k3s multi-node over the VLAN (server + agents, flannel) | yes |
| Backends inside the k3s cluster (cross-node flannel) | yes |
| Client hosts consuming a central engine over the VLAN (`peer_ip`) | yes (distributed layout above) |
| Other VPS behind an edge, reachable **only** over the VLAN | **no — see the gaps** |

Two gaps block the "edge + backend VPS with no public IP" topology (tracked in
`known-issues.md`):

- **No remote router target**: Traefik routers point at `localhost:<port>`
  (host network) or at a container on the local docker network. There is no
  `target: http://<vlan-ip>:<port>` for a backend on another host.
- **No jump host**: the provisioner has no `ProxyJump`/bastion support, so a
  host without a public IP cannot be provisioned.

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
