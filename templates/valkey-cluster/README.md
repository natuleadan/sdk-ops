# valkey-cluster

Native [Valkey](https://valkey.io) Cluster on k3s — 6 pods, 3 primaries +
3 replicas, 16384 hash slots sharded, automatic failover built into the
cluster protocol.

## Architecture

- **StatefulSet, 6 pods** (`valkey-0`..`valkey-5`), `--cluster-enabled yes`.
- **Sharding**: 3 primaries own 16384 slots; replicas mirror each shard.
- **Failover**: cluster-native (no Sentinel/operator) — when a primary dies,
  its replica is promoted after `cluster-node-timeout` (5s).
- **Identity persists** in `/data/nodes.conf` (PVC): restarts rejoin.
- **Stable endpoints**: nodes announce their pod DNS name
  (`valkey-N.valkey-headless.<ns>.svc.cluster.local`, `--cluster-preferred-endpoint-type hostname`),
  so a whole-cluster restart with fresh pod IPs does not break the config;
  `init.sh` additionally re-merges nodes with `CLUSTER MEET` if it ever finds
  `cluster_state:fail` with the slots assigned.
- **Services**: `valkey-headless` (pod DNS for bootstrap) + `valkey`
  (ClusterIP seed for cluster-aware clients — use `-c`).
- **Storage**: local-path PVC, 1Gi per pod.
- **Persistence**: RDB snapshots only (`save 900/300/60`) — deliberately no
  AOF: with AOF enabled the server loads the AOF at boot and ignores the RDB,
  which breaks the snapshot restore path. The S3 backups are RDB snapshots;
  a crash loses at most the last save window.

## Configuration

| Env | Default | Description |
|---|---|---|
| `VK_K8S_NAMESPACE` | `valkey` | Kubernetes namespace |
| `VK_K8S_NAME` | `valkey` | Seed service name |
| `VK_K8S_TAG` | `8.1.3` | Valkey image tag |
| `VK_K8S_NODES` | `6` | Total nodes (primaries + replicas) |
| `VK_K8S_CLUSTER_REPLICAS` | `1` | Replicas per shard |
| `VK_K8S_MAX_MEMORY` | profile | `maxmemory` per node |
| `VK_PASSWORD` | `valkey` | Auth (`requirepass` + `masterauth`) |
| `S3_*` | — | Enable backups when bucket+endpoint+keys are set |

## Backups (per shard)

`backup-s3.sh` BGSAVEs every primary, copies each pod's `dump.rdb` through the
API server to the host and uploads `s3://$S3_BUCKET/$S3_PREFIX/<date>/<pod>.rdb`
plus a `manifest.txt` (raw cluster layout + pod/slot map).

`restore-s3.sh` performs a **whole-cluster restore** (a per-primary restart
would race the cluster-native failover — the returning primary would resync
from its promoted replica instead of booting the snapshot): it maps each RDB to
the pod that currently owns its slots (failover-safe), scales the StatefulSet
down, stages every snapshot into its PVC (wiping the stale AOFs via helper
pods) and scales back up — primaries boot from their snapshots and replicas
full-sync. Expect a brief full-cluster downtime and snapshot semantics: keys
written after the BGSAVE are gone.

## Commands

```bash
bash init.sh                    # apply + bootstrap the cluster (idempotent)
bash validate.sh                # state ok, 16384 slots, 3 primaries, PING
bash test/test.sh               # sharding + failover + data survivorship
bash backup-s3.sh               # per-shard BGSAVE -> S3
bash restore-s3.sh [--yes] [date]   # per-shard in-place reload from S3
```

## Client

```bash
valkey-cli -c -h valkey.valkey.svc.cluster.local -p 6379 -a "$VK_PASSWORD"
```
