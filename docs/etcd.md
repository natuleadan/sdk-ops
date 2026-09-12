# etcd

etcd ships in two disposable modes plus two DCS variants:

| mode | template | use case |
|---|---|---|
| docker | `etcd` | shared DCS for Patroni (pgsql multi-VPS wiring) — 3-member quorum 2/3, v2 API on |
| bare | `etcd-bare` | native etcd + systemd, manual/standalone DCS across VPSes (same static bootstrap via peer_ip) |
| k3s | `etcd-cluster` | external DCS inside the k3s cluster (bitnami helm, 3 members over internal service DNS) |

## Mode notes

- **k3s never consumes the docker/bare etcd**: k3s ships its own embedded
  datastore (etcd with `--cluster-init`, or kine+sqlite by default), and our
  k3s PG path is CNPG, which uses the k8s API as its DCS.
- **No current consumer for the bare/cluster DCS variants**: etcd only exists
  as the Patroni DCS, and the only Patroni is `pgsql-cluster` (docker, uses
  `templates/etcd/`). The `etcd-bare` and `etcd-cluster` templates are
  complete and tested but their fleet YAMLs are archived (`_archive/`) — they
  stay available for a future in-cluster/bare Patroni. The bitnami helm chart
  itself is deprecated upstream (a new template would be created if needed).
- Static bootstrap: `--initial-cluster=etcdN=http://<peer_ip>:2380,...` is
  rendered from the fleet topology (etcdRenderData) — every member must boot
  with the same initial-cluster list.
- Snapshot DR: `etcdctl snapshot save` (API v3) -> S3; restore verifies
  integrity via `snapshot status` and re-seeds the data dir (bare does the
  swap with systemd; k3s uses a helper pod).
- Patroni needs `enable-v2: true` (python-etcd v2) and auto-compaction
  retention 2 — keep both flags/configs in every mode.
