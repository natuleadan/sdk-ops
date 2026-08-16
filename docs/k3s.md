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
