# yuga-bare

Install **YugabyteDB from the official tarball** and run `yugabyted` directly on
the VPS — **no Docker**. One node per fleet host; the first host is the seed and
later hosts join it to form an RF=3 cluster. This is the bare-metal mode (the
`yuga-docker` template is the Docker mode).

## Deploy

```yaml
mode: bare
hosts:
  - name: node-01
    host: <seed-ip>
    ssh_key: /path/key
    user: root
    services:
      yugabyte: { profile: lite }        # the bare template
```

`mode: bare` runs the init directly on the host (no Docker); `mode: docker` uses
the compose stack. `init.sh` downloads, verifies and installs the tarball:

```bash
wget https://software.yugabyte.com/releases/2026.1.1.1/yugabyte-2026.1.1.1-b2-linux-x86_64.tar.gz
shasum --check  # via the official .sha
tar xzf ... && ./bin/post_install.sh
./bin/yugabyted start
```

## Env

| Var | Default | Meaning |
|---|---|---|
| `YB_VERSION` | `2026.1.1.1-b2` | Tarball version |
| `YB_JOIN` | `` (empty) | Seed IP; empty = this host is the seed |
| `YB_ADVERTISE` | `127.0.0.1` | Advertise address (use the peer IP for the mesh) |
| `YB_INSTALL_DIR` | `/opt/yugabyte` | Where the tarball is unpacked |
| `YB_DATA_DIR` | `/var/lib/yugabyte` | Data + config dir |
| `YB_SHARDS` | `4` | Tablets per tserver |

## DR (external S3, never MinIO)

`backup.sh` dumps via `ysql_dump` and ships to **Backblaze B2** (`S3_*` env, the
`minio/mc` client only as a transport — the storage is the external bucket).
`restore.sh` pulls the latest dump and reloads it.

## Commands

| Command | What |
|---|---|
| `bash init.sh` | Download + install + start the yugabyted node (join if `YB_JOIN`) |
| `bash validate.sh` | yugabyted status + YSQL write/read |
| `bash backup.sh` | Dump -> local + external S3 |
| `bash restore.sh [-y]` | Restore from S3 / local |

## Gotchas

- The `yugabyte` OS user runs yugabyted (never root).
- The seed must be up before the followers join (`--join=<seed-ip>`).
- `--join` takes an IP (no `:port`); the master RPC port defaults to 7100.
- Run the same `YB_RELEASE`/`YB_VERSION` on every node.
- **ARM (aarch64)**: the init auto-detects `uname -m` and pulls the
  `-el8-aarch64` tarball on arm64 hosts — no extra config needed.

## Alternative: k3s operator (inside a cluster)

For a Kubernetes deployment the **yugabyte-k8s-operator** (helm) manages a
cluster inside k3s declaratively (the same role CloudNativePG plays for
postgres). See `mode: k3s` in the fleet YAML:

```bash
helm repo add yugabytedb https://charts.yugabyte.com
helm install yb-demo yugabytedb/yugabyte \
  --set replicas.master=1,replicas.tserver=1,Image.tag=2026.1.1.1-b2
```

The `yuga-docker` template (compose) is the `mode: docker` option; this
`yuga-bare` template is the `mode: bare` option.

For a **physical, production-grade restore** (vs the lightweight `ysql_dump`
path) use **YugabyteDB Anywhere** — the `yba_restore` Terraform resource
restores a backup to a universe from a storage config with PITR
(`restore_to_point_in_time_millis`). See `docs/yugabyte.md`.
