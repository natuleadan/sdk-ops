# etcd-cluster

etcd **inside the k3s cluster** via the official bitnami helm chart - an
external DCS for services running in-cluster that need etcd (Patroni, etc.).
k3s keeps its own embedded etcd untouched. Same role as templates/etcd
(docker, one member per VPS), but consumed over internal service DNS, no host
ports. Quorum 2/3 with 3 members; auto-compaction periodic 2h; RBAC off.

## Deploy

```yaml
mode: k3s
hosts:
  - name: node-01
    services:
      etcd-cluster:
        profile: normal
```

Scripts run ON the node: `bash init.sh` (helm auto-installed), then
`validate.sh` / `test/test.sh` / `backup-s3.sh` / `restore-s3.sh [snapshot]`.
Client endpoint: `<release>.<ns>.svc.cluster.local:2379` (no host ports).

## Env

| Var | Default | Meaning |
|---|---|---|
| `ETCD_K8S_NAMESPACE` | `etcd` | K8s namespace |
| `ETCD_K8S_RELEASE` | `etcd` | Helm release |
| `ETCD_K8S_TAG` | `3.5.15` | Image tag (pinned) |
| `ETCD_K8S_REPLICAS` | `3` | Members (quorum 2/3) |
| `S3_*` | env only | Bucket/prefix/keys for DR (never in YAML) |

## Gaps

- restore-s3 VERIFIES the snapshot in a helper pod; rebuilding the live
  members from it is manual (scale down, restore data dirs, scale up).
- RBAC disabled - restrict consumption with a NetworkPolicy.
