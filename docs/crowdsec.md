# CrowdSec (WAF/IPS) — usage

`sdk-ops` deploys CrowdSec via the `crowdsec-cluster` template: **LAPI + agent**
(helm) plus the **Traefik bouncer plugin** wired to the `web`/`websecure`
entrypoints, and default-deny NetworkPolicies for the namespace. Everything is
internal (ClusterIP); the only public surface stays the ingress.

- **Automatic**: the agent parses traefik access logs (the HelmChartConfig
  enables them) into scenarios; LAPI stores decisions; the Traefik plugin
  (stream mode, 15 s refresh) blocks matching clients with HTTP 403.
- **Manual**: `cscli` inside the LAPI pod for ad-hoc decisions (see below).
- No host-level CrowdSec is installed on k3s fleets — the enforcement point is
  the in-cluster Traefik. (Host/docker fleets use the host `--crowdsec` init.)

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
