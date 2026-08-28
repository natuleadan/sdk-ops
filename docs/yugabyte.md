# YugabyteDB with sdk-ops (yuga-docker)

A declarative distributed-SQL cluster: 3 `yugabyted` nodes (3 YB-Masters + 3
YB-TServers, RF=3), with automatic failover, a Postgres-compatible endpoint
(YSQL, port 5433) and a Cassandra-compatible one (YCQL, port 9042), and
logical backups to S3-compatible storage. Declared like any other fleet service:

```yaml
hosts:
  - name: node-01
    host: <ip>
    ssh_key: /path/key
    user: root
    services:
      yugabyte:
        profile: lite
```

```bash
sdk-ops apply fleet.yaml
```

## Topology

Each container is a `yugabyted` node running one master + one tserver. Only
`yugabyte-0` publishes host ports; nodes 1/2 join over a static bridge subnet
(`yb-net`, TEST-NET by default — override with `YB0_IP`/`YB1_IP`/`YB2_IP`).
A strict `depends_on: service_healthy` between nodes is **avoided on purpose**:
the seed never reports "Running" until the 2/3 quorum the followers provide, so
a strict ordering deadlocks. All nodes start concurrently and `yugabyted` joins
asynchronously.

## Commands (in the template dir)

| Command | What |
|---|---|
| `bash init.sh` | Start the 3 nodes, wait for quorum, create the app DB/user |
| `bash validate.sh` | Health + YSQL/YCQL write/read + replication + quorum |
| `bash backup.sh` | Logical dump (`ysql_dump`) -> local + S3 (retention) |
| `bash restore.sh [-y]` | Restore the latest dump from S3 / local |
| `bash test/test.sh` | Integration: write/read, replication, failover, DR cycle |

## Env / secrets

- `YB_DB`, `YB_USER`, `YB_PASSWORD`, `YB_VERSION`, `YB_SHARDS`, `YB_CLOUD_LOCATION`,
  `YB0_IP`/`YB1_IP`/`YB2_IP` (bridge IPs).
- Backup S3 (never in the repo): `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`,
  `S3_SECRET_KEY`. Backups land at `s3://<bucket>/yugabyte/<db>-<stamp>.sql.gz`.

## Production DR with YBA (physical restore + PITR)

The `ysql_dump` path above is the lightweight route (works on any `yugabyted`
node). For a **physical, production-grade restore** the platform tool is
**YugabyteDB Anywhere (YBA)** — the `yba_restore` Terraform resource restores a
backup to a universe from a storage config, with point-in-time recovery:

```hcl
resource "yba_restore" "example" {
  universe_uuid       = "<target-universe-uuid>"
  storage_config_uuid = "<storage-config-uuid>"
  backup_storage_info {
    storage_location = "<storage-location-from-backup>"
    keyspace         = "my_database"      # YSQL db / YCQL keyspace
    backup_type      = "PGSQL_TABLE_TYPE" # YSQL | YCQL
  }
  # optional: restore_to_point_in_time_millis = <unix-ms>  # PITR
}
```

Key points (from the YBA restore docs):

- Each `backup_storage_info` restores one keyspace/database; multi-keyspace
  backups list one entry per database (`keyspace` may differ from the original
  to restore elsewhere).
- `backup_type` selects YSQL (`PGSQL_TABLE_TYPE`), YCQL (`YQL_TABLE_TYPE`) or
  Redis (`REDIS_TABLE_TYPE`).
- `restore_to_point_in_time_millis` enables **PITR** (restore to a specific
  Unix timestamp).
- `alter_load_balancer` (default true) keeps the LB rebalanced during restore;
  `parallelism` (default 8) sets the concurrent SSH commands.
- The restore is **one-time**: the resource does not track remote state —
  remove it after `terraform apply`.
- `storage_config_uuid` points at the storage config (e.g. the S3/B2 bucket
  where the backup lives) — never MinIO internally for the DR copy.

## Failover

RF=3 tolerates one node loss — killing any single `yugabyte-*` keeps reads and
writes flowing while the remaining masters hold quorum (2/3). `test/test.sh`
verifies it. A lost node re-joins and re-syncs on restart.

## Validated

- 3-node cluster: masters quorum (3), YSQL + YCQL read/write, follower reads.
- Failover: kill one node -> cluster stays healthy -> node re-joins.
- Backup to real S3 (B2): `ysql_dump` shipped and confirmed in the bucket.
- Resource profiles in `profiles.yaml` (`lite`/`normal`/`medium`/`large`).

## Gotchas

- `pg_dump` is **not** shipped — use `ysql_dump` (under the postgres bin dir in
  the image). `ysqlsh` is `/usr/local/bin/ysqlsh` (a `bin/ysqlsh` symlink).
- The masters bind the container bridge IP, not loopback — health checks and
  clients must target the bridge IP (or the published host port).
- `--join` takes an IP (no `:port`); the master RPC port defaults to 7100.
- Same `--cloud_location` for all nodes (placement by zone); per-zone placement
  over a 3-node local cluster fails the quorum check.
- The `minio/mc` image entrypoint is `mc` — run it with `--entrypoint sh` for
  multi-command scripts.
