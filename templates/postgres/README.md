# postgres — PostgreSQL HA cluster template (Patroni + etcd + PgDog + pgbackrest)

One scalable template, sized per node via `profiles.yaml` (lite/rs/...). Selected
via `provision.yaml`: `services: { postgres: { profile: <name> } }`.

## What it deploys (per node)

- **Patroni** (custom image: postgres 18-alpine + `patroni[etcd]` + pgbackrest):
  manages the primary/standby roles, failover AUTO (~35s), re-join (`pg_rewind`),
  `synchronous_mode` (RPO=0 — the replica acks before commit).
- **PgDog** (only on the pooler node): the app entry (6432), read/write split
  (`exclude_primary`), round-robin reads, `role="auto"` follows the leader.
- **etcd** (shared DCS — deploy `templates/etcd/` too): 3 members, quorum 2/3,
  v2 API (`--enable-v2=true` — Patroni uses python-etcd v2).
- **pgbackrest**: WAL archiving + full/diff/incr → S3 (aes-256-cbc). The backup
  runs on the **backup-server** node (backup-standby → the standby is the source).
  The restore runs ON the postgres host (pgbackrest 2.59 requirement).
- **DR**: `restore: true|false` — idempotent (if the cluster is operational, skip).

## Secrets (env-only, never the YAML)

`PG_SUPERUSER`, `PG_APP_PASSWORD`, `PG_REPLICATOR_PASSWORD`, `PG_REWIND_PASSWORD`,
`S3_*`, `PG_CIPHER_PASS`.

## Gotchas (validated on a 3-node cluster)

- pgbackrest **2.59 PGDG** (the apt 2.50 does not support postgres 18); the SAME
  version on every host (the protocol requires it).
- The Patroni container runs as `user: "70:70"` (initdb refuses root).
- `bin_dir=/usr/local/bin` (alpine layout); the socket lives at `/run/postgresql`
  (mount `./run:/run/postgresql` — `/var/run` is a symlink).
- The restored node must start FIRST (an empty node bootstrapping first creates a
  fresh cluster → system-id mismatch).
- The stanza must match the `archive_command` (`stanza main`); after a wipe the
  system-id changes → `stop` + `stanza-delete` + `start` + `stanza-create`.
- The provision re-applies the firewall peers per-port (last wins) → re-expose
  the etcd ports combined + re-apply the nft bridge fixes after each provision.
