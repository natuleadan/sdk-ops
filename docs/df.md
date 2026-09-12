# df — Dragonfly (Redis-compatible)

Dragonfly ships in three modes, all with profiles + validate +
test + S3 DR cycle:

| mode | template | transport | DR |
|---|---|---|---|
| docker | `df-dockerized` | compose: primary + 2 replicas + HAProxy TLS (single VPS) | BGSAVE -> s3cmd, restore-s3 |
| bare | `df-bare` | native dragonfly binary + systemd + native HAProxy TLS | same |
| k3s | `df-cluster` | official `dragonflydb/dragonfly-operator` (CRD `dragonflydb.io/v1alpha1`) — primary + replicas with **automatic failover**; service follows the master | native `spec.snapshot.dir: s3://` (dragonfly >= 1.12) + explicit BGSAVE/cp/s3cmd cycle |

## DragonflyDB vs dragonflyoss — same name, different product

`dragonflyoss/dragonfly` (d7y.io) is the P2P **image distribution** system
(CDN for container pulls). The KV store is **DragonflyDB**
(dragonflydb.io, redis-compatible). The k3s template uses the
dragonflydb operator only.

## k3s lessons

- The operator CR supports `replicas`, `image` (pin it), `args`
  (`--requirepass`, `--cluster_mode=emulated`), `resources`, and
  `spec.snapshot.dir: "s3://<bucket>/<prefix>/"` + `cron` for native S3
  snapshots.
- **`networkPolicyEnabled: false`** matters if the operator is bumped past
  **v1.6.0**: those versions create a NetworkPolicy by default that restricts
  port 6379 to operator-managed pods only — blocking validate/test pods and
  any external client. Set it to `false` in the CR to disable it (YAML-driven,
  no manual `kubectl patch`). The pinned operator (v1.1.4) does not create the
  policy, so the current CR does not need the field.
- The operator manages the primary/replica set itself — no manual
  `REPLICAOF`; the CR service always points at the current master.
- **Label selectors**: the operator creates pods with
  `app.kubernetes.io/name=dragonfly` (NOT `app.kubernetes.io/instance`). The
  `role=master` / `role=replica` labels identify the current master for
  backup/restore/failover scripts. Use
  `app.kubernetes.io/name=dragonfly,role=master` to target the master pod.
- DR from the node: `kubectl exec SAVE` + `kubectl cp` the dump out, s3cmd
  put; restore copies the dump back into the master and restarts the pod
  (Dragonfly reloads dump.rdb on boot).
- Failover test: delete the master pod — the operator promotes a replica and
  the service flips to it; keys written before the kill survive.
- S3 prefix convention: `df` (not `kv`). All df-* templates default
  `S3_PREFIX=df` and `S3_BUCKET=df-backups`.
