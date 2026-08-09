# Deploying a NATS JetStream cluster with sdk-ops

YAML-driven NATS cluster deployment via `sdk-ops infra provision`. Validated on
a 3-node geo cluster (R3) and a 2-node cluster (R2) with a consumer-only node.

## 1. Declare the fleet

```yaml
hosts:
  - name: node-a
    host: 203.0.113.10
    peer_ip: 198.51.100.2            # private VLAN IP when present, else the public host
    user: root
    ssh_key: ~/.ssh/id_ed25519
    services:
      nats:
        profile: lite            # lite (1c/2GB) or rs (bigger)
        server_tags: [region:mia]
  - name: node-b
    host: 203.0.113.11
    peer_ip: 198.51.100.3
    user: root
    ssh_key: ~/.ssh/id_ed25519
    services:
      nats:
        profile: lite
        server_tags: [region:mia]

# A host WITHOUT `services:` runs no NATS — it can consume the cluster over
# the private network as a plain app host.

peers:
  - { from: node-a, to: node-b, ports: [4222, 6222] }
  - { from: node-b, to: node-a, ports: [4222, 6222] }
  # consumer-only host reaching the clients:
  - { from: app-01, to: node-a, ports: [4222] }
  - { from: app-01, to: node-b, ports: [4222] }
```

Secrets (passwords, certs, S3, nkeys) come from the environment/.env — never
the YAML. Run `sdk-ops infra provision provision.yaml --check` for a dry-run.

## 2. Profiles

`templates/nats-dockerized/profiles.yaml` sizes a node:

| Profile | max_connections | file_store | memory_store | mem_limit | cpus |
|---------|-----------------|-----------|--------------|-----------|------|
| `lite`  | 100             | 2GB       | 128MB        | 1g        | 1    |
| `rs`    | 1000            | 10GB      | 256MB        | 2g        | 2    |

## 3. KV buckets

The sdk-api creates KV buckets on first use with a Memory + 256MB + R1 default,
which exceeds a `lite` node's memory store. On a cluster, pre-create the
service's buckets with file storage and the cluster's replica count:

```
nats kv add <bucket> --replicas 3 --storage file
```

`sdk-api`'s `EnsureKeyValue` loads an existing bucket as-is, so the pre-created
config is kept. (Or set `Replicas` on the KVConfig when creating it in code.)

## 4. Connecting a microservice

`sdk-api` stream config (service.yaml) supports auth + mTLS:

```yaml
stream:
  - name: primary
    driver: nats
    url: "${NATS_URL}"              # tls://user:pass@host:4222 also works for auth
    user: "${NATS_USER}"
    password: "${NATS_PASSWORD}"
    ca_file: "${NATS_CA}"           # empty = system roots (public LE certs)
    cert_file: "${NATS_CERT}"
    key_file: "${NATS_KEY}"
```

Three validated topologies:
- **Remote**: app/tests from the operator to the public URLs.
- **Intra-VPS**: app on the same VPS as NATS, `tls://127.0.0.1:4222` (loopback).
- **Intra-VLAN**: app on a consumer-only VPS, connecting to the private IPs
  (no internet).

## 5. Gotchas

- **App user subjects**: the `app` user must be able to publish/subscribe the
  microservice's subjects. Set them via `NATS_APP_PUBLISH`/`NATS_APP_SUBSCRIBE`
  (comma-separated); the base allow set includes `$KV.>`, `links.>`,
  `nats-rpc.>`, `nats-pull.>`, `demo.>`, `events.>`, `$JS.*`, `_INBOX.>`.
- **Config reload**: the provision recreates the container when the rendered
  `nats.conf` changes (idempotent: a matching config leaves the container up).
- **Replica count**: a 3-node cluster → R3 streams; a 2-node cluster → R2
  (write quorum = both nodes). Use `--replicas` matching the node count.
- **Server cert SANs**: include the node IPs and `127.0.0.1` so intra-VPS
  loopback clients verify.
- **Mesh routes**: only hosts declaring the `nats` service appear in the mesh;
  consumer-only hosts are excluded automatically.

## 6. Client connectivity (how a microservice resolves the cluster)

There is **no proxy/LB in front of NATS** — the cluster (mesh + replication) IS
the layer. A client opens ONE connection to a member and the cluster routes
interest and replicates streams. How the URL is resolved:

| Form | URL | Notes |
|------|-----|-------|
| **External** (operator/app outside) | `tls://nts.example.com:4222` | geo-DNS → nearest node (or a server list of public IPs) |
| **Internal VLAN** (app on a consumer-only VPS) | `tls://198.51.100.2:4222,tls://198.51.100.3:4222` | server list of the private IPs |
| **Intra-VPS** (app on the same VPS as NATS) | `tls://127.0.0.1:4222` | loopback, reuses the node certs |

### Distribution (both forms documented)

- **Server list raw**: the nats.go client **randomizes the server pool by
  default** (`DontRandomize` disables it). Each client shuffles the list at
  connect, so a fleet spreads statistically across the nodes (~50/50 for 2)
  instead of piling onto the first. If the connected node dies, the client
  reconnects to the next in its pool. This is failover + distribution in one.
- **DNS round-robin (recommended for the internal VLAN)**: a DNS name (or a
  fleet `/etc/hosts` entry) that lists the private IPs round-robin — one URL to
  configure, the resolver spreads + fails over. Cleaner than embedding the list.

### Capacity is the real limit

The connection distribution avoids a single-node pile-up, but the **total
capacity** is bounded by each node's `max_connections` (profile `lite` = 100,
`rs` = 1000). A cluster of small nodes can not accept "99999 microservices" —
the server rejects the excess. Scale with bigger profiles, more nodes, or
connection pooling.

### A client subscribes once, never jumps

A microservice subscribes to its stream over its single connection. The cluster
propagates the subscription interest across the mesh and replicates the
streams, so it receives messages published on any node. The server list is only
the failover pool — the client does not bounce between servers per message.
