# crowdsec-cluster — CrowdSec WAF/IPS inside k3s

Deploys **CrowdSec** (LAPI + agent) in the cluster via the official helm chart
and wires the **Traefik bouncer plugin**, so every request routed through the
in-cluster ingress passes the IPS (behavior/community decisions) and, with the
`normal` profile or above, the **AppSec WAF** (OWASP CRS virtual patching).
Default-deny NetworkPolicies are applied to the namespace. **Internal only** —
ClusterIP services, no host ports.

## Install (fleet YAML, granular)

```yaml
mode: k3s
hosts:
  - name: cp1
    host: 192.0.2.10
    role: server                 # must run on a k3s server host (kubectl + helm)
    services:
      crowdsec-cluster:
        profile: lite            # lite = stream (IPS) | normal+ = AppSec WAF on
```

```bash
sdk-ops infra provision crowdsec.yaml --insecure
```

Raw commands (ON the node, under `/opt/sdk-ops/services/crowdsec-cluster/`):
`bash init.sh` · `bash validate.sh` · `bash test/test.sh`.

## How the Traefik wiring works

- **k3s native addon (default)**: `init.sh` applies a `HelmChartConfig` that
  enables `experimental.plugins.crowdsec-bouncer` and attaches the middleware
  to the `web`/`websecure` entrypoints (access logs on). The plugin downloads
  from GitHub on traefik start (nodes need egress 443).
- **External traefik**: enable the same plugin + entrypoint middlewares in its
  own values (see the crowdsec docs); the middleware created here still works.
- **No traefik**: CrowdSec runs standalone (validate skips the bouncer checks).

The middleware (`<namespace>-traefik-bouncer`) uses **stream mode**: decisions
are fetched from the LAPI every 15 s. The bouncer key is generated once and
kept in the `crowdsec-bouncer-key` secret (re-running init reuses it).

## Distributed layout (central LAPI, clients over the VLAN)

One cluster host runs the engine; other hosts (bare/docker, no engine of their
own) **consume its decisions**:

1. Expose the LAPI on a NodePort and open it to the peers:

   ```yaml
   # on the k3s server host
   services:
     crowdsec-cluster:
       profile: lite
   # env when provisioning: CS_K8S_LAPI_NODEPORT=30080
   peers:
     - { from: edge-02, to: cp1, ports: [30080] }   # block form in the YAML
     - { from: edge-03, to: cp1, ports: [30080] }
   ```

2. On the client hosts declare `crowdsec-bare` in **client mode** with
   `CS_LAPI_URL=http://<cp1-vlan-ip>:30080`, the machine credentials and a
   bouncer key created on the central (`cscli bouncers add <node>`).

The central processes (parses the clients' logs, decides); each client's local
bouncer pulls the decisions and blocks locally. See `docs/crowdsec.md`.

## Edge-first (DDoS)

This stack is the **origin** layer: CrowdSec decides per request (cheap).
Front it with an edge WAF/CDN (rate-limit + bot/DDoS absorption) so the tiny
nodes never see the flood — the forwarded client IP is trusted only from the
internal ranges (`forwardedHeadersTrustedIPs`).

## App namespaces

`netpol-app-example.yaml` is the copy-paste default-deny pattern for an app
namespace (ingress only from traefik, egress only DNS + the datastore
namespaces). Apply per namespace with `kubectl apply -f ... -n <app-ns>`.

## Profiles

| Profile | LAPI | Agent | AppSec (WAF) |
|---|---|---|---|
| `lite` | 100m / 96Mi | 100m / 64Mi | no (stream IPS only) |
| `normal` | 250m / 256Mi | 250m / 128Mi | yes (CRS) |
| `medium` | 500m / 512Mi | 500m / 256Mi | yes (CRS) |
| `large` | 1 / 1Gi | 1 / 512Mi | yes (CRS) |

## Uninstall

Declared-service cleanup removes the release + namespace automatically; a
manual removal also drops the traefik addon override:
`helm uninstall crowdsec -n crowdsec && kubectl delete helmchartconfig traefik -n kube-system`.

## Gotchas

- Must be declared on a **k3s server host** (the template uses `k3s kubectl`).
- Helm comes from the fleet provision (pinned); the template only verifies it.
- The plugin is a **community** traefik plugin (not first-party CrowdSec).
- `lite` + AppSec off keeps the 2 GiB nodes comfortable; enable AppSec only
  where the node has headroom.
- The init installs **two phased**: the LAPI first (agents disabled), and only
  then enables the agents. The agents' registration init does not give up when
  the LAPI is missing, and on small nodes that retry loop saturates the disk
  and can take the kube-apiserver down — never start agents before the LAPI.
