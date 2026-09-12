# nats-bare - NATS bare-metal template.

Native `nats-server` binary + systemd on the host (no Docker): one cluster
node (R3 mesh over peer_ip) with TLS/mTLS, at-rest JetStream encryption and
per-service authz. Same render inputs, profiles and scripts as
`nats-dockerized`; select it from provision.yaml:

```yaml
services:
  nats-bare:
    profile: lite
```

## Render inputs (set at provision time, secrets from .env - never the YAML)

`ServerName`/`Advertise`/`Routes` (hosts + peers), the profile limits
(`MaxConnections`, `MaxFileStore`, `MaxMemoryStore`, `MemLimit` -> systemd
`MemoryMax`, `Cpus` -> `CPUQuota`), `ClusterName`/`JSKey` (NATS_JS_KEY) and
bcrypt hashes of `NATS_APP/SVC/SYS_PASSWORD` - same `natsRenderData()` builder
as nats-dockerized.

## Commands

`init` (pinned tarball v2.10.24 + nats CLI 0.4.0, `nats` system user, certs,
systemd unit, health wait - idempotent, restarts on config change),
`validate`, `backup` / `backup-cron` (JetStream seal -> S3, NKey curve),
`restore <stream> [timestamp]` (operator recipient NKey), `test` (7-step
integration test incl. failover via `systemctl stop/start nats-server` and an
S3 DR cycle over s3cmd).

## Gaps

- Secrets live in the operator env; backup/restore/validate read `$DIR/.env`
  (the dockerized `nats` service gets it wired by Go - for nats-bare the
  operator drops `/opt/sdk-ops/services/nats-bare/.env`, 0600, or exports the
  `NATS_*`/`S3_*` vars when running the scripts).
- The validate/backup systemd timers are NOT installed by init.sh (the
  dockerized wiring does it in Go) - add a daily timer for backup-cron.sh.
- Single-VPS multi-replica mode (replicas>1 with one NATS host) is docker-only
  (`maybeWriteSingleVPS` targets compose); nats-bare needs one VPS per node.
