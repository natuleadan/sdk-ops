# nats-dockerized — NATS JetStream cluster node template.

Deploys one node of a 3-node NATS cluster (R3) with TLS/mTLS, at-rest
encryption and per-service authz. One scalable template sized per node via
`profiles.yaml` (lite/rs); select it from provision.yaml:

```yaml
services:
  nats:
    profile: lite
```

## Render inputs (set at provision time, secrets from .env — never the YAML)

| Field | Source |
|---|---|
| `ServerName`, `Advertise`, `Routes` | provision.yaml hosts + peers (private VLAN or public IPs) |
| `MaxConnections`, `MaxFileStore`, `MaxMemoryStore`, `MemLimit`, `Cpus` | the selected profile |
| `ClusterName`, `JSKey` | provision `.env` |
| `AppPasswordHash`/`SvcPasswordHash`/`SysPasswordHash` | bcrypt of the user passwords from `.env` |
| node certs (server/ca + client app/sys/svc) | uploaded by the provision from the operator's cert store |

## Runtime layout on the node (`/opt/sdk-ops/services/nats/`)

- `nats.conf`, `docker-compose.yml` — rendered.
- `certs/` — server.pem/server.key/ca.pem + client certs.
- `.env` — the secrets (0600).
- `nats` — the NATS CLI.
- `backup.sh` / `backup-cron.sh` — daily JetStream seal → S3 (NKey curve).
- `restore.sh` — unseal + restore (operator side, needs the recipient NKey).
- `validate.sh` — health assertions.

## Security

- TLS-first + mTLS (`verify: true`), server cert per node.
- At-rest: JetStream `key` (ChaCha20-Poly1305) from `$JS_KEY`.
- Auth: bcrypt users (app/sys/svc) with scoped publish/subscribe.
- Cluster mesh (6222) TLS + routes over private VLAN or public IPs.

## Validated gotchas (3-node geo cluster, 2026-08)

- **App user permissions**: the `app` user must be able to publish/subscribe the
  microservice's subjects. Current allow list includes `$KV.>`, `links.>`,
  `nats-rpc.>`, `nats-pull.>`, `demo.>`, `events.>`, `$JS.*`, `_INBOX.>`.
  Adjust per service.
- **Config reload**: `docker compose up -d` does not restart a container when a
  mounted config changed — restart the NATS container after a `nats.conf` change.
- **KV buckets**: pre-create the service's buckets with
  `nats kv add <bucket> --replicas 3 --storage file` (the sdk-api default is
  Memory R1 256MB, which exceeds a `lite` node's 128MB memory store).
- **Server cert SANs**: include `127.0.0.1` for intra-VPS loopback clients.
- **Boot order**: each node lists all peers in its routes (full mesh), so the
  cluster forms regardless of boot order; gossip fills any missing route.
