# PostgreSQL HA with sdk-ops (Patroni + etcd + PgDog + pgbackrest)

The `postgres` service deploys a production-grade PostgreSQL cluster driven by
`provision.yaml` — the same pattern as `nats` (one scalable template + profiles).

## The declarative command

```bash
set -a; . env/.env; set +a            # the secrets (never in the YAML)
sdk-ops apply provision.yaml          # the whole fleet: init + services + DR
sdk-ops apply provision.yaml -v       # the per-step detail
sdk-ops apply provision.yaml --check  # dry-run: parse + render, no changes
```

One command, idempotent: re-applying an unchanged fleet is a no-op; the DR
restore runs only when the cluster is NOT operational.

## Topology

```yaml
mode: docker
hardening: false               # skip the OS hardening (docker + services only)
services:
  etcd: { profile: lite }        # DCS shared — 3 members, quorum 2/3
  postgres:
    profile: lite                # lite | normal | small
    backup_mode: leader          # leader (the primary backs up) | server
    restore: true                # idempotent DR from S3
    recreate: true               # wipe + clean redeploy (containers, data, DCS)
    backup: { full: weekly, diff: daily, incr: hourly }   # YAML-driven cadence
```

- **`hardening: false`** (default true): skips the OS hardening — no sdkops user,
  no firewall/allowlist — docker + the services only. For fast connectivity
  drills or pre-hardened nodes; connect as root. When `hardening: true` the
  provision runs the full hardening first (sdkops user + nft allowlist) and the
  peers/exposes are enforced.
- **OS matrix (validated)**: Ubuntu 22.04 / 24.04 / 26.04 + Debian 13 (trixie) —
  the provider mirror (`mirror.<provider>`, e.g. the provider's mirror) is replaced
  with the official archives (`archive.ubuntu.com` / `deb.debian.org`) on every
  wire — provider-agnostic (any provider).
- **`--check` validation**: in the docker mode the dry-run warns when a node has
  no IPv6 anywhere — the docker-mode install (docker, images, S3) needs a public
  route (IPv6 or a NAT egress); without it the deploy fails at the install step.

- **fleet-etcd first**: all the etcd members deploy BEFORE any postgres — the
  Patroni needs the DCS quorum (2/3) to elect; a single member blocks the
  linearizable reads.
- **Patroni** (custom image: postgres 18 + `patroni[etcd]` 4.1.4 + pgbackrest):
  failover AUTO (~35s), re-join with `pg_rewind`, `synchronous_mode` (RPO=0),
  **dual-stack** (`listen: "*:5432"` — IPv4 + IPv6).
- **PgDog per node** (v0.1.52): EVERY postgres node runs its own PgDog (6432) —
  the "local-read" pattern: each app reads via its internal PgDog
  (round-robin → replicas, `exclude_primary`) and writes go to the primary.
  One URL per app — the pooler does the split (the SQL parser + the role
  detection — it follows the leader on failover).
- **pgbackrest**: WAL archiving + full/diff/incr → S3 (aes-256-cbc) — the
  backup runs on the CURRENT primary (`backup_mode: leader` — the replicas skip).
- **DR**: `restore: true` + idempotent — restores the LATEST FULL from S3 only
  when the data is missing and the node is the primary candidate; the replicas
  clone from the restored leader.

## Secrets (env-only, never the YAML)

`PG_CERT_DIR`, `PG_SUPERUSER`, `PG_APP_PASSWORD`, `PG_REPLICATOR_PASSWORD`,
`PG_REWIND_PASSWORD`, `S3_*`, `PG_CIPHER_PASS`.

## Network model

- **Inside the VPS**: IPv4 (the docker bridge — `172.17.0.1` for the local <!-- go-check:ignore-ip -->
  hairpin; a container reaching the node's OWN public IP is firewall-blocked).
- **Between VPSes**: whatever the YAML's `peer_ip` declares — the private VLAN
  (`10.0.0.x`) for the intra variant, the public IPv4/IPv6 for the external ones.
- **IPv6-only hosts work**: the postgres listens dual (`*`), the mesh runs over
  the peers' IPv6, and the VPS does not need IPv4 at all.

### IPv6 resolutions (validated on IPv6-only VPSes)

- **Docker IPv6**: `daemon.json` (`ipv6` + `fixed-cidr-v6` + `ip6tables`) +
  `enable_ipv6` on the compose networks (the postgres + etcd).
- **nft rules**: the bridge egress for the peers' ports (5432/6432/2379/2380/
  8008) on both stacks (`172.16.0.0/12` + `fd00::/8`) AND **the 443 egress** <!-- go-check:ignore-ip -->
  — the containers must reach the S3 (pgbackrest) over HTTPS.
- **apt**: on a v4-less host, `Acquire::ForceIPv6` + the official Ubuntu
  archive (the provider mirror may be v4-only or v6-unreachable).
- **Image build**: `network: host` on the Patroni build — the build container's
  DNS fails on IPv6-only providers.
- **pg_hba**: the dynamic `postgresql.pg_hba` (the DCS — applied continuously)
  with the IPv4 + IPv6 rules (`0.0.0.0/0` + `::/0`) — the bootstrap's only runs
  once; without the dynamic one the v6 peers get rejected.
- **PgDog fallback**: `ghcr.io` (the PgDog image) has NO IPv6 (v4-only DNS). On
  an IPv6-only node the pull fails — the CLI then downloads the image from the
  OPERATOR's machine (pure-Go OCI client — no docker needed), ships it over the
  SSH pipe and `docker load`s it (the temp tarball is removed). The fallback is
  automatic, idempotent (the `image inspect` skips) and only for the PgDog.

## Usage guide

1. **Secrets**: source the env before any apply — the S3 keys, the NATS creds,
   the cipher pass — NEVER in the YAML:
   ```bash
   set -a; . env/.env; set +a
   ```
2. **Validate first** — the dry-run renders the plan and warns about the gaps:
   ```bash
   sdk-ops apply provision.yaml --check
   ```
   It warns when a node has no IPv6 (the docker-mode install needs a public
   route) and when env secrets are missing — fix before the real apply.
3. **Deploy**:
   ```bash
   sdk-ops apply provision.yaml --insecure
   ```
   Idempotent: the config diff recreates only what changed; the restore never
   runs over an operational cluster. `hardening: false` skips the OS hardening
   (docker + services only — connect as root); the default runs the full
   hardening first (sdkops user + nft allowlist).
4. **Validate the cluster** — the 27/27 suite on the primary (the workspace
   wrapper resolves the primary + the user sdkops/root automatically):
   ```bash
   the validation suite on the primary (the workspace wrapper)
   ```
   Covers connection, sync (RPO=0), split, pooling, TLS/mTLS, backups
   (full/diff/incr), PITR, WAL, clone, config, capacity, failover_ready.
5. **Drills**: kill the primary → the Patroni promotes alone (~35s); the DR is
   `recreate: true` + `restore: true` → the data comes back from S3.

## When to scale

When the docker model (≤3 nodes) is not enough — production HA, many nodes,
declarative scaling — move to **k3s + a postgres operator** (CloudNativePG).
The k3s cluster + the TLS + the registry images: see `docs/k3s.md`.
## Validated

- Failover AUTO + re-join + switchover + sync RPO=0.
- DR drill: `recreate: true` + `restore: true` → the data comes back from S3
  (the marker verified) + leader + sync standby.
- The read/write split: writes → the primary, reads → the replicas (via each
  node's PgDog), the data consistent (RPO=0).
- **The connectivity variants** (each with its own YAML):
  - Intra (VLAN `10.0.0.x`) — all the traffic private.
  - Internet IPv4 (the public v4 as peers).
  - Internet IPv6 (the public v6 as peers — the postgres dual, v4 only inside).
  - IPv6-only (no v4, no VLAN — the whole stack over the peers' v6).
  - Mixed (VLAN + IPv6 per host — the `peer_ip` decides per node).
- **OS matrix**: Ubuntu 22.04 / 24.04 / 26.04 + Debian 13 (trixie).
- **No-hardening matrix** (`hardening: false`, 2026-08-14): the suite **27/27
  ALL PASS** + the failover real (kill primary → promote) + the DR
  (marker → S3 → restore) on every connectivity variant:
  - VLAN mesh (`10.0.0.x`) — the v4/v6 only for egress.
  - IPv6 mesh (`::/64`, the internet) — no VLAN.
  - IPv4 mesh (the public v4, the internet) — no VLAN.
  - Mixed (VLAN + IPv6 per host — the `peer_ip` decides per node).

## Gotchas

- **`Acquire::ForceIPv6` breaks v4-only hosts**: the apt would force the
  unreachable IPv6 (the mirror/archive resolves to AAAA first) and every
  install fails ("Network is unreachable"). The docker install + the postgres
  wire remove `/etc/apt/apt.conf.d/99force-ipv6` ALWAYS and re-add it only on
  v4-less hosts — a leftover from a previous v6-only deploy breaks a v4-only
  node completely.
- **The init marker** (`/opt/sdk-ops/.version`): an existing marker makes the
  provision skip the full init ("already initialized, applying phases only") —
  removing it forces a full re-init (e.g. when switching the node mode).
- pgbackrest **2.59 PGDG** (the apt 2.50 does not support postgres 18).
- The Patroni container runs as `user: "70:70"`; the socket at `/run/postgresql`.
- The restored node must start FIRST (an empty node bootstrapping first creates
  a fresh cluster → system-id mismatch); the wire waits for the primary ready.
- The S3 repo's stanza: a fresh cluster needs `stanza-create` (the empty repo)
  or `stanza-upgrade` (the old system-id) — handled by the post-bootstrap.
- The backup scripts use `set -euo pipefail` — a failed backup must NOT report
  success.
- `ghcr.io` has no IPv6: the PgDog image fallback (see above) is the only
  non-native step — the data path (S3, streaming, the mesh) is always native.
- **SSH as `sdkops`** (not root) after hardening — the root's `authorized_keys`
  can carry a stale entry ("Server accepts key → Permission denied"); the
  sdkops account always works.
- **Backup cadence**: `backup: { full, diff, incr }` (all optional) → three
  systemd timers (`pgx-backup-full/diff/incr`). The full + diff share the
  00:15 slot by default — the diff is only scheduled when `diff:` is set.
- **Validation**: the granular suite (`pgx-test.sh`) is copied to the primary
  (`/opt/sdk-ops/pgx/`) with `/etc/sdk-ops/pgx.env` — `PG_NODES_IPS` must list
  the cluster's peer IPs (v4 or v6) for the role resolution; the app user needs
  `pg_monitor` (the sync visibility) + `CREATEDB` (the clone test); the TLS
  certs live at `/etc/sdk-ops/certs/`. Run from the Mac:
  `the validation suite on the primary (the workspace wrapper)`.
