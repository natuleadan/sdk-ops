package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
	golang_ssh "golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"

	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/ssh"
	"github.com/natuleadan/sdk-ops/templates"
)

// applyServicesOn deploys the services declared for one host (YAML-driven).
// Each service renders its template with the node's profile + cluster topology
// and the secrets from the environment (never the YAML). Idempotent by design:
// identical rendered config leaves the running service untouched.
func applyServicesOn(pf ProvisionFile, h ProvisionHost) error {
	r := resolveHostConfig(&pf, h)
	if len(r.services) == 0 {
		return nil
	}
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := infraFlags{user: "sdkops", key: h.SSHKey, port: port, mode: pf.Mode}
	conn, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("services: connect %s: %w", h.Name, err)
	}
	defer closeConn(conn)

	for name, cfg := range r.services {
		if err := deployServiceOn(conn, pf, h, name, cfg); err != nil {
			return fmt.Errorf("services %s on %s: %w", name, h.Name, err)
		}
	}
	return nil
}

// deployServiceOn deploys one service (e.g. nats) onto a node.
func deployServiceOn(conn *golang_ssh.Client, pf ProvisionFile, h ProvisionHost, name string, cfg ServiceConfig) error {
	tmpl, ok := templates.Templates[name+"-dockerized"]
	if !ok {
		return fmt.Errorf("no template for service %q (available infra templates: pg-dockerized, kv-dockerized, libsql-dockerized, nats-dockerized)", name)
	}

	data, err := buildRenderData(pf, h, tmpl.DirName, cfg.Profile, cfg)
	if err != nil {
		return err
	}

	renderDir, err := os.MkdirTemp("", "sdk-ops-svc-"+name+"-")
	if err != nil {
		return err
	}
	defer removeAll(renderDir)

	if err := templates.RenderDir(tmpl.DirName, renderDir, data); err != nil {
		return err
	}

	svcDir := "/opt/sdk-ops/services/" + name
	if _, _, err := ssh.Run(conn, "sudo mkdir -p "+svcDir); err != nil {
		return err
	}
	// Decide before uploading: compare the rendered config vs the deployed one
	// (the upload overwrites the remote, so the diff must be taken first).
	recreate, err := serviceConfigChanged(conn, renderDir, svcDir)
	if err != nil {
		return err
	}
	if err := uploadDir(conn, renderDir, svcDir); err != nil {
		return err
	}

	// Service-specific wiring (certs, secrets, CLI, timers).
	switch name {
	case "nats":
		if err := wireNATSOn(conn, svcDir, h.Name); err != nil {
			return err
		}
	default:
		return fmt.Errorf("no wiring for service %q", name)
	}

	// Idempotent deploy: recreate the container only when a mounted config
	// changed (compose up alone does not restart on config-file changes).
	up := fmt.Sprintf("cd %s && sudo docker compose up -d", svcDir)
	if recreate {
		up += " --force-recreate"
	}
	if _, _, err := ssh.Run(conn, up); err != nil {
		return err
	}
	if err := exposeServicePorts(conn, renderDir, pf, h); err != nil {
		return err
	}
	return nil
}

// serviceConfigChanged reports whether the rendered config differs from the
// one deployed on the node (or the service container is not running), which
// means the container must be recreated to pick up the new config.
func serviceConfigChanged(conn *golang_ssh.Client, renderDir, svcDir string) (bool, error) {
	//nolint:gosec // renderDir is a MkdirTemp dir we control + fixed filename
	rendered, err := os.ReadFile(filepath.Join(renderDir, "nats.conf"))
	if err != nil {
		// No renderable config for this service — fall back to a plain up.
		return false, nil
	}
	remote, _, err := ssh.Run(conn, "sudo cat "+filepath.Join(svcDir, "nats.conf")+" 2>/dev/null || true")
	if err != nil {
		return false, err
	}
	out, _, _ := ssh.Run(conn, "sudo docker ps -q -f name=nats 2>/dev/null | head -1")
	running := strings.TrimSpace(out) != ""
	return !running || strings.TrimSpace(remote) != strings.TrimSpace(string(rendered)), nil
}

// buildRenderData merges the profile variables with the node context.
func buildRenderData(pf ProvisionFile, h ProvisionHost, dirName, profile string, cfg ServiceConfig) (map[string]any, error) {
	profiles, err := templates.LoadProfiles(dirName)
	if err != nil {
		return nil, err
	}
	if profile == "" {
		profile = "lite"
	}
	prof, ok := profiles[profile]
	if !ok {
		names := make([]string, 0, len(profiles))
		for n := range profiles {
			names = append(names, n)
		}
		return nil, fmt.Errorf("unknown profile %q (available: %s)", profile, strings.Join(names, ", "))
	}
	switch {
	case strings.HasPrefix(dirName, "nats"):
		return natsRenderData(pf, h, prof, cfg)
	default:
		return nil, fmt.Errorf("no render builder for template %q", dirName)
	}
}

// natsRenderData builds the NATS cluster node render context.
func natsRenderData(pf ProvisionFile, h ProvisionHost, prof map[string]any, cfg ServiceConfig) (map[string]any, error) {
	env := os.Getenv
	cluster := env("NATS_CLUSTER_NAME")
	if cluster == "" {
		cluster = "nla"
	}
	var routes []string
	for _, other := range pf.Hosts {
		if other.Name == h.Name {
			continue
		}
		// Only peer with hosts that actually run the NATS service; a
		// consumer-only host (no nats in services) must not appear in the mesh.
		if _, ok := resolveHostConfig(&pf, other).services["nats"]; !ok {
			continue
		}
		routes = append(routes, peerRouteIP(h, other))
	}
	appHash, err := bcryptHash(env("NATS_APP_PASSWORD"))
	if err != nil {
		return nil, err
	}
	svcHash, err := bcryptHash(env("NATS_SVC_PASSWORD"))
	if err != nil {
		return nil, err
	}
	sysHash, err := bcryptHash(env("NATS_SYS_PASSWORD"))
	if err != nil {
		return nil, err
	}
	// App user permissions: the base allow list plus any extra subjects the
	// operator provides via the environment (comma-separated). Keeps the
	// template generic — each microservice declares its own subjects.
	appPublish := []string{
		"demo", "demo.>", "events.>", "links.>", "nats-rpc.>", "nats-pull.>",
		"$KV.>", "$JS.API.>", "$JSC.API.>", "$JS.SNAPSHOT.>", "$JS.ACK.>", "_INBOX.>",
	}
	appPublish = append(appPublish, splitCsv(env("NATS_APP_PUBLISH"))...)
	appSubscribe := []string{
		"demo.>", "events.>", "links.>", "nats-rpc.>", "nats-pull.>", "$KV.>", "_INBOX.>",
	}
	appSubscribe = append(appSubscribe, splitCsv(env("NATS_APP_SUBSCRIBE"))...)
	return map[string]any{
		"ServerName":        h.Name,
		"Advertise":         h.Host,
		"Routes":            routes,
		"ClusterName":       cluster,
		"MaxConnections":    prof["max_connections"],
		"MaxFileStore":      prof["max_file_store"],
		"MaxMemoryStore":    prof["max_memory_store"],
		"MemLimit":          prof["mem_limit"],
		"Cpus":              prof["cpus"],
		"JSKey":             env("NATS_JS_KEY"),
		"AppPasswordHash":   appHash,
		"SvcPasswordHash":   svcHash,
		"SysPasswordHash":   sysHash,
		"ServerTags":        jsonTags(cfg.ServerTags),
		"ClientAdvertise":   cfg.ClientAdvertise,
		"AppPublishAllow":   jsonTags(appPublish),
		"AppSubscribeAllow": jsonTags(appSubscribe),
	}, nil
}

// splitCsv splits a comma-separated list, trimming and dropping empties.
func splitCsv(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// jsonTags renders a server tag list as the JSON array NATS expects
// (e.g. ["region:mia","disk:ssd"]).
func jsonTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	quoted := make([]string, len(tags))
	for i, t := range tags {
		quoted[i] = `"` + t + `"`
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
func bcryptHash(pass string) (string, error) {
	if pass == "" {
		return "", fmt.Errorf("empty password — set NATS_*_PASSWORD in the environment")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt: %w", err)
	}
	return string(b), nil
}

// peerRouteIP returns the address node A uses to reach node B for the cluster
// mesh. When both are on a private network (RFC1918 peer_ip) the private
// address is used (same-DC VLAN); otherwise B's public host is used (a peer
// outside the private network cannot reach 10.0.0.x).
func peerRouteIP(a, b ProvisionHost) string {
	if isPrivateIP(a.PeerIP) && isPrivateIP(b.PeerIP) {
		return b.PeerIP
	}
	return b.Host
}

func isPrivateIP(ip string) bool {
	if strings.HasPrefix(ip, "10.") || strings.HasPrefix(ip, "192.168.") {
		return true
	}
	if strings.HasPrefix(ip, "172.") {
		parts := strings.Split(ip, ".")
		if len(parts) == 4 {
			n, err := strconv.Atoi(parts[1])
			return err == nil && n >= 16 && n <= 31
		}
	}
	return false
}

// uploadDir streams a local directory to a remote one as a tar over stdin.
func uploadDir(conn *golang_ssh.Client, localDir, remoteDir string) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.IsDir() {
			//nolint:gosec // path comes from filepath.Walk within the local render dir
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(tw, f)
			_ = f.Close()
			if cerr != nil {
				return cerr
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	_, _, err = ssh.RunWithStdin(conn,
		fmt.Sprintf("sudo mkdir -p %s && sudo tar xf - -C %s && sudo chown -R sdkops:sdkops %s", remoteDir, remoteDir, remoteDir), buf.String())
	if err != nil {
		return fmt.Errorf("upload %s -> %s: %w", localDir, remoteDir, err)
	}
	return nil
}

// writeRemoteFile writes content to a remote path (0600 via sudo tee).
func writeRemoteFile(conn *golang_ssh.Client, path, content string) error {
	_, _, err := ssh.RunWithStdin(conn, "sudo tee "+path+" >/dev/null", content)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	_, _, err = ssh.Run(conn, "sudo chmod 0600 "+path)
	if err != nil {
		return err
	}
	return nil
}

// exposeServicePorts opens the service.yaml ports to the operator (admin scope).
// Peer access is already granted by the provision.yaml peers section.
// exposeServicePorts opens the service.yaml ports to the operator AND the
// cluster peers in a single ips-scope call, so the two never wipe each other
// (AllowlistExposePort rebuilds a port's chain rules per call).
func exposeServicePorts(conn *golang_ssh.Client, renderDir string, pf ProvisionFile, h ProvisionHost) error {
	//nolint:gosec // renderDir is a MkdirTemp dir we control + fixed filename
	data, err := os.ReadFile(filepath.Join(renderDir, "service.yaml"))
	if err != nil {
		return err
	}
	var svc struct {
		Ports []string `yaml:"ports"`
	}
	if err := yaml.Unmarshal(data, &svc); err != nil {
		return err
	}
	var ips []string
	for raw := range strings.SplitSeq(resolveHostConfig(&pf, h).adminIPs, ",") {
		if raw = strings.TrimSpace(raw); raw != "" {
			ips = append(ips, raw)
		}
	}
	for _, peer := range pf.Peers {
		if peer.To != h.Name {
			continue
		}
		if ip := hostPeerIP(pf, peer.From); ip != "" {
			ips = append(ips, ip)
		}
	}
	if len(ips) == 0 {
		return fmt.Errorf("no operator or peer IPs to expose %s service ports", h.Name)
	}
	for _, p := range svc.Ports {
		hostPort := strings.SplitN(p, ":", 2)[0]
		n, err := strconv.Atoi(strings.TrimSpace(hostPort))
		if err != nil {
			return fmt.Errorf("bad port %q: %w", p, err)
		}
		if err := hardening.AllowlistExposePort(conn, n, "tcp", hardening.PortScopeIPs, ips...); err != nil {
			return fmt.Errorf("expose %d: %w", n, err)
		}
	}
	return nil
}

// hostPeerIP returns the reachable IP of a peer host (peer_ip, fallback host).
func hostPeerIP(pf ProvisionFile, name string) string {
	for _, other := range pf.Hosts {
		if other.Name == name {
			if other.PeerIP != "" {
				return other.PeerIP
			}
			return other.Host
		}
	}
	return ""
}

// removeAll is a safe cleanup helper (best-effort) that satisfies errcheck.
func removeAll(path string) {
	_ = os.RemoveAll(path)
}

// runProvisionCheck is the provision --check dry-run: it parses the fleet,
// resolves each host, renders every declared service and prints the plan
// without SSH-ing into any node.
func runProvisionCheck(path, tags string) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("read provision file: %w", err)
	}
	var pf ProvisionFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("parse provision file: %w", err)
	}
	if err := normalizeProvision(&pf); err != nil {
		return err
	}
	if _, err := validateProvision(&pf); err != nil {
		return err
	}
	hosts := selectHostsByTags(pf.Hosts, tags)
	if len(hosts) == 0 {
		return fmt.Errorf("no hosts match the given tags")
	}
	fmt.Printf("[check] fleet: %d hosts, %d services declared\n", len(hosts), len(resolveHostConfig(&pf, hosts[0]).services))
	for _, h := range hosts {
		r := resolveHostConfig(&pf, h)
		if len(r.services) == 0 {
			fmt.Printf("  %-8s (no services)\n", h.Name)
			continue
		}
		fmt.Printf("  %-8s %s\n", h.Name, h.Host)
		for name, cfg := range r.services {
			if err := checkRenderService(pf, h, name, cfg); err != nil {
				return err
			}
		}
	}
	if missing := missingEnvSecrets(); len(missing) > 0 {
		fmt.Printf("[check] ⚠ env vars NOT set (deploy would fail): %s\n", strings.Join(missing, ", "))
	} else {
		fmt.Println("[check] env secrets: OK")
	}
	fmt.Println("[check] render OK — dry-run only, nothing applied")
	return nil
}

// checkRenderService renders one service for the dry-run and prints its plan.
func checkRenderService(pf ProvisionFile, h ProvisionHost, name string, cfg ServiceConfig) error {
	tmpl, ok := templates.Templates[name+"-dockerized"]
	if !ok {
		return fmt.Errorf("[check] %s: no template for service %q", h.Name, name)
	}
	rendered, err := buildRenderData(pf, h, tmpl.DirName, cfg.Profile, cfg)
	if err != nil {
		return fmt.Errorf("[check] %s/%s: %w", h.Name, name, err)
	}
	dir, err := os.MkdirTemp("", "sdk-ops-check-")
	if err != nil {
		return err
	}
	if err := templates.RenderDir(tmpl.DirName, dir, rendered); err != nil {
		_ = os.RemoveAll(dir)
		return fmt.Errorf("[check] %s/%s render: %w", h.Name, name, err)
	}
	_ = os.RemoveAll(dir)
	routes, _ := rendered["Routes"].([]string)
	fmt.Printf("      %-4s profile=%-4s routes=%v\n", name, cfg.Profile, routes)
	return nil
}

// missingEnvSecrets lists the env vars the NATS service needs to deploy.
func missingEnvSecrets() []string {
	var missing []string
	for _, k := range []string{
		"NATS_APP_PASSWORD", "NATS_SVC_PASSWORD", "NATS_SYS_PASSWORD", "NATS_JS_KEY",
		"NATS_CERT_DIR", "NATS_SENDER_NK", "NATS_RECIPIENT_PUB",
		"S3_BUCKET", "S3_ENDPOINT", "S3_ACCESS_KEY", "S3_SECRET_KEY",
	} {
		if os.Getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	return missing
}
