# libSQL with sdk-ops (libsql-dockerized)

A declarative 3-node libSQL (sqld) cluster: 1 primary + 2 replicas with
**automatic failover** (etcd leader election + controller), a **write-aware TLS
router**, and **S3 backup/restore** for point-in-time recovery (DR). Declared
like any other fleet service:

```yaml
hosts:
  - name: node-01
    host: <ip>
    ssh_key: /path/key
    user: root
    services:
      libsql:
        profile: lite        # lite | normal | medium | large
```

```bash
sdk-ops apply fleet.yaml            # deploy the whole stack
sdk-ops apply fleet.yaml --check    # dry-run: parse + render, no changes
```

## Topology

| Role | Port | TLS | Description |
|------|:----:|:---:|-------------|
| **Router** | **8443** | Yes | **Entrypoint** — writes → primary, reads → replicas (round-robin) |
| **Controller** | **9090** | No | Leader election (etcd), automatic failover + rejoin |
| **etcd** | 2379 (int) | No | DCS — stores leader state with epoch fencing |
| **sqld-primary** | 8080 (int) | No | Read/write, gRPC :5001 |
| **sqld-replica-1** | 8081 (int) | No | Read-only, gRPC to primary |
| **sqld-replica-2** | 8082 (int) | No | Read-only, gRPC to primary |

The whole stack runs as one compose project on the node. The controller
recreates containers via `docker run` after a failover, so container names gain
instance suffixes (`sqld-replica-2` vs `sqld-replica-2-1`) — scripts resolve
names dynamically, never hardcoded.

## Failover (self-healing)

The controller health-checks every sqld every 5s. After `HEALTH_THRESHOLD`
(3) consecutive failures of the primary it:

1. Promotes the healthiest replica (its volume becomes the new primary).
2. Recreates the replicas pointing at the new primary's gRPC.
3. Bumps the etcd leader epoch (fencing) and notifies the router via watch.
4. Rejoins any lost replica back to the full count (`DESIRED_REPLICAS`).

The router watches etcd AND rediscovers containers via docker.sock every 10s,
so it re-targets the new primary without a restart:

```bash
docker kill libsql-dockerized-sqld-primary-1
# ~15-20s later: controller promotes a replica, epoch bumps, writes keep working
curl -k -d '{"statements":["SELECT 1"]}' https://<IP>:8443
```

## Backup / restore (S3, point-in-time)

Backups are **full `data.sqld/` snapshots** (tar.gz) — sqld keeps committed
writes in its WAL frame log, not in a plain `.db`, so copying only the `.db`
would restore an empty checkpoint. The backup runs a snapshot of the whole data
directory and uploads to S3-compatible storage with retention:

```bash
# Set S3 credentials (never in the repo)
export S3_ENDPOINT=... S3_BUCKET=... S3_ACCESS_KEY=... S3_SECRET_KEY=...

bash backup-s3.sh                 # snapshot → upload → retention (keep 7)
bash restore-s3.sh                # restore latest from S3 (interactive)
bash restore-s3.sh --yes libsql-2026-08-27-102242.tar.gz   # specific backup
```

Restore semantics: the snapshot returns the data **as of the backup moment** —
writes after the backup are not replayed (clean point-in-time). The
`test/test.sh` PITR cycle verifies exactly this (3 rows pre-backup → restore →
exactly 3 rows).

`init.sh` also auto-restores from S3 on a fresh node when the data volume is
empty (idempotent DR: re-applying an existing node skips it).

## Commands (in the template dir)

| Command | What |
|---|---|
| `bash init.sh` | TLS → etcd → (DR restore if empty) → sqld ×3 → controller/router → schema |
| `bash validate.sh` | etcd, controller, leader, 3× sqld health+SQL, router TLS, replication, 3-node |
| `bash backup.sh` | Local `data.sqld` tar.gz snapshot |
| `bash backup-s3.sh` | Snapshot → S3 upload → retention |
| `bash restore-s3.sh [-y] [backup]` | Download from S3 + restore (full data dir) |
| `bash test/test.sh` | PITR cycle + failover + S3 + router routing (14 steps) |
| `bash backup-cron.sh` | Install a daily backup cron |

## Profiles

Fleet YAML `profile:` sizes the whole stack deterministically (Go-rendered
into the compose):

| Profile | sqld mem/cpu | etcd mem/cpu | Total RAM |
|---------|--------------|--------------|-----------|
| lite | 256m / 0.5 | 128m / 0.25 | ~1g |
| normal | 512m / 1 | 256m / 0.5 | ~2g |
| medium | 1g / 2 | 512m / 0.5 | ~4g |
| large | 2g / 4 | 1g / 1 | ~8g |

## Env / secrets

- Sizing (from profile, overridable via env): `SQLD_MEM`, `SQLD_CPUS`,
  `ETCD_MEM`, `ETCD_CPUS`, `CONTROLLER_MEM`, `ROUTER_MEM`, ...
- Ports (defaults): `LIBSQL_HTTP=8080`, `LIBSQL_REPLICA_HTTP=8081`,
  `LIBSQL_REPLICA2_HTTP=8082`, `LIBSQL_HTTP_TLS=8443`, `CONTROLLER_PORT=9090`.
- Controller: `HEALTH_INTERVAL=5`, `HEALTH_THRESHOLD=3`, `DESIRED_REPLICAS=2`.
- Backup S3 (never in the repo): `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`,
  `S3_SECRET_KEY`, `S3_PREFIX`, `RETENTION=7`.

## Cloud-native notes

- The stack is the **cloud-native variant** of libsql (declarative YAML +
  self-healing + DR), equivalent to `pgsql-docker`/`yuga-docker`. There is no
  mature Kubernetes operator for sqld (unlike CNPG/yugabyte-operator), so the
  compose stack with etcd + controller is the supported HA deployment.
- Images are pinned (`libsql-server:v0.24.33`, etcd `v3.5.15`); never `latest`.
- The controller/router Go sources ship as `main.go.txt` (embed-safe) and are
  renamed by their Dockerfiles at build time, keeping `go mod tidy` clean.
- The router mounts docker.sock to rediscover containers after failover.
