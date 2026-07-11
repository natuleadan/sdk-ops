# Providers

This directory contains documentation for each service provider integrated into
`sdk-ops`. Providers implement the `providers.Provider` interface or provide
standalone CLI command trees.

| Provider | Type | CLI prefix | Doc |
|----------|------|------------|-----|
| **CubePath** | Interface (VPS, K8s, LB, DNS) | `sdk-ops provider * --provider cubepath` | [cubepath.md](cubepath.md) |
| **Vultr** | Interface + extras (firewall, S3, CDN, block storage) | `sdk-ops provider * --provider vultr` | [vultr.md](vultr.md) |
| **Bunny.net** | Standalone SDK (MC, DNS, CDN, Storage, Stream, Shield, Scripting) | `sdk-ops bunny *` | [bunny.md](bunny.md) |
| **Hetzner** | Interface (VPS, K8s) | `sdk-ops provider * --provider hetzner` | — |
| **DigitalOcean** | Interface (VPS, K8s) | `sdk-ops provider * --provider digitalocean` | — |
| **AWS** | Interface (EC2, EKS, ELB, Route53) | `sdk-ops provider * --provider aws` | — |

## Auth

Each provider requires its own API key or token. See [provider-credentials.md](../provider-credentials.md)
for setup instructions.
