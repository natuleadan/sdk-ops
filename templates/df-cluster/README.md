# df-cluster

DragonflyDB in k3s via the official `dragonflydb/dragonfly-operator`. The
operator reconciles the `Dragonfly` CR (dragonflydb.io/v1alpha1): a master +
replicas with operator-managed recovery (a fast master pod restart is
re-adopted; a sustained failure fails over to a replica), and the service
`<name>.<ns>.svc.cluster.local` always points at the current master.

NOTE: this is DragonflyDB (redis-compatible store) — not dragonflyoss (the
P2P image distribution system; same name, different product).

- Pinned image (`DF_K8S_TAG`, default v1.30.1), operator pinned to a release
  manifest (no `:latest` anywhere).
- Native S3 snapshots via `spec.snapshot.dir` (dragonfly >= v1.12) when the
  operator env carries `S3_*`; explicit DR cycle always available through
  `backup`/`restore` (BGSAVE -> kubectl cp -> s3cmd, and back). The S3 key
  prefix is `DF_S3_PREFIX` (default `df`) and `S3_ENDPOINT` may be given with
  or without scheme (normalized for Dragonfly's `--s3_endpoint`).

## Deploy (fleet YAML)

```yaml
mode: k3s
hosts: [...]
services:
  df-cluster:
    profile: lite   # lite | normal | medium | large
```

## Commands

| command | what |
|---|---|
| `init` | operator manifest (pinned) + Dragonfly CR + wait replicas |
| `validate` | pods ready + CR + master PING + replication state |
| `test` | 6-step integration (lifecycle, replication, S3 DR, failover) |
| `backup` / `restore` | BGSAVE -> cp -> S3 (and back) |

Clients connect to `redis://<name>.<ns>.svc.cluster.local:6379` with
`DF_PASSWORD` (env — never in the YAML).
