# libsql-dockerized — libSQL 3-Node Cluster

libSQL (sqld) cluster: 1 primary + 2 replicas, etcd-based controller (automatic failover), write-aware router (TLS), and S3 backup.

## Services

| Role | Port | TLS | Description |
|------|:----:|:---:|-------------|
| **Router** | **8443** | Yes | **Entrypoint** — writes -> primary, reads -> replicas |
| **Controller** | **9090** | No | Leader election (etcd), automatic failover |
| **etcd** | 2379 (int) | No | DCS — stores leader state with epoch fencing |
| **sqld-primary** | 8080 (int) | No | Read/write, gRPC :5001 |
| **sqld-replica-1** | 8081 (int) | No | Read-only, gRPC to primary |
| **sqld-replica-2** | 8082 (int) | No | Read-only, gRPC to primary |

## Quick start

```bash
cd /path/to/libsql-dockerized
bash init.sh
```

## Connect

```bash
# Write via router (routes to primary)
curl -k -d '{"statements":["INSERT INTO items VALUES(3, '\''test'\'')"]}' https://<IP>:8443

# Read via router (routes to replicas, round-robin)
curl -k https://<IP>:8443

# Query leader info
curl http://<IP>:9090/leader
```

## Failover

The controller detects primary failure and promotes a replica:

```bash
docker kill libsql-dockerized-sqld-primary-1
# Controller promotes a replica within 15-20s
curl -k -d '{"statements":["SELECT 1"]}' https://localhost:8443
```

## Backup to S3

```bash
# Set S3 credentials
export S3_ACCESS_KEY=...
export S3_SECRET_KEY=...

# Backup
bash backup-s3.sh

# Restore (latest from S3)
bash restore-s3.sh

# Restore specific backup
bash restore-s3.sh libsql-2026-08-21-040000.tar.gz

# Install daily cron
bash backup-cron.sh
```

## Validate

```bash
bash validate.sh
```

## Test

```bash
bash test/test.sh       # PITR cycle + failover test
```

## Architecture

```
                           +--------------+
                           |   Clients     |
                           +------+-------+
                                  | 8443 (TLS, write-aware)
                           +------▼-------+
                           |   Router     |
                           |  POST -> P    |
                           |  GET  -> R    |
                           +--+-------+---+
                              |       |
                     +--------▼--+ +--▼------+ +---▼------+
                     | Primary    | | Rep-1    | | Rep-2    |
                     | :8080      | | :8081    | | :8082    |
                     | gRPC:5001  | | gRPC v   | | gRPC v   |
                     +------------+ +----------+ +----------+
                              |
                     +--------▼--------+
                     |   Controller    | <--- etcd (leader, epoch)
                     |   (failover)    |
                     +-----------------+
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | 3 sqld + router + etcd + controller |
| `Dockerfile` | Custom sqld image (curl for health checks) |
| `controller/main.go` | etcd-based leader election + failover |
| `router/main.go` | TLS reverse proxy with write/read splitting |
| `init.sh` | TLS + etcd + sqld + controller + schema |
| `validate.sh` | Health checks (etcd, controller, 3 sqld, router TLS) |
| `backup-s3.sh` | Backup data.sqld (tar.gz) + upload to S3 |
| `restore-s3.sh` | Download from S3 + restore |
| `backup-cron.sh` | Install daily backup cron |
| `test/test.sh` | PITR + failover integration test |
| `profiles.yaml` | Resource tiers (lite/normal/medium/large) |

## Profiles

| Profile | sqld mem | sqld cpus | etcd mem | Total RAM |
|---------|----------|-----------|----------|-----------|
| lite | 256m | 0.5 | 128m | ~1g |
| normal | 512m | 1 | 256m | ~2g |
| medium | 1g | 2 | 512m | ~4g |
| large | 2g | 4 | 1g | ~8g |

## Env vars

| Variable | Default | Description |
|----------|---------|-------------|
| `S3_ACCESS_KEY` | — | S3 access key |
| `S3_SECRET_KEY` | — | S3 secret key |
| `S3_BUCKET` | `libsql-backups` | S3 bucket name |
| `S3_PREFIX` | `libsql` | S3 key prefix |
| `S3_ENDPOINT` | `s3.us-east-005.backblazeb2.com` | S3 endpoint |
| `CONTROLLER_PORT` | `9090` | Controller HTTP port |
| `LIBSQL_HTTP_TLS` | `8443` | Router TLS port |
| `HEALTH_THRESHOLD` | `3` | Consecutive failures before failover |
| `RETENTION` | `7` | Days of backups to keep |

## PITR / restore semantics

The backup captures the sqld **data directory** (`data.sqld/` as a tar.gz),
which contains the WAL frames, `wallog` and snapshots — the full state sqld
needs to restore. Copying only `dbs/default/data` (the plain SQLite file)
restores an **empty checkpoint**, because sqld keeps committed writes in its
frame log, not in the `.db` file.

The `test/test.sh` PITR cycle confirms that a restore returns the **pre-backup
rows** (3 in the test) — i.e. the data as of the snapshot, **not** the writes
that landed after the backup. This is point-in-time recovery up to the
snapshot. For continuous WAL-level continuity, use sqld's **Bottomless**
replication (WAL pages streamed to S3-compatible storage continuously) in
addition to the snapshot backup. The snapshot path is the simple,
self-contained DR; Bottomless is the production continuity layer.

