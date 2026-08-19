package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	f := hostInfraFlags(pf, h, port)
	conn, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("services: connect %s: %w", h.Name, err)
	}
	defer closeConn(conn)

	// Deterministic service order — the dependencies first (etcd = the DCS the
	// postgres needs; the map iteration alone is random and a postgres deploy
	// racing its own etcd would miss the DCS during the bootstrap).
	for _, name := range orderedServiceNames(r.services) {
		cfg := r.services[name]
		if err := deployServiceOn(conn, pf, h, name, cfg); err != nil {
			return fmt.Errorf("services %s on %s: %w", name, h.Name, err)
		}
	}
	return nil
}

// orderedServiceNames sorts the declared services deterministically: the
// dependency order first (etcd before postgres), the rest alphabetically.
func orderedServiceNames(services ProvisionServices) []string {
	order := []string{"etcd", "postgres", "nats", "kv", "libsql"}
	var out []string
	seen := map[string]bool{}
	for _, name := range order {
		if _, ok := services[name]; ok && !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	var rest []string
	for name := range services {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// deployServiceOn deploys one service (e.g. nats) onto a node.
// wireService dispatches the per-service wiring (certs, secrets, CLI, timers).
func wireService(conn *golang_ssh.Client, svcDir, nodeName, name string, cfg ServiceConfig, pf ProvisionFile, h ProvisionHost) error {
	switch name {
	case "nats":
		return wireNATSOn(conn, svcDir, nodeName)
	case "etcd":
		return wireEtcdOn(conn, svcDir, nodeName)
	case "postgres":
		return wirePGOn(conn, svcDir, nodeName, cfg, pf, h)
	default:
		return fmt.Errorf("no wiring for service %q", name)
	}
}

// resolveServiceTemplate resolves the template for a service: the exact name
// first (etcd, postgres), then the legacy "<name>-dockerized" convention.
func resolveServiceTemplate(name string) (templates.Template, bool) {
	tmpl, ok := templates.Templates[name]
	if !ok {
		tmpl, ok = templates.Templates[name+"-dockerized"]
	}
	return tmpl, ok
}

func deployServiceOn(conn *golang_ssh.Client, pf ProvisionFile, h ProvisionHost, name string, cfg ServiceConfig) error {
	verbosef("service %s on %s: render", name, h.Name)
	tmpl, ok := resolveServiceTemplate(name)
	if !ok {
		return fmt.Errorf("no template for service %q (available: nats, kv, libsql, pg, etcd, postgres)", name)
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
	if err := maybeWriteSingleVPS(renderDir, data, name, h.Name); err != nil {
		return err
	}

	svcDir := "/opt/sdk-ops/services/" + name
	if _, _, err := ssh.Run(conn, "sudo mkdir -p "+svcDir); err != nil {
		return err
	}
	// Decide before uploading: compare the rendered config vs the deployed one
	// (the upload overwrites the remote, so the diff must be taken first).
	recreate, err := serviceConfigChanged(conn, renderDir, svcDir, name)
	if err != nil {
		return err
	}
	verbosef("service %s on %s: upload + wiring", name, h.Name)
	if err := uploadDir(conn, renderDir, svcDir); err != nil {
		return err
	}

	// Service-specific wiring (certs, secrets, CLI, timers).
	if err := wireService(conn, svcDir, h.Name, name, cfg, pf, h); err != nil {
		return err
	}

	// Idempotent deploy: recreate the container only when a mounted config
	// changed (compose up alone does not restart on config-file changes).
	verbosef("service %s on %s: compose up (recreate=%v)", name, h.Name, recreate)
	up := fmt.Sprintf("cd %s && sudo docker compose up -d", svcDir)
	if recreate {
		up += " --force-recreate"
	}
	if _, _, err := ssh.Run(conn, up); err != nil {
		return err
	}
	if err := waitServiceUp(conn, name, pf, h); err != nil {
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
func serviceConfigChanged(conn *golang_ssh.Client, renderDir, svcDir, name string) (bool, error) {
	// Per-service config to diff: nats.conf (or nats-0.conf), patroni.yml,
	// docker-compose.yml (etcd), postgresql.auto.conf...
	cfgFiles := map[string][]string{
		"nats":     {"nats.conf", "nats-0.conf"},
		"etcd":     {"docker-compose.yml"},
		"postgres": {"patroni.yml", "pgdog.toml", "docker-compose.yml", "pgbackrest.conf"},
	}
	files, ok := cfgFiles[name]
	if !ok {
		return false, nil
	}
	for _, cfgFile := range files {
		//nolint:gosec // renderDir is a MkdirTemp dir we control + fixed filename
		rendered, err := os.ReadFile(filepath.Join(renderDir, cfgFile))
		if err != nil {
			continue // this config is not rendered for the service — try the next
		}
		remote, _, err := ssh.Run(conn, "sudo cat "+filepath.Join(svcDir, cfgFile)+" 2>/dev/null || true")
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(remote) != strings.TrimSpace(string(rendered)) {
			return true, nil
		}
	}
	container := name
	out, _, _ := ssh.Run(conn, "sudo docker ps -q -f name="+container+" 2>/dev/null | head -1")
	running := strings.TrimSpace(out) != ""
	return !running, nil
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
	case dirName == "etcd":
		return etcdRenderData(pf, h, prof, cfg)
	case dirName == "postgres":
		return pgRenderData(pf, h, prof, cfg)
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
	replicas, nodeCount, singleVPS, routes, containers := natsTopology(pf, h, cfg)
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
		"Advertise":         meshAdvertise(pf, h),
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
		"Replicas":          replicas,
		"NodeCount":         nodeCount,
		"SingleVPS":         singleVPS,
		"Containers":        containers,
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
// meshAdvertise returns the address the peers reach this node by. When the
// node and every NATS peer are on a private network, the private IP is used
// (firewall allows the private source); otherwise the public host is used so
// external peers can reach back.
func meshAdvertise(pf ProvisionFile, h ProvisionHost) string {
	advertise := h.Host
	allPrivate := h.PeerIP != "" && isPrivateIP(h.PeerIP)
	for _, other := range pf.Hosts {
		if other.Name == h.Name {
			continue
		}
		if _, ok := resolveHostConfig(&pf, other).services["nats"]; !ok {
			continue
		}
		if other.PeerIP == "" || !isPrivateIP(other.PeerIP) {
			allPrivate = false
		}
	}
	if allPrivate {
		advertise = h.PeerIP
	}
	return advertise
}

// natsTopology derives the replica mode for a NATS node: the desired replica
// count, the number of hosts running NATS and whether the replicas are served
// by N containers on this same VPS (singleVPS) or by N peer VPS nodes.
func natsTopology(pf ProvisionFile, h ProvisionHost, cfg ServiceConfig) (int, int, bool, []string, []int) {
	nodeCount := 0
	for _, other := range pf.Hosts {
		if _, ok := resolveHostConfig(&pf, other).services["nats"]; ok {
			nodeCount++
		}
	}
	replicas := cfg.Replicas
	if replicas <= 0 {
		replicas = nodeCount // default: one copy per NATS node
	}
	singleVPS := replicas > 1 && nodeCount == 1
	var routes []string
	if singleVPS {
		routes = singleVPSRoutes(replicas)
	} else {
		routes = natsSeedRoutes(pf, h, cfg.Seeds)
	}
	var containers []int
	if singleVPS {
		for i := 0; i < replicas; i++ {
			containers = append(containers, i)
		}
	}
	return replicas, nodeCount, singleVPS, routes, containers
}

// natsSeedRoutes returns the explicit mesh routes for a node: the first
// `seeds` peers (default 3, at most nodeCount-1). Gossip discovers the rest.
func natsSeedRoutes(pf ProvisionFile, h ProvisionHost, seeds int) []string {
	if seeds <= 0 {
		seeds = 3
	}
	var peers []ProvisionHost
	for _, other := range pf.Hosts {
		if other.Name == h.Name {
			continue
		}
		if _, ok := resolveHostConfig(&pf, other).services["nats"]; !ok {
			continue
		}
		peers = append(peers, other)
	}
	if seeds > len(peers) {
		seeds = len(peers)
	}
	var routes []string
	for i := 0; i < seeds; i++ {
		routes = append(routes, peerRouteIP(h, peers[i]))
	}
	return routes
}

// singleVPSRoutes returns the internal container mesh routes.
func singleVPSRoutes(replicas int) []string {
	var routes []string
	for i := 1; i < replicas; i++ {
		routes = append(routes, fmt.Sprintf("nats-%d", i))
	}
	return routes
}

// maybeWriteSingleVPS replaces the single-node template output with the
// single-VPS multi-container setup when a host declares replicas>1 with no
// peer NATS hosts.
func maybeWriteSingleVPS(renderDir string, data map[string]any, name, hostName string) error {
	if name != "nats" {
		return nil
	}
	single, _ := data["SingleVPS"].(bool)
	if !single {
		return nil
	}
	replicas, _ := data["Replicas"].(int)
	if replicas < 1 {
		replicas = 3
	}
	return writeSingleVPSSetup(renderDir, data, replicas, hostName)
}

func bcryptHash(pass string) (string, error) {	if pass == "" {
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
	root, err := os.OpenRoot(localDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err = filepath.WalkDir(localDir, func(path string, d os.DirEntry, err error) error {
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
		if strings.Contains(rel, "..") {
			return fmt.Errorf("invalid path %q", rel)
		}
		return writeTarEntry(tw, root, rel, d)
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	_, _, err = ssh.RunWithStdin(conn,
		fmt.Sprintf("sudo mkdir -p %s && sudo tar xf - -C %s && (id sdkops >/dev/null 2>&1 && sudo chown -R sdkops:sdkops %s || true)", remoteDir, remoteDir, remoteDir), buf.String())
	if err != nil {
		return fmt.Errorf("upload %s -> %s: %w", localDir, remoteDir, err)
	}
	return nil
}

func writeTarEntry(tw *tar.Writer, root *os.Root, rel string, d os.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = rel
	if d.IsDir() {
		hdr.Name += "/"
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if !d.IsDir() {
		f, err := root.Open(rel)
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
	if pf.Hardening != nil && !*pf.Hardening {
		return nil // no firewall in the no-hardening mode — the ports are open
	}
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
	// The docker mode (no k3s) installs docker, pulls the images and archives
	// to S3 — every node needs a public route. Without IPv6 (or a NAT egress)
	// the deploy will fail at the install step, so warn early in the dry-run.
	if pf.Mode != "k3s" {
		for _, h := range hosts {
			hasV6 := strings.Contains(h.Host, ":") || strings.Contains(h.PeerIP, ":")
			if !hasV6 {
				fmt.Printf("[check] ⚠ %s has no IPv6 (%s) — the docker-mode install (docker, images, S3) needs a public route (IPv6 or a NAT egress); without it the deploy fails at the install step\n", h.Name, h.Host)
			}
		}
	}
	fmt.Println("[check] render OK — dry-run only, nothing applied")
	return nil
}

// checkRenderService renders one service for the dry-run and prints its plan.
func checkRenderService(pf ProvisionFile, h ProvisionHost, name string, cfg ServiceConfig) error {
	tmpl, ok := resolveServiceTemplate(name)
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
