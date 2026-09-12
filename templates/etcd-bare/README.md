# etcd-bare - etcd bare-metal template.

Native `etcd` binary + systemd on the host (no Docker): one DCS member per
fleet host, static bootstrap over peer_ip (`initial-cluster=etcdN=http://<ip>:2380`,
quorum 2/3), v2 API enabled (Patroni) and 2h auto-compaction. Same render
inputs and topology as `templates/etcd`; select it from provision.yaml:

```yaml
services:
  etcd-bare:
    profile: lite
```

## Render inputs (etcdRenderData - same builder as the docker etcd)

`EtcdName` (etcd0/1/2), `EtcdIP` (peer_ip), `InitialCluster`
(`etcd0=http://ip:2380,...`), profile limits (`MemLimit` -> systemd
`MemoryMax`, `Cpus` -> `CPUQuota`).

## Commands

`init` (pinned tarball v3.5.15 + SHA256SUMS check, arch amd64/arm64, `etcd`
system user, systemd unit, health wait - idempotent, restarts on config
change), `validate` (every member healthy), `snapshot`, `backup` / `restore`
(S3 over host s3cmd, ~/.s3cfg), `test` (quorum + consensus + failover via
`systemctl stop/start etcd` + S3 DR cycle). One member per VPS; the S3 DR
restore must run on ALL members in a maintenance window (see restore-s3.sh).
