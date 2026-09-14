# crowdsec-bare - CrowdSec engine + nftables bouncer (host-native)

Installs the **CrowdSec engine** and the **nftables firewall bouncer** natively
under systemd (no Docker), from the **pinned apt repo** (signed keyring + fixed
release; no `curl|sh`). Enforcement is L3/L4: offending IPs are dropped at the
kernel by the bouncer.

## Two modes

| Mode | When | What runs |
|---|---|---|
| **standalone** (default) | single host | local LAPI + agent + bouncer (decisions local) |
| **client** (`CS_LAPI_URL`) | host without a central engine | agent reports to a **remote LAPI**; the local bouncer consumes that LAPI's decisions |

Client mode is the VLAN / multi-host layout: **one central engine processes**
(parses logs, decides) and **every host consumes** the decisions, blocking
locally. The client needs `CS_LAPI_URL`, `CS_LAPI_USER`, `CS_LAPI_PASSWORD`
(machine credentials) and `CS_BOUNCER_KEY` (create it on the central:
`cscli bouncers add <node>`). Secrets come from the environment and are written
to the node as `/opt/sdk-ops/services/crowdsec-bare/.env` (mode 0600).

## Install (fleet YAML, granular)

Standalone:

```yaml
mode: bare
hosts:
  - name: edge
    host: 192.0.2.10
    services:
      crowdsec-bare:
        profile: lite
```

Client of a central LAPI (env carries the secrets):

```bash
export CS_LAPI_URL=http://198.51.100.10:30080   # the central LAPI
export CS_LAPI_USER=machine
export CS_LAPI_PASSWORD=...
export CS_BOUNCER_KEY=...                        # cscli bouncers add edge (on the central)
```

```yaml
mode: bare
hosts:
  - name: edge
    host: 192.0.2.10
    services:
      crowdsec-bare:
        profile: normal
```

```bash
sdk-ops infra provision crowdsec.yaml --insecure
```

Raw commands (ON the node, under `/opt/sdk-ops/services/crowdsec-bare/`):
`bash init.sh` · `bash validate.sh` · `bash test/test.sh`.

## What the init does

1. Adds the pinned apt repo (`packagecloud.io/crowdsec/crowdsec`, signed key).
2. Installs `crowdsec=<CS_VERSION>` and
   `crowdsec-firewall-bouncer-nftables=<CS_BOUNCER_VERSION>` (version prefix
   resolved against the repo; an absent version fails loudly).
3. Installs the profile collections and an SSH acquisition (`/var/log/auth.log`).
4. Wires the mode (local or remote LAPI) and the bouncer (`api_url` + key).
5. Applies a systemd drop-in (`MemoryMax`/`CPUQuota` from the profile) and
   starts both units.

## Profiles

| Profile | Engine | CPUQuota | Collections |
|---|---|---|---|
| `lite` | 256M | 50% | linux, sshd |
| `normal` | 512M | 100% | linux, sshd, base-http |
| `large` | 1G | 200% | + http-cve |

## Uninstall

Declared-service cleanup disables both units and removes the service dir.
Packages stay (shared host tooling); purge manually with
`apt-get remove --purge crowdsec crowdsec-firewall-bouncer-nftables`.

## Gotchas

- **Pinned versions**: `CS_VERSION` / `CS_BOUNCER_VERSION` are prefixes matched
  against the repo (packagecloud may suffix a distro tag). A missing prefix
  fails the init instead of installing a surprise version.
- **Bare central serving remotes** is opt-in via `CS_LAPI_LISTEN` (default
  `127.0.0.1:8080`); the port must still be opened to the peers (VLAN).
- **nftables** must be the host firewall backend (Ubuntu/Debian default).
- Client mode still runs a local LAPI (harmless); only the agent's reporting
  target and the bouncer's LAPI change.
- The bouncer streams decisions every ~10s: the enforcement assertion in
  `test/test.sh` polls up to 30s.
