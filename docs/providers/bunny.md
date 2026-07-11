# Bunny.net SDK

Bunny.net is a content delivery and edge computing platform offering CDN,
DNS, object storage, video streaming, WAF/shield, edge scripting, and
container hosting (Magic Containers).

Unlike `cubepath` and `vultr`, Bunny.net is **not** a compute/VPS provider and
does **not** implement the `Provider` interface. It is a standalone SDK with
its own CLI command tree: `sdk-ops bunny *`.

## Authentication

| Method | Value |
|--------|-------|
| Env var | `BUNNY_API_KEY` |
| CLI flag | `--api-key` |
| Header | `AccessKey` |
| API | `https://api.bunny.net` |

The API key is the same key from your Bunny.net dashboard Settings > API.
Storage file operations additionally require the **storage zone password**
as the `AccessKey` header (fetched automatically by the CLI).

## Package Structure

```
bunny/
├── client.go           # HTTP client with AccessKey auth (GET/POST/PUT/PATCH/DELETE)
├── types.go            # All shared type definitions (800+ lines)
├── dns.go              # DNS zone and record management + geo-routing
├── pullzone.go         # CDN Pull Zones + edge rules + hostnames + cache purge
├── mc_app.go           # Magic Containers app CRUD + deploy/undeploy/restart
├── mc_container.go     # Container templates, env vars, image config
├── mc_deploy.go        # Deploy flow: image registry auto-detect + CDN/Anycast
├── mc_endpoint.go      # Endpoints (CDN + Anycast)
├── mc_region.go        # Regions, autoscaling, nodes
├── mc_registry.go      # Container registries (Docker Hub, GHCR)
├── mc_volume.go        # Persistent volumes, log forwarding
├── storage.go          # Edge Storage zones + file upload/download/list/delete
├── stream.go           # Stream video libraries + video CRUD + fetch
├── shield.go           # Shield WAF zones + rate limits + bot detection
├── edgescript.go       # Edge Scripting (23 endpoints)
├── logging.go          # CDN logging query (v2 API)
├── origin_errors.go    # Origin error log retrieval
└── spec/               # 8 OpenAPI JSON specs downloaded
    ├── core.json       # Core Platform API
    ├── mc.json         # Magic Containers API
    ├── compute.json    # Edge Scripting API
    ├── shield.json     # Shield WAF API
    ├── stream.json     # Stream Video API
    ├── storage.json    # Edge Storage API
    ├── logging.json    # CDN Logging API
    └── origin-errors.json # Origin Errors API
```

## Services

### Magic Containers

Deploy containerized applications globally on Bunny's edge network.

| Command | Description |
|---------|-------------|
| `sdk-ops bunny app create <name> -i <image> -p <port> -r <region>` | Create + deploy an app |
| `sdk-ops bunny app list` | List all apps |
| `sdk-ops bunny app status <id>` | App details + resource usage |
| `sdk-ops bunny app overview <id>` | CPU, RAM, cost, latency |
| `sdk-ops bunny app logs <id>` | Pod logs and container status |
| `sdk-ops bunny app deploy <id>` | Deploy (start) an app |
| `sdk-ops bunny app restart <id>` | Restart all pods |
| `sdk-ops bunny app delete <id>` | Delete an app |
| `sdk-ops bunny app endpoint add-anycast <id> -p <port>` | Add Anycast IP endpoint |

Region aliases: `bogota`/`latam` → CO, `miami`/`usa` → MI, `frankfurt`/`europe` → DE

Registry auto-detection: `ghcr.io/*` → GHCR (ID 1156), others → Docker Hub (ID 1155).
Override with `--registry-id`.

### DNS

| Command | Description |
|---------|-------------|
| `sdk-ops bunny dns zone-list` | List all zones |
| `sdk-ops bunny dns zone-add <domain>` | Add a zone |
| `sdk-ops bunny dns zone-delete <id>` | Delete a zone |
| `sdk-ops bunny dns record-add <zone> --type A --name @ --value x.x.x.x` | Add record (types: A, CNAME, MX, TXT, NS, SRV, CAA, PTR, HTTPS, SVCB, TLSA) |
| `sdk-ops bunny dns record-list <zone>` | List records |
| `sdk-ops bunny dns record-delete <zone> <record>` | Delete record |

Geo-routing fields available in the API: `GeolocationInfo`, `LatencyZone`,
`SmartRoutingType`, `MonitorType`.

### CDN Pull Zones

| Command | Description |
|---------|-------------|
| `sdk-ops bunny pullzone list` | List all pull zones |
| `sdk-ops bunny pullzone create <name> -o <origin>` | Create a pull zone |
| `sdk-ops bunny pullzone delete <id>` | Delete a pull zone |
| `sdk-ops bunny pullzone purge <id>` | Purge cache |
| `sdk-ops bunny pullzone edge-rule list <id>` | List edge rules |
| `sdk-ops bunny pullzone edge-rule add <id> -d <desc> -s <header>` | Add edge rule (set-header) |
| `sdk-ops bunny pullzone edge-rule delete <id> <rule>` | Delete edge rule |

### Edge Storage

| Command | Description |
|---------|-------------|
| `sdk-ops bunny storage zone list` | List storage zones |
| `sdk-ops bunny storage zone create <name> -r <region>` | Create a zone (DE, NY, LA, SG, SYD) |
| `sdk-ops bunny storage zone delete <id>` | Delete a zone |
| `sdk-ops bunny storage file list <zone> <path>` | List files |
| `sdk-ops bunny storage file upload <zone> <path> <file>` | Upload a file (raw binary) |
| `sdk-ops bunny storage file download <zone> <path> <output>` | Download a file |
| `sdk-ops bunny storage file delete <zone> <path>` | Delete a file |

File operations use the storage zone password as `AccessKey` (fetched automatically).

### Stream Video

| Command | Description |
|---------|-------------|
| `sdk-ops bunny stream library list` | List video libraries |
| `sdk-ops bunny stream library create <name>` | Create a video library |
| `sdk-ops bunny stream video create <library> <title>` | Create a video (get GUID + upload URL) |
| `sdk-ops bunny stream video list <library>` | List videos |
| `sdk-ops bunny stream video get <library> <id>` | Get video details (status, views, size) |
| `sdk-ops bunny stream video delete <library> <id>` | Delete a video |
| `sdk-ops bunny stream video fetch <library> <url>` | Import video from a URL |

### Shield WAF

| Command | Description |
|---------|-------------|
| `sdk-ops bunny shield zone-list` | List shield zones |
| `sdk-ops bunny shield rate-limit list <zone>` | List rate limits |

### Edge Scripting

| Command | Description |
|---------|-------------|
| `sdk-ops bunny script create <name>` | Create an edge script |
| `sdk-ops bunny script list` | List scripts |
| `sdk-ops bunny script delete <id>` | Delete a script |
| `sdk-ops bunny script set-code <id> <file>` | Set script code from file |
| `sdk-ops bunny script publish <id>` | Deploy a release |

### Other

| Command | Description |
|---------|-------------|
| `sdk-ops bunny login` | Verify API key validity |

## Known Limitations

- **Edge Scripting** DELETE may require Bearer token auth (not just AccessKey)
- **Anycast IP** may not be reachable from all geographic locations immediately
- **File uploads** must be raw binary (not multipart)
- **Storage file operations** require the zone password (not the API key)
- **Shared runtime** MC pods do not support Fiber prefork (parent process exits)

## Code Location

All Bunny SDK code is in `bunny/`.
CLI commands are in `cmd/sdk-ops/bunny.go`.
8 OpenAPI specs are in `bunny/spec/`.
