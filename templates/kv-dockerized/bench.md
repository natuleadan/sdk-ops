# kv-dockerized — redis-benchmark RPS

## What is RPS

**RPS** = Requests Per Second. redis-benchmark measures how many Redis commands
per second the server can process.

## Methodology

| Parameter | Value | Why |
|-----------|-------|-----|
| **Requests** | 300,000 per round | ~5-10s of measurement |
| **Clients** | 50 (`-c 50`) | Moderate concurrency |
| **Pipeline** | 1 | No pipelining, raw latency |
| **Payload** | 3 bytes | Default size |
| **Warmup** | 3 rounds (discarded) | Stabilize before measure |
| **Rounds** | 5 per command | Enough samples for avg |
| **Commands** | SET, GET, INCR | Write, read, atomic counter |
| **Tool** | `redis-benchmark` (inside Dragonfly container) |

## Environments

| Environment | CPU | RAM | Type |
|-------------|:---:|:---:|------|
| **Mac baremetal** | 10c ARM (Apple Silicon) | 32GB | Local Docker (Dragonfly primary direct) |
| **4c x86_64 VPS** | 4c high-frequency x86_64 | 8GB | Dedicated VPS (Dragonfly primary direct) |

## Results

### SET (write)

| Round | Mac | VPS |
|:----:|:---:|:---:|
| 1 | 121,892 | 46,569 |
| 2 | 124,875 | 46,041 |
| 3 | 124,938 | 45,133 |
| 4 | 124,347 | 44,079 |
| 5 | 125,376 | 43,840 |
| **Avg** | **124,285 rps** | **45,332 rps** |

### GET (read)

| Round | Mac | VPS |
|:----:|:---:|:---:|
| 1 | 125,376 | 46,076 |
| 2 | 120,861 | 46,555 |
| 3 | 124,751 | 47,081 |
| 4 | 124,906 | 45,823 |
| 5 | 126,582 | 42,766 |
| **Avg** | **124,495 rps** | **45,660 rps** |

### INCR (atomic counter)

| Round | Mac | VPS |
|:----:|:---:|:---:|
| 1 | 125,376 | 45,194 |
| 2 | 125,755 | 44,709 |
| 3 | 125,881 | 46,598 |
| 4 | 123,062 | 44,066 |
| 5 | 120,135 | 45,746 |
| **Avg** | **124,042 rps** | **45,263 rps** |

## Comparison

| Command | Mac | VPS | Ratio |
|---------|:---:|:---:|:-----:|
| SET | 124,285 | 45,332 | **2.7×** |
| GET | 124,495 | 45,660 | **2.7×** |
| INCR | 124,042 | 45,263 | **2.7×** |

## Notes

- Bare metal VPS test not possible: HAProxy requires TLS and `redis-benchmark`
  does not support `--tls`.
- All commands show similar throughput regardless of read/write — Dragonfly
  is not WAL-flush bound like PostgreSQL.
