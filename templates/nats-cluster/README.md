# nats-cluster

NATS JetStream R3 in k3s via the official `nats/nats` helm chart, plus the
NACK JetStream controller (`nats/nack`) so streams and consumers are
declarative Kubernetes CRDs reconciled by the controller.

- StatefulSet R3 (the chart sets `podManagementPolicy: Parallel`), JetStream
  file store on a local-path PVC, soft topology spread across k3s nodes.
- Headless service for routes; clients use `nats://<release>.<ns>.svc:4222`.
- NACK reconciles `jetstream.nats.io` Stream/Consumer/KV/ObjectStore CRDs.
  A NACK-managed stream is exclusively NACK-owned (manual edits are enforced
  back). With `NATS_K8S_NACK_CONTROL_LOOP=true` the controller also reverts
  drift continuously; pick ONE owner per stream (CRD or CLI, never both).
- No server auth/TLS in-cluster (k8s network trust model + NetworkPolicy).
  TLS/mTLS + accounts remain the nats-dockerized/nats-bare (edge/exposed)
  story. Gap documented.

## Deploy (fleet YAML)

```yaml
mode: k3s
hosts: [...] # the k3s nodes
services:
  nats-cluster:
    profile: lite   # lite | normal | medium | large
```

## Commands

| command | what |
|---|---|
| `init` | helm (auto-pinned) + chart install + NACK + CRDs + /healthz wait |
| `validate` | statefulset ready + healthz + pub/sub + JetStream + NACK CRD |
| `test` | 8-step integration (cluster, JetStream, KV, NACK CRD, failover, S3 DR) |
| `backup` / `restore` | node-side DR: stream backup -> nkey seal -> S3 (and back) |

Env: `NATS_K8S_*` (namespace/release/tag/replicas/storage class/NACK), `S3_*`
(endpoint with or without scheme), `NATS_S3_PREFIX` (default `nats`),
`NATS_SEAL_SENDER_NK` (PATH to the sender's XKey **seed** file — curve keys
`X...`/`SX...`, not user nkeys: `nkey.CreateCurveKeys()` / `nsc generate nkey`),
`NATS_SEAL_RECIPIENT_PUB` (PATH to a file with the recipient's public XKey),
`NATS_RECIPIENT_NK` / `NATS_UNSEAL_RECIPIENT_NK` (PATH to the recipient's seed
file — the unseal half, operator secret, never on the node).
