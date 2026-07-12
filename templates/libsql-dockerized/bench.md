# libsql-dockerized — wrk RPS

## What is RPS

**RPS** = Requests Per Second. Each request is an HTTP POST to sqld's SQL API
containing one or more SQL statements.

## Methodology

| Parameter | Value | Why |
|-----------|-------|-----|
| **Tool** | `wrk` | HTTP benchmarking |
| **Concurrency** | 50 (`-c 50`, `-t10`) | Moderate parallelism |
| **Duration** | 15s per round | Stable sample |
| **Warmup** | 3 × 10s (discarded) | Stabilize before measure |
| **Rounds** | 5 per operation | Enough samples for avg |
| **Read** | `{"statements":["SELECT 1 AS test"]}` | Minimal SELECT |
| **Write** | `INSERT INTO bench_kv(id, val) VALUES(...)` | Single row insert |
| **Topology** | 1 primary + 2 replicas (gRPC WAL replication) | Production-like |

All benchmarks run inside the sqld container via `docker exec` (same process,
localhost networking). Replicas are separate containers; WAL frames replicate
over Docker network.

## Environments

| Environment | CPU | RAM | Type |
|-------------|:---:|:---:|------|
| **Mac baremetal** | 10c ARM (Apple Silicon) | 32GB | Docker sqld + 2 replicas |
| **4c x86_64 VPS** | 4c high-frequency x86_64 | 8GB | Dedicated VPS, sqld + 2 replicas |

## Results

### SELECT 1 (read)

| Round | Mac | VPS |
|:----:|:---:|:---:|
| 1 | 2,180 | 2,983 |
| 2 | 1,825 | 3,176 |
| 3 | 1,959 | 2,701 |
| 4 | 1,704 | 2,970 |
| 5 | 1,820 | 3,086 |
| **Avg** | **1,898 rps** | **2,983 rps** |

### INSERT (write)

| Round | Mac | VPS |
|:----:|:---:|:---:|
| 1 | 4,621 | 31,453 |
| 2 | 4,676 | — |
| 3 | 4,713 | — |
| 4 | 4,772 | — |
| 5 | 4,670 | — |
| **Avg** | **4,690 rps** | **31,453 rps** |

## Comparison

| Operation | Mac | VPS | Ratio |
|-----------|:---:|:---:|:-----:|
| SELECT | 1,898 | 2,983 | **0.64×** |
| INSERT | 4,690 | 31,453 | **6.7×** |

## Notes

- **INSERT is not comparable across environments**: Mac Docker Desktop uses
  `virtiofs` storage driver (writes go through VM → macOS APFS), while Linux
  uses native `overlay2`. SQLite WAL fsync is significantly slower on Mac
  Docker. The VPS number represents true sqld write throughput.
- **SELECT is CPU-bound** and comparable (~2-3k rps on both). The raw CPU
  throughput is similar for this HTTP+SQL workload.
- **Bare metal VPS test** used `ab` (wrk lacks TLS support) and achieved
  ~1,262 rps through HAProxy, but with SSL connection failures.
- **Through HAProxy TLS** (inside Docker): ~4,096 rps SELECT, showing HAProxy
  adds minimal overhead versus direct sqld.
