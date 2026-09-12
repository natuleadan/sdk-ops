# Valkey Cluster on k3s (valkey-cluster)

The `valkey-cluster` template runs a **native Valkey Cluster** inside k3s: 6
cluster-enabled nodes = 3 primaries + 3 replicas, 16384 hash slots sharded and
cluster-native failover (no Sentinel, no operator — Valkey is Redis-compatible
but the template does not depend on any Redis/operator tooling).

## The declarative command

```bash
set -a; . env/.env; set +a            # VK_PASSWORD + S3_* (never in the YAML)
sdk-ops provision svc.yaml --check    # dry-run: parse + render
sdk-ops provision svc.yaml            # deploy / update (idempotent init)
```

```yaml
mode: k3s
hardening: false
no_traefik: true
hosts:
  - { name: mia-01, host: <ip>, peer_ip: <ip> }
services:
  valkey-cluster:
    profile: lite          # Mem/CPU/MaxMemory from profiles.yaml
```

## Topology

- **StatefulSet, 6 pods** (`valkey-0`..`valkey-5`, `podManagementPolicy:
  Parallel`), `--cluster-enabled yes`, cluster bus on 16379.
- **Bootstrap**: `init.sh` meets the nodes and runs
  `valkey-cli --cluster create ... --cluster-replicas 1` once; re-provisions
  skip it when `cluster_state:ok` with all slots assigned. If a bootstrap ever
  fails midway (partial config, `node is not empty`), the init resets the nodes
  and creates fresh (only when no slots are assigned).
- **Stable endpoints**: nodes announce their pod DNS name
  (`valkey-N.valkey-headless.<ns>.svc.cluster.local`,
  `--cluster-preferred-endpoint-type hostname`) so a full restart with new pod
  IPs does not break the config; the init re-merges with `CLUSTER MEET` if it
  ever finds `cluster_state:fail` with the slots assigned.
- **Persistence**: RDB snapshots only (`save 900/300/60`) — deliberately no
  AOF: with AOF enabled the server loads the AOF at boot and ignores the RDB,
  which breaks the snapshot restore path.
- **Services**: `valkey-headless` (pod DNS) + `valkey` (ClusterIP seed for
  cluster-aware clients).

## Profiles

`lite: { CPU: 250m, Mem: 128Mi, MaxMemory: 96mb }` (the minimum functional),
then `normal` / `medium` / `large`. `MaxMemory` defaults from the profile and
can be overridden with `VK_K8S_MAX_MEMORY`.

## Client

```bash
valkey-cli -c -h valkey.valkey.svc.cluster.local -p 6379 -a "$VK_PASSWORD"
```

Cluster-aware clients (`-c` / MOVED-ASK) are required: hash slots belong to
primaries, the seed service answers with redirects.

## DR (S3)

- `backup-s3.sh`: BGSAVE on every primary, `kubectl cp` each shard's RDB out
  and upload `s3://$S3_BUCKET/$S3_PREFIX/<date>/<pod>.rdb` + a `manifest.txt`
  with the pod/slot map.
- `restore-s3.sh`: whole-cluster restore — maps each RDB to the pod that
  currently owns its slots (failover-safe), scales the StatefulSet to 0,
  stages the snapshots into the PVCs (helper pods, md5-verified), drops the
  stale AOFs, scales back up: primaries boot from their snapshots and replicas
  full-sync. Snapshot semantics: keys written after the BGSAVE are gone.

## Gotchas (validated)

- `kubectl wait --for=condition=Ready pod -l <label>` does **not** wait for
  the N replicas (only the pods alive when invoked) — the scripts count ready
  pods instead.
- Master counts must parse `myself,master` too (awk on the role field, not a
  plain `grep " master"`).
- A per-primary in-place restore races the cluster-native failover (the
  returning primary resyncs from its promoted replica and overwrites the
  staged RDB) — that is why the restore is whole-cluster.
