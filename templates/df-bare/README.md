# df-bare - Dragonfly KV bare-metal

Dragonfly cluster (primary + 2 replicas) as native systemd units (no Docker) behind a
native HAProxy TLS entrypoint on :6443 (writes -> primary, reads round-robin -> replicas).
S3 backup/restore (DR) via host s3cmd (~/.s3cfg). Declarative YAML-driven via sdk-ops
with profiles (lite/normal/medium/large), sized by `kvRenderData`.

| Component | Port | Notes |
|-----------|------|-------|
| dragonfly-primary | 127.0.0.1:6379 (admin 10001) | read/write, cluster mode, snapshots |
| dragonfly-replica-1 | 127.0.0.1:6380 (admin 10002) | read-only, follows primary |
| dragonfly-replica-2 | 127.0.0.1:6381 (admin 10003) | read-only, follows primary |
| HAProxy (TLS) | *:6443 | single public surface: writes -> primary, reads -> replicas |

All data ports bind loopback only. Open the entrypoint with
`infra firewall allowlist expose 6443`.

## Usage

```bash
bash init.sh        # pinned binary + units + haproxy + certs (idempotent)
bash validate.sh    # health check (PING, links, TLS, real exit codes)
bash backup.sh      # BGSAVE -> ./backups (local)
bash backup-s3.sh   # BGSAVE -> tar.gz -> S3 + retention (keep 7)
bash restore-s3.sh --yes [backup-name]
bash test/test.sh   # PITR + S3 DR + failover integration test
```

Env: `DF_PASSWORD`, `DF_VERSION` (default `v1.30.1`), `S3_*` - never in the repo.
