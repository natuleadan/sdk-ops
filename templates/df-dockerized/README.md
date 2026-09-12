# df-dockerized — Dragonfly KV Full Stack

Dragonfly cluster (1 primary + 2 replicas) with HAProxy TLS (single entrypoint) and S3 backup/restore (DR). Declarative YAML-driven via sdk-ops with profiles (lite/normal/medium/large).

## Services

| Role | Internal port | TLS | Description |
|------|:------------:|:---:|-------------|
| **HAProxy** | **6379** | [OK] | **Entrypoint** — round_robin primary + rep-1 + rep-2 |
| **Primary** | 6379 (int) | [X] | Read/write, all hash slots |
| **Replica-1** | 6380 (int) | [X] | Read-only, follows primary |
| **Replica-2** | 6381 (int) | [X] | Read-only, follows primary |

S3 is ALWAYS external (S3_ENDPOINT/S3_BUCKET env) — no embedded object storage.

## Quick start

```bash
cd /path/to/df-dockerized
bash init.sh
```

## Connect

```bash
# Single entrypoint — HAProxy distributes via round-robin
redis-cli --tls --cacert ssl/ca.crt -h <VPS_IP> -p 6379 -a dragonfly
```

## Backups

```bash
bash backup-s3.sh        # BGSAVE -> tar.gz -> S3 upload + retention (keep 7)
bash backup-cron.sh      # daily cron at 3 AM (backup-s3)
```

## Restore

```bash
bash restore-s3.sh                     # latest from S3 (interactive)
bash restore-s3.sh --yes               # latest from S3 (no prompt)
bash restore-s3.sh --yes df-2026-08-27-040000.tar.gz   # specific backup
```

## Validate

```bash
bash validate.sh
```

## Test

```bash
bash test/test.sh       # PITR cycle + S3 DR cycle + failover (reads survive)
```

## Env vars

| Variable | Default | Description |
|----------|---------|-------------|
| `DF_PASSWORD` | `dragonfly` | Auth password |
| `DF_PORT` | `6379` | HAProxy primary port (host) |
| `DF_REPLICA_PORT` | `6380` | HAProxy replica port (host) |
| `S3_ENDPOINT` | — | External S3 endpoint (required for backup-s3/restore-s3) |
| `S3_BUCKET` | `df-backups` | S3 bucket name |
| `S3_PREFIX` | `df` | Key prefix inside the bucket |
| `S3_ACCESS_KEY` | — | S3 access key (never in the repo) |
| `S3_SECRET_KEY` | — | S3 secret key (never in the repo) |
| `RETENTION` | `7` | Backup copies to keep in S3 |

## Profiles (fleet YAML)

`profile: lite | normal | medium | large` sizes dragonfly + haproxy (mem/cpus)
via the sdk-ops render engine (`dfRenderData`), same as pgsql/libsql/yuga.

## Architecture

```
                          +--------------+
                          |   Clients     |
                          +------+-------+
                                  | 6379 (single exposed port)
                          +------+-------+
                          |   HAProxy    |  round_robin
                          |   TLS term   |  server primary
                          |              |  server rep-1
                          +--+--------+--+  server rep-2
                             |        |
                     +-------+--+ +---+------+ +---+------+
                     | Primary   | | Rep-1    | | Rep-2    |
                     | :6379     | | :6380    | | :6381    |
                     | cluster   | | replica  | | replica  |
                     +-----------+ +----------+ +----------+
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Dragonfly primary + 2 replicas + HAProxy (Go-template rendered) |
| `profiles.yaml` | lite/normal/medium/large resource profiles |
| `init.sh` | SSL + start + REPLICAOF + DFLYCLUSTER CONFIG |
| `validate.sh` | Health check (PING, role, replication, TLS via HAProxy, 3-node) |
| `backup-s3.sh` | BGSAVE -> tar.gz -> S3 upload + retention |
| `restore-s3.sh` | Download latest (or named) backup from S3 + restore |
| `backup.sh` | BGSAVE -> local dir only (no S3) |
| `restore.sh` | Restore from local .dfs snapshot |
| `backup-cron.sh` | Daily backup-s3 cron |
| `haproxy.cfg` | Round-robin TLS termination |
| `gen-certs.sh` | EC key generation + PEM for HAProxy |
| `dragonfly-primary.conf` | Reference config for primary |
| `dragonfly-replica.conf` | Reference config for replica |
| `test/test.sh` | PITR + S3 DR + failover integration test |
