# libsql-dockerized — libSQL Full Stack

libSQL (sqld) primary-replica cluster with TLS gRPC replication, WAL-based incremental snapshots, and optional MinIO.

## Services

| Role | Port | TLS | Description |
|------|:----:|:---:|-------------|
| **sqld-primary** | 8080 (HTTP), 5001 (gRPC) | ✅ gRPC | Read/write |
| **sqld-replica** | 8081 (HTTP) | ✅ gRPC | Read-only |
| **MinIO** | 9000/9001 | ❌ | S3 storage (profile: s3) |

## Quick start

```bash
cd /path/to/libsql-dockerized
bash init.sh
```

## Connect

```bash
# HTTP API
curl -d '{"statements":["SELECT * FROM items"]}' http://<VPS_IP>:8080

# Vector search
curl -d '{"statements":["SELECT content FROM items ORDER BY embedding MATCH vector('"'"'[0.1,0.2,0.3]'"'"') LIMIT 5"]}' http://<VPS_IP>:8080
```

## Backups

```bash
bash backup.sh              # Full .db + WAL snapshots
bash backup-cron.sh          # Daily cron at 4 AM
```

Automatic WAL snapshots every 30s via `--snapshot-exec` and `--max-log-duration=30`.

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
┌──────────────────┐    gRPC (TLS)     ┌──────────────────┐
│  sqld-primary     │◄──────────────────│  sqld-replica     │
│  :8080 HTTP       │  WAL streaming    │  :8081 HTTP       │
│  :5001 gRPC       │                   │  read-only        │
│  snapshot_exec    │                   │                   │
└──────────────────┘                   └──────────────────┘
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | sqld primary + replica + MinIO (profile: s3) |
| `service.yaml` | Metadata for deploy |
| `init.sh` | TLS certs + start + schema |
| `validate.sh` | Health check (HTTP, SQL, vector, TLS) |
| `backup.sh` | Copy .db + WAL snapshots |
| `restore.sh` | Restore from .db backup (--yes flag) |
| `backup-cron.sh` | Daily backup cron |
| `gen-certs.sh` | Generate TLS certs for gRPC |
| `snapshot.sh` | Callback for --snapshot-exec |
| `libsql-primary.conf` | Reference config for primary |
| `libsql-replica.conf` | Reference config for replica |
| `test/test.sh` | Integration test (PITR cycle) |
