# kv-full-bm — Dragonfly KV Full Stack

Single-node Dragonfly cluster with primary-replica replication, TLS, and automatic snapshot backups.

## Services

| Role | Port | TLS | Admin (no TLS) | Description |
|------|:----:|:---:|:---------------|-------------|
| **Primary** | 6379 | ✅ | 10001 | Read/write, all hash slots |
| **Replica** | 6380 | ✅ | 10002 | Read-only, follows primary |
| **MinIO** | 9000 | ❌ | 9001 (console) | S3-compatible storage (profile: s3) |

## Quick start

```bash
cd /path/to/kv-full-bm
bash init.sh
```

## Connect

```bash
# Copy CA cert first
scp root@<VPS_IP>:/path/to/kv-full-bm/ssl/ca.crt ./kv-ca.crt

# Primary (read/write)
redis-cli --tls --cacert ./kv-ca.crt -h <VPS_IP> -p 6379 -a dragonfly

# Replica (read-only)
redis-cli --tls --cacert ./kv-ca.crt -h <VPS_IP> -p 6380 -a dragonfly
```

## Backups

```bash
bash backup.sh              # BGSAVE → local + MinIO
bash backup-cron.sh          # daily cron at 3 AM
```

Built-in `--snapshot_cron=*/30 * * * *` saves snapshots to Docker volumes automatically.

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
| `PRIMARY_IP` | `127.0.0.1` | Primary IP (for REPLICAOF) |
| `S3_ENDPOINT` | — | MinIO/S3 endpoint |
| `S3_BUCKET` | `kv-backups` | S3 bucket name |
| `S3_KEY` | `minioadmin` | S3 access key |
| `S3_SECRET` | `minioadmin` | S3 secret key |

## Architecture

```
┌─────────────────┐     REPLICAOF (TLS)     ┌─────────────────┐
│  Primary         │◄───────────────────────│  Replica         │
│  :6379 TLS       │  --tls_replication     │  :6380 TLS       │
│  :10001 admin    │                        │  :10002 admin    │
│  snapshot_cron   │                        │  read-only       │
└─────────────────┘                        └─────────────────┘
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Dragonfly primary + replica + MinIO (profile: s3) |
| `service.yaml` | Metadata for sdk-ops deploy |
| `init.sh` | SSL certs + start + cluster config |
| `validate.sh` | Health check (PING, ROLE, cluster, TLS) |
| `backup.sh` | BGSAVE → local dir + S3 (MinIO) |
| `restore.sh` | Restore from .dfs snapshot (--yes flag) |
| `backup-cron.sh` | Daily backup cron |
| `dragonfly-primary.conf` | Reference config for primary |
| `dragonfly-replica.conf` | Reference config for replica |
| `test/test.sh` | Integration test (PITR cycle) |
