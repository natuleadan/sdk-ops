# kv-dockerized — Dragonfly KV Full Stack

Dragonfly cluster (1 primary + 2 replicas) with HAProxy TLS (single entrypoint) and automatic snapshot backups.

## Services

| Role | Internal port | TLS | Description |
|------|:------------:|:---:|-------------|
| **HAProxy** | **6379** | ✅ | **Entrypoint** — round_robin primary + rep-1 + rep-2 |
| **Primary** | 6379 (int) | ❌ | Read/write, all hash slots |
| **Replica-1** | 6380 (int) | ❌ | Read-only, follows primary |
| **Replica-2** | 6381 (int) | ❌ | Read-only, follows primary |
| **MinIO** | 9000 | ❌ | S3 storage (profile: s3) |

## Quick start

```bash
cd /path/to/kv-dockerized
bash init.sh
```

## Connect

```bash
# Single entrypoint — HAProxy distributes via round-robin
redis-cli --tls --cacert ssl/ca.crt -h <VPS_IP> -p 6379 -a dragonfly
```

## Backups

```bash
bash backup.sh              # BGSAVE → local + MinIO
bash backup-cron.sh          # daily cron at 3 AM
```

## Restore

```bash
bash restore.sh backup-20260712.dfs          # Full restore
bash restore.sh --yes backup-20260712.dfs     # Skip confirmation
```

## Validate

```bash
bash validate.sh
```

## Test

```bash
bash test/test.sh       # PITR cycle: SET → BGSAVE → FLUSHALL → restore → verify
```

## Env vars

| Variable | Default | Description |
|----------|---------|-------------|
| `DF_PASSWORD` | `dragonfly` | Auth password |
| `S3_ENDPOINT` | — | MinIO/S3 endpoint |
| `S3_BUCKET` | `kv-backups` | S3 bucket name |
| `S3_KEY` | `minioadmin` | S3 access key |
| `S3_SECRET` | `minioadmin` | S3 secret key |

## Architecture

```
                          ┌──────────────┐
                          │   Clients     │
                          └──────┬───────┘
                                 │ 6379 (único puerto expuesto)
                          ┌──────▼───────┐
                          │   HAProxy      │  round_robin
                          │   TLS term     │  server primary
                          │                │  server rep-1
                          └──┬────────┬────┘  server rep-2
                             │        │
                     ┌───────▼──┐ ┌───▼──────┐ ┌───▼──────┐
                     │ Primary   │ │ Rep-1    │ │ Rep-2    │
                     │ :6379     │ │ :6380    │ │ :6381    │
                     │ cluster   │ │ replica  │ │ replica  │
                     └───────────┘ └──────────┘ └──────────┘
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Dragonfly primary + 2 replicas + HAProxy + MinIO |
| `init.sh` | SSL + start + REPLICAOF + DFLYCLUSTER CONFIG |
| `validate.sh` | Health check (PING, role, replication, TLS via HAProxy) |
| `backup.sh` | BGSAVE → local dir + S3 (MinIO) |
| `restore.sh` | Restore from .dfs snapshot (--yes flag, dir/file modes) |
| `backup-cron.sh` | Daily backup cron |
| `haproxy.cfg` | Round-robin TLS termination |
| `gen-certs.sh` | EC key generation + PEM for HAProxy |
| `dragonfly-primary.conf` | Reference config for primary |
| `dragonfly-replica.conf` | Reference config for replica |
| `test/test.sh` | PITR integration test |
