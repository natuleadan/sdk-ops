# nats-dockerized bench

Measure the JetStream throughput of a deployed node/cluster.

## Quick RPS (core pub)

```
./rps.sh                    # core pub, 100 clients / 500k / 128B
./rps.sh --js               # JetStream sync pub, 20 / 100k / 128B
```

## Methodology (validated on a 3-node geo cluster)

- Run INSIDE the node (or the operator Mac) with the app credentials.
- Core pub: `nats bench` (100 clients, 500k messages, 128B) → messages/s.
- JetStream sync: `nats bench --js` (20 clients, 100k, 128B) → the ack'd rate.
- **Cluster mode** (R2/R3): JetStream sync writes wait for the raft quorum, so
  the rate is bounded by the quorum RTT (cross-region ~220ms). A single node
  (1R) has no quorum wait and publishes much faster.
- Compare like-for-like: same params, same node size, host memory free (no swap).
