# Provider Credentials

Credentials are resolved in this priority order:

1. `--api-key` CLI flag
2. Environment variable (provider-specific)
3. `~/.sdk-ops/credentials.yaml` file

## Environment Variables

| Provider | Env Var | Required? |
|----------|---------|-----------|
| CubePath | `CUBEPATH_API_KEY` | yes |
| Hetzner | `HETZNER_API_TOKEN` | yes |
| DigitalOcean | `DIGITALOCEAN_TOKEN` | yes |
| Vultr | `VULTR_API_KEY` | yes |
| Bunny.net | `BUNNY_API_KEY` | yes (also read from credentials file) |
| Civo | `CIVO_API_KEY` | yes |
| AWS | `AWS_REGION` + `AWS_PROFILE` (or default chain) | optional |

## Credential File

Save credentials to a file with:

```bash
# Set env vars first
export CUBEPATH_API_KEY="your-key"
export HETZNER_API_TOKEN="your-token"
export DIGITALOCEAN_TOKEN="your-token"
export VULTR_API_KEY="your-key"
export CIVO_API_KEY="your-key"
export AWS_REGION="us-east-1"
export AWS_PROFILE="default"

# Save them
sdk-ops config set-credentials
```

This creates `~/.sdk-ops/credentials.yaml`:

```yaml
cubepath_api_key: your-key
hetzner_api_token: your-token
digitalocean_token: your-token
vultr_api_key: your-key
civo_api_key: your-key
aws_region: us-east-1
aws_profile: default
```

After saving, you can use any provider command without env vars:

```bash
sdk-ops provider vps list --provider cubepath
sdk-ops provider vps list --provider hetzner
sdk-ops provider vps list --provider digitalocean
```

## Priority Example

```bash
# 1. --api-key flag wins
sdk-ops provider vps list --provider cubepath --api-key "override-key"

# 2. env var if no --api-key
export CUBEPATH_API_KEY="env-key"
sdk-ops provider vps list --provider cubepath

# 3. credentials file if no env var and no --api-key
# (after running config set-credentials)
unset CUBEPATH_API_KEY
sdk-ops provider vps list --provider cubepath
```
