# libsql-dockerized — libSQL Full Stack

libSQL (sqld) cluster with 1 primary + 2 replicas, HAProxy TLS (single entrypoint), WAL-based incremental snapshots, and optional MinIO.

## Services

| Role | Port | TLS | Description |
|------|:----:|:---:|-------------|
| **HAProxy** | **8443** | ✅ | **Entrypoint** — round_robin primary + rep-1 + rep-2 |
| **sqld-primary** | 8080 (int) | ❌ | Read/write, gRPC :5001 |
| **sqld-replica-1** | 8081 (int) | ❌ | Read-only, gRPC to primary |
| **sqld-replica-2** | 8082 (int) | ❌ | Read-only, gRPC to primary |
| **MinIO** | 9000/9001 | ❌ | S3 storage (profile: s3) |

## Quick start

```bash
cd /path/to/libsql-dockerized
bash init.sh
```

## Connect

```bash
# Single entrypoint — HAProxy distributes via round-robin
curl -d '{"statements":["SELECT * FROM items"]}' https://<VPS_IP>:8443
```

## Backups

```bash
bash backup.sh              # Full .db + WAL snapshots
bash backup-cron.sh          # Daily cron at 4 AM
```

## Restore

```bash
bash restore.sh backup-20260712.db          # Full restore
bash restore.sh --yes backup-20260712.db     # Skip confirmation
```

## Validate

```bash
bash validate.sh
```

## Test

```bash
bash test/test.sh       # PITR cycle: CREATE → INSERT → DROP → restore → verify
```

## Env vars

| Variable | Default | Description |
|----------|---------|-------------|
| `S3_ENDPOINT` | — | MinIO/S3 endpoint |
| `S3_BUCKET` | `libsql-backups` | S3 bucket name |
| `S3_KEY` | `minioadmin` | S3 access key |
| `S3_SECRET` | `minioadmin` | S3 secret key |

## Architecture

```
                           ┌──────────────┐
                           │   Clients     │
                           └──────┬───────┘
                                  │ 8443 (único puerto expuesto, TLS)
                           ┌──────▼───────┐
                           │   HAProxy      │  round_robin
                           │   TLS term     │  server primary
                           │                │  server rep-1
                           └──┬────────┬────┘  server rep-2
                              │        │
                     ┌────────▼──┐ ┌───▼──────┐ ┌───▼──────┐
                     │ Primary    │ │ Rep-1    │ │ Rep-2    │
                     │ :8080      │ │ :8081    │ │ :8082    │
                     │ gRPC:5001  │ │ gRPC ↓   │ │ gRPC ↓   │
                     └────────────┘ └──────────┘ └──────────┘
                     WAL snapshots every 30s → ./backups/
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | sqld primary + 2 replicas + HAProxy + MinIO |
| `Dockerfile` | Custom image (adds curl for health checks) |
| `init.sh` | TLS + start + schema creation |
| `validate.sh` | Health check (HTTP, SQL, vector, TLS via HAProxy) |
| `backup.sh` | Copy .db + WAL snapshots |
| `restore.sh` | Restore from .db backup (--yes flag) |
| `backup-cron.sh` | Daily backup cron |
| `haproxy.cfg` | Round-robin TLS termination |
| `gen-certs.sh` | EC key generation + PEM for HAProxy |
| `snapshot.sh` | Callback for --snapshot-exec |
| `libsql-primary.conf` | Reference config for primary |
| `libsql-replica.conf` | Reference config for replica |
| `test/test.sh` | PITR integration test |
