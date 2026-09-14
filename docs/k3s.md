# k3s clusters with sdk-ops (mesh, registry images, TLS)

The declarative path for the fleet: a k3s cluster (control-plane + agents)
provisioned by `sdk-ops apply` — YAML/CLI-driven, no hardening for drills.

## The declarative command

```bash
sdk-ops apply provision.yaml --insecure
```

`mode: k3s` installs docker + k3s on every host. The FIRST host becomes the
**server** (control-plane); the rest join as **agents** with the server token.
`hardening: false` skips the OS hardening (drills); the default runs it first.

```yaml
mode: k3s
hardening: false
no_traefik: true          # k3s bundles its own traefik (ingress) — the host
                          # one is not installed (no double traefik)
hosts:
  - name: node-01         # server
    host: <ip>
    peer_ip: <ip>
  - name: node-02         # agent
    host: <ip>
    peer_ip: <ip>
peers:
  - { from: node-01, to: node-02, ports: [6443, 8472, 10250, 2379, 2380] }
```

The mesh: the flannel overlay (VXLAN — UDP 8472) between the nodes; the
server exposes the API on 6443 + the embedded etcd on 2379/2380.

## The agents (join)

The `apply` installs the server. The agents join with the token:

```bash
sdk-ops infra join <server-ip> <agent-ip> --token <token> --insecure
```

The token lives at `/var/lib/rancher/k3s/server/node-token` on the server. The
join is idempotent (the installer rewrites the agent unit); a previous
standalone server on the agent must be cleaned first (the units + the state).

## High-availability (HA) mode

`k3s_ha: true` in the provision YAML turns the fleet into an HA cluster: the
first host boots with `--cluster-init` (embedded etcd) on its `peer_ip`, every
following host joins as an **HA server** (not agent) via
`https://<first-peer-ip>:6443`. 3 servers = etcd quorum 2/3.

```yaml
k3s_ha: true
k3s_iface: ens19           # flannel binds to the VLAN NIC
hosts:
  - name: node-01
    host: 192.0.2.10
    peer_ip: 192.0.2.20    # the private/VLAN address — flannel + etcd peer
  - name: node-02
    host: 192.0.2.11
    peer_ip: 192.0.2.21
  - name: node-03
    host: 192.0.2.12
    peer_ip: 192.0.2.22
```

**Critical flags must be written BEFORE the install.** The provision writes
`/etc/rancher/k3s/config.yaml` with `cluster-init: true`, `flannel-iface`,
and `node-ip` before running the k3s install script. Without this, the
installer starts a single-node cluster and the join creates 3 independent
servers instead of one HA cluster.

**Join form** (manual, if needed):
```bash
# On each subsequent server node:
sh -s - server --server https://<first-peer-ip>:6443
```

The provision handles this automatically; the join form is documented for
manual recovery.

**Traefik note**: the k3s traefik addon is disabled only with
`k3s_disable_traefik: true` (default: enabled). `no_traefik` is a different
knob — it skips the HOST-level docker traefik (for bare/docker hosts), not the
in-cluster ingress.

## Roles (control-plane / worker)

Every host can declare its cluster role explicitly. When any host declares a
role, the fleet builds ONE cluster: servers form the control plane and roleless
hosts (or `role: agent`) are workers.

```yaml
mode: k3s
k3s_iface: ens19
hosts:
  - name: cp1
    host: 192.0.2.10
    peer_ip: 192.0.2.20
    role: server            # control-plane (boots the cluster)
  - name: w1
    host: 192.0.2.11
    peer_ip: 192.0.2.21
    role: agent             # worker (K3S_URL join)
```

- **One server** = standalone control plane (sqlite) + workers.
- **More than one server** requires `k3s_ha: true` (embedded-etcd quorum);
  the provision rejects it otherwise.
- Hosts without `role` in a roles fleet default to **agent**.
- A **dedicated control-plane node** is just a host whose role is `server`
  (the future external-CP topology: one server elsewhere + agents on the app
  nodes). Without any role, the legacy behavior applies (`k3s_ha`: all
  servers; otherwise an independent single-node k3s per host).

## Helm (centralized)

The provision installs a **pinned helm** (`v3.15.4`) on every k3s host during
the tuning phase — one binary version for the whole fleet. The helm-based
service templates (`nats-cluster`, `etcd-cluster`, `crowdsec-cluster`) only
VERIFY it (`command -v helm` + version check); they never download helm
themselves. Re-provisioning is idempotent (a matching version is a no-op).

## Hardening + k3s (firewall, peers, kubelet)

With `hardening: true` the host input chain is default-deny (nftables); only
22/80/443/6443 stay open to the world. A k3s fleet needs the cluster ports
between the nodes — declare them in `peers` (the fleet YAML is the single
source of truth):

- `8472/udp` — flannel vxlan (cross-node pod traffic), every pair.
- `10250/tcp` — kubelet: the apiserver dials it for exec/logs and
  metrics-server scrapes it; every pair.
- `2379`, `2380`, `6443` — only when `k3s_ha: true` (embedded etcd peering).

The provision also opens `10250` from each node's own addresses (`peer_ip` and
`host`) **and from the pod network** (`10.42.0.0/16`<!-- go-check:ignore-ip -->): the local apiserver
reaches the kubelet through the node address, and a metrics-server pod on the
same node scrapes it from its pod IP (same-node traffic is not SNATed) — the
scrape fails with `<unknown>` without that rule. None of this is an exposure:
the kubelet keeps its TLS client auth + authorization (k3s disables anonymous
auth).

Peers are **always** direct persisted rules in the `input` chain, and every
application first purges stale allowlist state for the port (an old `exposed`
accept+drop entry shadows the local accepts, and the 5-minute state watchdog
re-applies registry entries — a `dport 10250 drop` there hangs every
`kubectl exec`). `allowlist expose` refuses the peer ports (8472, 2379, 2380,
10250) for the same reason. Audit anytime with:

```bash
sudo nft list chain inet filter exposed   # must stay empty for peer ports
kubectl exec <pod> -- true                # hangs => the exposed chain is poisoned
```

## The registry images (private/public)

The Deployment references the image from a registry (GHCR, VCR, ...) with an
`imagePullSecrets` for private images:

```yaml
spec:
  imagePullSecrets:
    - name: registry-token     # the docker-registry Secret (the read token)
  containers:
    - name: app
      image: registry.example.com/org/app:latest
      imagePullPolicy: Always
```

```bash
kubectl create secret docker-registry registry-token \
  --docker-server=registry.example.com \
  --docker-username=<user> --docker-password=<token>
```

The image is built elsewhere (CI / the operator machine — `--platform
linux/amd64`) and pushed; the nodes only pull — no builds on the fleet.

## TLS (automatic)

`infra cert install --domain <d> --runtime k3s` does everything:

1. Installs the **cert-manager** operator (if the CRD is missing).
2. Creates the **ClusterIssuer** `letsencrypt-prod` (ACME HTTP-01 via traefik).
3. Creates the **Certificate** in the server manifests (the cert + the secret).

The Ingress only needs the annotation + the secret name:

```yaml
metadata:
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
    - hosts: [app.example.com]
      secretName: app-example-com-tls
```

## When to use it

| Size / case | Model |
|---|---|
| ≤3 nodes · edge · drills | docker (the `docs/pg.md` model) |
| ≥4 nodes · production HA · declarative scaling | **k3s + an operator** (e.g. CloudNativePG for postgres) |

The k3s mode is the declarative path: the scaling is `kubectl scale` /
`kubectl patch` — no per-node wiring.

## Gotchas

- **The init marker** (`/opt/sdk-ops/.version`): an existing marker skips the
  full init ("already initialized") — the k3s never installs. Remove it to
  force a re-init when switching the node mode.
- **The image platform**: the build host may be ARM (e.g. a Mac) — build with
  `--platform linux/amd64`, otherwise the nodes fail with "no match for
  platform in manifest".
- **The server node RAM**: the control-plane (etcd + apiserver + flannel +
  traefik) is heavy (~1GB base) — a small plan needs swap; keep the builds off
  the fleet nodes.
- **No double traefik**: the k3s mode does not install the host traefik — the
  cluster uses its own ingress controller (`no_traefik` for clarity).

## Datastore services inside k3s (per-template, granular)

The datastore services run inside the cluster as their own templates — deploy
each one **granularly** with its fleet YAML, or declare the ones that must
coexist together in a single YAML (the provision uninstalls the services a
YAML does not declare):

| Service | Template | What it runs |
|---|---|---|
| PostgreSQL HA | `pgsql-cnpg` | CloudNativePG operator + Cluster CR: primary + replicas, `-rw`/`-ro` services, barman S3 backups + PITR |
| Valkey Cluster | `valkey-cluster` | 6 cluster-enabled nodes: 3 primaries + 3 replicas, 16384 shards slots, cluster-native failover |
| Dragonfly | `df-cluster` | dragonflydb operator (master + replicas), native S3 snapshots |
| NATS JetStream | `nats-cluster` | official helm chart (R3) + NACK CRDs (Stream/Consumer/KV) |
| etcd (DCS) | `etcd-cluster` | bitnami helm chart, 3 replicas |

```bash
set -a; . env/.env; set +a            # VK_PASSWORD / DF_PASSWORD / S3_* (never in the YAML)
sdk-ops provision svc.yaml --check    # dry-run: parse + render, no changes
sdk-ops provision svc.yaml            # deploy / update (idempotent init)
```

Size them with the template **profiles** (`profile: lite|normal|medium|large`)
and remember the nodes are shared: several services asking full cores will not
schedule on the small plans.

Services install **strictly in queue**: each one finishes its init (workloads
ready) before the next starts, plus a short settle pause so image pulls and IO
drain instead of stacking up (`SDKOPS_SERVICE_SETTLE` seconds, default 10, 0
disables). Dependency order first (postgres before the rest), then the
template order.

Changing the profile of a live service works in place only when the change does
**not** alter its storage size: k3s `local-path` cannot resize PVCs, so a size
change (up or down) fails the helm/CR apply with "only dynamically provisioned
pvc can be resized". To change the storage, delete the PVC (the service
re-bootstraps its data) or expand manually on a storage class that supports it.
The crowdsec profiles keep a constant LAPI storage for this reason.

DR: every template ships `backup-s3.sh` / `restore-s3.sh` (per-shard RDB for
valkey, barman PITR for cnpg, native snapshots for df, nkey-sealed streams for
nats) — see each `templates/<name>/README.md`.

S3 conventions (shared by the templates and their DR scripts):

- `S3_ENDPOINT` may be given with or without scheme; each consumer normalizes
  it (df's `--s3_endpoint` takes the bare host, pg composes `https://` from
  it, the scripts strip it for `s3cfg`).
- Key prefix per service: `DF_S3_PREFIX` (df), `PG_S3_PREFIX` (pg),
  `VK_S3_PREFIX` (valkey), `NATS_S3_PREFIX` (nats) — never a shared
  `S3_PREFIX` (it leaked one service's prefix into another service's `.env`).
- The df/pg inits install `s3cmd` and write `~/.s3cfg` from the env, so the
  explicit backup/restore scripts work on a fresh node.
- **Clean the prefix before a fresh deploy**: barman refuses to archive over an
  existing store ("Expected empty archive") and CNPG then retries in a loop
  that saturates the node (io wait 100%, apiserver timeouts, hung
  `kubectl exec`). The pg init fails fast with the exact cleaning command; a
  re-provision of a live cluster skips the check.
- The acceptance tooling ships with the service: `validate.sh` and `test/`
  land in `/opt/sdk-ops/services/<name>/` after a provision.

## WAF/IPS inside k3s (`crowdsec-cluster`)

`crowdsec-cluster` deploys CrowdSec (LAPI + agent) via helm and wires the
**Traefik bouncer plugin**: when the native k3s traefik addon is present,
`init.sh` applies a `HelmChartConfig` (`experimental.plugins.crowdsec-bouncer`
+ the middleware attached to the `web`/`websecure` entrypoints), creates the
bouncer (`traefik-bouncer`) and the middleware, and applies default-deny
NetworkPolicies to the namespace. `lite` profile = stream IPS (IP/behavior
decisions); `normal`+ enables the **AppSec WAF** (OWASP CRS).

```yaml
services:
  crowdsec-cluster:
    profile: lite      # declare it on a k3s SERVER host (uses k3s kubectl + helm)
```

Validate/e2e: `validate.sh` (LAPI/agent/bouncer/middleware/netpols) and
`test/test.sh` (decision lifecycle + an end-to-end block through traefik:
a client pod gets 200, is banned, gets 403, and gets 200 again after the
unban). Keep it behind an edge WAF/CDN so the tiny nodes never absorb a
DDoS at the origin. Usage guide (cscli, dashboards, AppSec): `docs/crowdsec.md`.

## YugabyteDB inside k3s (operator)

The `yugabyte-k8s-operator` manages a cluster inside k3s declaratively via a
helm chart — the same role CloudNativePG plays for postgres. The template
provides `deploy-k3s.sh` (run from the operator machine, `kubectl` pointed at
the k3s cluster):

```bash
helm repo add yugabytedb https://charts.yugabyte.com
helm upgrade --install yb-demo yugabytedb/yugabyte \
  --namespace yb-demo \
  --set Image.tag=2026.1.1.1-b2 \
  --set replicas.master=1,replicas.tserver=1
```

Microservices in the cluster consume yugabyte over the internal service DNS
(`yb-master.yb-demo` / `yb-tserver.yb-demo`) — no host ports are exposed; the
operator sets up the services and the replication (RF) between masters.

