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
- Use `--insecure` or set `SDK_OPS_SSH_STRICT_HOST_KEY=false` if host keys changed (e.g., after reinstall)
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

When deploying to a VPS with a private registry, the node needs to authenticate. sdk-ops auto-runs `docker login` on the node during `deploy push` using credentials from `REGISTRY_USER` / `REGISTRY_PASS` env vars. If login fails:

```bash
# Verify credentials
echo "$REGISTRY_PASS" | docker login -u "$REGISTRY_USER" --password-stdin $REGISTRY_SERVER
```

## Cloud-init Limitations

Some providers have specific cloud-init requirements:

- **User-data scripts must be valid YAML** in the `#cloud-config` format
- Some providers (Hetzner, DigitalOcean, Vultr, AWS) accept cloud-init natively
- If a provider doesn't support cloud-init, `--cloud-init` falls back to SSH-based provisioning

## nftables Forward Policy

The hardening step used to set `policy drop` on the forward chain, which broke inter-container networking (Docker containers could not communicate with each other). Starting from the current release, forward policy defaults to `accept` for Docker compatibility.

If you need to block forwarding for security reasons, manually add rules:

```bash
ssh <ip> "sudo nft add chain inet filter forward '{ type filter hook forward priority 0; policy drop; }'"
```

## Health Check

The health check in `deploy push` supports custom endpoints via `health_url` in `service.yaml`. If not set, it falls back to HTTP 200 on `/health` or `/healthz` on ports 18081, 8080, or 3000.

```yaml
# service.yaml — custom health check
health_url: http://localhost:9191/api/v1/health
health_timeout: 15
```

Or use the nested YAML format:

```yaml
health:
  path: /api/v1/health
  interval: 15
```

Without a custom `health_url`, the fallback probes ports 18081, 8080, and 3000. If your app listens on a different port or uses a different endpoint, the health check will fail and trigger a rollback.

## Secrets Rotation: PostgreSQL Readiness

When rotating a PostgreSQL password with `service rotate db`, the command includes a retry loop (up to 3 attempts at 2s intervals) to wait for PostgreSQL readiness. If the rotation fails, ensure the container is actually running and accepting connections before retrying.

```bash
# Check if the DB container is ready
docker exec <container> psql -h localhost -U <user> -c "SELECT 1;"
```

## Docker Port Conflict with k3s Traefik

When k3s is installed with Traefik (default), Traefik occupies ports 80 and 443 via iptables DNAT rules. Docker containers that also expose port 80 will conflict. Solutions:

- Use `--disable-traefik` during `infra init` to skip Traefik installation
- Deploy with `--runtime k3s` to use k3s Deployment + Service + Ingress instead of docker-compose
- Expose Docker containers on non-conflicting ports (e.g., 8080, 8081)

## SSH Access After Hardening

The hardening step now sets `PermitRootLogin no` (CIS Level 1). After running `infra init`, root SSH access is blocked. Use the `sdkops` user instead:

```bash
# Before hardening (as root)
sdk-ops infra init <ip> --user root

# After hardening (as sdkops)
ssh sdkops@<ip>
sdk-ops infra status <ip> --user sdkops
```

If you need root access temporarily, connect as sdkops and use `sudo -i`.

## Permissions After Drain

After `cluster drain <node>`, the node is marked `Ready,SchedulingDisabled`. Use `cluster uncordon <node>` to re-enable scheduling.

## Cert Install: Let's Encrypt Validation

Let's Encrypt HTTP-01 validation requires the domain to be publicly accessible on port 80. If the domain is behind Cloudflare proxy (orange cloud), the ACME challenge may fail. Solutions:

- Pause Cloudflare proxy (gray cloud) during certificate issuance
- Use Cloudflare Origin CA instead (requires CF API)
- Use DNS-01 challenge (requires Cloudflare API token)

The `--runtime k3s` flag configures the cert via Traefik, which can use its own ACME resolver if configured.

## Vultr LB: Algorithm Must Be leastconn

Vultr load balancers use `leastconn` as their balancing algorithm. The value
`round_robin` is not supported and will result in a `"Invalid algorithm."`
error. The provider automatically maps `round_robin` to `leastconn`.

## Vultr K8s: Minimum Plan Requirement

Vultr's managed Kubernetes (VKE) requires a minimum of `vc2-2c-2gb` for node
pools. The `vc2-1c-1gb` and `vhf-1c-1gb` plans are not supported for VKE and
will result in `"Invalid NodePool plan"`.

## Vultr Object Storage: tier_id Required

Creating an object storage bucket requires both `cluster_id` and `tier_id`.
The govultr SDK does not support `tier_id` yet, so creation via the provider
may fail. Use the Vultr dashboard or raw API for initial setup, then manage
with the CLI.

## Bunny MC: Fiber Prefork Not Supported

Fiber's prefork mode calls `os.Exit(0)` in the parent process after spawning
children. In Docker containers, PID 1 exiting causes the container to stop.
Use single-process mode with `GOMAXPROCS=8` instead.

## Bunny MC: Anycast IP Reachability

Anycast IPs (e.g. `109.x.x.x`) may not be reachable from all geographic
locations immediately after provisioning. BGP propagation can take minutes
to hours depending on the region.

## Bunny Edge Storage: Raw Binary Required

File uploads to Bunny Edge Storage must be sent as **raw binary** in the
request body. Multipart/form-data encoding will result in a 401 error.
The provider sends raw binary automatically.

## Bunny Edge Scripting: Auth May Require Bearer Token

Some Edge Scripting endpoints may require `Authorization: Bearer` instead of
`AccessKey` header. If you get a 401, try using the dashboard or raw API
with both header types.
