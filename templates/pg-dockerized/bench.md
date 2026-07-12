# pg-dockerized — pgbench RPS

## What is TPS / RPS

**TPS** = Transactions Per Second. pgbench measures TPC-B-like transactions: each transaction is `SELECT abalance + UPDATE abalance + INSERT`. **RPS** = Requests Per Second (HTTP). Both measure throughput.

## Methodology

| Parameter | Value |
|-----------|-------|
| **Scale** | 10 (~1M rows in `pgbench_accounts`) |
| **Clients** | 50 (`-c 50`) |
| **Threads** | 4 (`-j 4`) |
| **Warmup** | 3 rounds × 10s each (discarded) |
| **Measure** | 5 rounds × 15s each |
| **Read-only** | `-S` flag (SELECT only) |
| **Write** | default (TPC-B: SELECT + UPDATE + INSERT) |
| **Tool** | `pgbench` from PostgreSQL 18 (inside Docker container) |

All benchmarks run **inside** the Docker container (same network, no network latency).

## Environments

| Environment | CPU | RAM | Type |
|-------------|:---:|:---:|------|
| **Mac baremetal** | 10c ARM (Apple Silicon) | 32GB | Local Docker (Postgres directo) |
| **4c x86_64 VPS** | 4c high-frequency x86_64 | 8GB | Dedicated VPS (Postgres directo) |
| **4c x86_64 VPS via PgDog** | Same VPS + PgDog proxy | 8GB | VPS Docker (PgDog → Postgres) |

## Results

### Read-only (SELECT)

| Round | Mac directo | VPS directo | VPS via PgDog |
|:----:|:-----------:|:-----------:|:-------------:|
| 1 | 114,233 | 23,257 | 16,079 |
| 2 | 114,253 | 22,584 | 15,797 |
| 3 | 104,875 | 23,795 | 15,484 |
| 4 | 98,214 | 23,283 | 15,425 |
| 5 | 104,171 | 22,924 | 16,654 |
| **Avg** | **107,149 tps** | **23,169 tps** | **15,888 tps** |

### Write (SELECT + UPDATE + INSERT)

| Round | Mac directo | VPS directo | VPS via PgDog |
|:----:|:-----------:|:-----------:|:-------------:|
| 1 | 7,231 | 2,859 | 2,197 |
| 2 | 9,173 | 2,868 | 2,175 |
| 3 | 10,243 | 2,758 | 2,176 |
| 4 | 10,598 | 2,916 | 2,241 |
| 5 | 10,172 | 2,811 | 2,137 |
| **Avg** | **9,483 tps** | **2,842 tps** | **2,185 tps** |

### Comparison

| Path | Read-only | Write | Ratio vs Mac (read) |
|------|:---------:|:-----:|:-------------------:|
| Mac baremetal directo | 107,149 | 9,483 | 1× |
| 4c x86_64 VPS directo | 23,169 | 2,842 | ~4.6× slower |
| 4c x86_64 VPS via PgDog | 15,888 | 2,185 | ~6.7× slower |

## Observations

- **PgDog adds ~31% read / ~23% write overhead** in raw pgbench because it parses the PostgreSQL wire protocol in Go. In production with hundreds of short-lived connections, PgDog's connection pooling compensates.
- **Write is WAL-flush bound** — all environments show ~3-4× lower throughput on writes than reads.
- **Scale 10 fits entirely in RAM** on all environments (1M rows ≈ 250MB of `pgbench_accounts`), so disk I/O does not factor into these results.
