# Known Issues & Workarounds

## Provider API Rate Limits

**CubePath**: 5 requests per 5 minutes.

When running batch operations (create + poll + delete), add sleeps between commands to avoid HTTP 429 errors.

```bash
# Bad (hits rate limit):
sdk-ops provider vps create ...
sdk-ops provider vps list ...
sdk-ops provider vps delete ...

# Good:
sdk-ops provider vps create ...
sleep 70
sdk-ops provider vps list ...
sdk-ops provider vps delete ...
```

## Firewall / SSH Port

Some VPS providers have upstream firewalls that block non-standard ports. By default, sdk-ops **keeps SSH on port 22** and does not migrate. If you use `--ssh-port N`, the new port is added alongside port 22. If you get locked out:

- Verify the provider doesn't block the new port
- Use `--insecure` if host keys changed (e.g., after reinstall)
- Reconnect on port 22 (which is always kept open)

## kubectl top (Metrics API)

`kubectl top` requires the metrics-server to be fully operational. After k3s installation:

```bash
# Check if metrics-server is ready
sdk-ops cluster pods --all-namespaces | grep metrics-server

# Wait for it (may take 1-2 minutes)
sdk-ops cluster top  # will work once metrics-server is Ready
```

## Load Balancer Deletion

Some providers (CubePath) do not allow deleting a Load Balancer while it is in "deploying" state. Wait for the LB to become active before deleting:

```bash
sdk-ops provider lb list          # check status
# If "deploying", wait and retry
sleep 30
sdk-ops provider lb delete <id>
```

## Deploy: Docker Registry Auth

When deploying to a VPS with a private registry, the node needs to authenticate. sdk-ops auto-runs `docker login` on the node during `deploy push` using credentials from `NLA_REGISTRY_USER` / `NLA_REGISTRY_PASS` env vars. If login fails:

```bash
# Verify credentials
echo "$NLA_REGISTRY_PASS" | docker login -u "$NLA_REGISTRY_USER" --password-stdin $NLA_REGISTRY_SERVER

# Default registry server: ewr.vultrcr.com/nlaregistry
```

## Cloud-init Limitations

Some providers have specific cloud-init requirements:

- **User-data scripts must be valid YAML** in the `#cloud-config` format
- Some providers (Hetzner, DigitalOcean, Vultr, AWS) accept cloud-init natively
- If a provider doesn't support cloud-init, `--cloud-init` falls back to SSH-based provisioning
