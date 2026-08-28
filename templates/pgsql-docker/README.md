# pgsql-docker — PostgreSQL Full Stack

PostgreSQL 18 + 2 streaming replicas + PgDog connection pooler (read/write split, round_robin LB) + SSL/TLS + pgbackrest backups + optional MinIO S3.

## Services

| Role | Port | TLS | Description |
|------|:----:|:---:|-------------|
| **PgDog** | **6432** | [OK] | **Entrypoint** — LB round_robin, exclude_primary, role=auto |
| **PostgreSQL (primary)** | 5432 (internal) | [OK] | WAL archiving, pgbackrest |
| **PostgreSQL (replica-1)** | 5433 (internal) | [OK] | Streaming standby, hot standby |
| **PostgreSQL (replica-2)** | 5434 (internal) | [OK] | Streaming standby, hot standby |
| **MinIO** | 9000/9001 | [X] | S3 storage (`--profile s3`) |

## Quick start

```bash
cd /path/to/pgsql-docker
bash init.sh
```

## Connect

```bash
psql "postgresql://dev:devpass@<VPS_IP>:6432/postgres?sslmode=require"
```

## Backups

```bash
bash backup.sh                    # pgbackrest full backup
bash backup-cron.sh               # daily cron at 3 AM
```

## Restore

```bash
bash restore.sh                          # latest backup (immediate recovery)
bash restore.sh --mode full              # last full backup
bash restore.sh --mode pitr --target '2026-07-12 15:30:00'   # Point-in-time
```

## Validation

```bash
bash validate.sh
```

## Test

```bash
bash test/test.sh       # PITR cycle: backup -> disaster -> restore -> verify
```

## Env vars

| Variable | Default | Description |
|----------|---------|-------------|
| `PG_USER` | `dev` | PostgreSQL user |
| `PG_PASSWORD` | `devpass` | PostgreSQL password |
| `PG_DATABASE` | `postgres` | Default database |
| `PG_PORT` | `5432` | Primary port |
| `PG_REPLICA_PORT` | `5433` | Replica-1 port (internal) |
| `PG_REPLICA2_PORT` | `5434` | Replica-2 port (internal) |
| `PGDOG_PORT` | `6432` | PgDog port |
| `PGDOG_POOL_SIZE` | `20` | Connection pool size |
| `REPLICATOR_PASSWORD` | `replicatorpass` | Replication user password |
| `S3_ENDPOINT` | — | MinIO/S3 endpoint |
| `S3_BUCKET` | `pg-backups` | S3 bucket name |
| `S3_KEY` | `minioadmin` | S3 access key |
| `S3_SECRET` | `minioadmin` | S3 secret key |

## Architecture

```
                    +--------------+
                    |   Clients     |
                    +------+-------+
                           | 6432 (único puerto expuesto)
                    +------▼-------+
                    |   PgDog      |  LB round_robin
                    |  role=auto   |  exclude_primary
                    |  repl check  |  lsn_check_interval=1s
                    +--+--------+--+
                       |        |          +-----------+
                +------▼--+ +--▼----+ +----▼----------+
                | Primary  | | Rep-1 | | Rep-2        |
                | :5432    | |:5433  | | :5434        |
                | pgbackrest| |stream | | stream      |
                +----+-----+ +-------+ +--------------+
                     |
              +------▼------+
              | pgbackrest   |
              | repo (local  |
              | or S3/MinIO) |
              +-------------+
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Primary + replica + PgDog + MinIO (profile: s3) |
| `service.yaml` | Metadata for deploy |
| `init.sh` | SSL -> primary -> replicator -> stanza -> replica -> PgDog |
| `validate.sh` | Health checks (primary, replica, PgDog, streaming, pgbackrest) |
| `backup.sh` | pgbackrest full backup (docker exec) |
| `restore.sh` | Restore latest/full/PITR via temp container |
| `backup-cron.sh` | Daily backup cron |
| `pg-entrypoint.sh` | Installs pgbackrest for WAL archiving |
| `pg-replica-entrypoint.sh` | pg_basebackup clone + standby.signal |
| `pgbackrest.conf` | pgbackrest config (local or S3) |
| `pgdog.toml` | LB + replication + connection recovery |
| `users.toml` | PgDog user credentials |
| `test/test.sh` | Full PITR cycle test |
