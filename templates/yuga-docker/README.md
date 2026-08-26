# yuga-docker

YugabyteDB distributed SQL cluster for sdk-ops — **3 `yugabyted` nodes**
(3 YB-Masters + 3 YB-TServers, RF=3), with automatic failover, a
Postgres-compatible endpoint (YSQL) and a Cassandra-compatible one (YCQL),
and logical backups to S3-compatible storage.

One declarative command provisions it on a VPS:

```bash
sdk-ops apply fleet.yaml
```

## Topology

```
                 Clients
                    │
           ┌────────┴─────────┐
           │ yugabyte-0 (EP)  │   YSQL :5433 · YCQL :9042 · UI :7000/9000
           └────────┬─────────┘
        ┌───────────┼───────────┐
        ▼           ▼           ▼
   yugabyte-0    yugabyte-1   yugabyte-2   (each = master + tserver, RF=3)
        └───────────┼───────────┘
                    │
                 docker network (advertise_address = service name)
```

Only **yugabyte-0** publishes ports (the entrypoint). Nodes 1/2 join over the
docker network via `--join=yugabyte-0:7100`. To span real VPSes, the fleet
provisioner rewrites `advertise_address`/`join` to the peer IPs — the same
pattern the `pg`/`nats` templates use for inter-node mesh.

## Commands

| Command | What |
|---|---|
| `bash init.sh` | Start the 3 nodes, wait for quorum, create the app DB/user |
| `bash validate.sh` | Health + YSQL/YCQL write/read + replication + quorum |
| `bash backup.sh` | Logical dump (`pg_dump`) → local + S3 (retention) |
| `bash restore.sh [-y]` | Restore the latest dump from S3 / local |
| `bash test/test.sh` | Integration: write/read, replication, failover, DR cycle |

## Env / secrets

- `YB_DB`, `YB_USER`, `YB_PASSWORD`, `YB_VERSION`, `YB_SHARDS`, `YB_CLOUD_LOCATION` (`cloud.region.zone` per node).
- Backup S3 (never in the repo): `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`.

## Failover

RF=3 tolerates one node loss — kill any single `yugabyte-*` and the cluster
keeps serving reads and writes while the remaining masters hold quorum (2/3).
The integration test verifies this.

## Resource profiles

`profiles.yaml` sizes CPU/memory/shards (`lite`, `normal`, `medium`, `large`);
the provisioner renders them into the compose resource limits.

## Gotchas

- **Node loss tolerance**: a small VPS (2 cores) can host all 3 nodes but only
  with the `lite` profile; keep builds off the cluster nodes.
- **YCQL auth**: the app role gets YSQL grants in `init.sh`; YCQL permissions
  may need a separate `CREATE ROLE ... SUPERUSER` if the app uses YCQL.
- **First boot** is slow (masters elect, tables split) — allow ~60s before the
  health checks pass.
