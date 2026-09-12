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

// cleanupStaleServices removes services previously deployed on the host that
// are no longer declared in the desired YAML. It detects the previous YAML
// by listing /opt/sdk-ops/services and uninstalls anything not in desired.
// Families are mutually exclusive: pgsql-bare/docker/cluster and
// yuga-bare/docker/cluster — switching YAML auto-cleans the previous family
// to avoid saturating the server. libsql is never auto-removed (banca).
func cleanupStaleServices(conn *golang_ssh.Client, desired ProvisionServices) {
	out, _, err := ssh.Run(conn, "ls /opt/sdk-ops/services 2>/dev/null | tr '\\n' ' ' || true")
	if err != nil {
		return
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return
	}
	for name := range strings.FieldsSeq(out) {
		if _, ok := desired[name]; ok {
			continue
		}
		if name == "libsql" || strings.HasPrefix(name, "libsql") {
			verbosef("cleanup: keep stale %s (libsql en banca)", name)
			continue
		}
		verbosef("cleanup: stale service %s not in desired YAML — uninstall", name)
		uninstallService(conn, name)
		verbosef("cleanup: removed %s", name)
	}
}

// serviceUninstallers maps a service name to the remote uninstall commands
// specific to it, BEFORE the generic service-dir removal. Compose stacks are
// covered by the generic compose-down step; bare services declare their
// systemd units; cluster services uninstall their release/CR + namespace.
// Add a row when a template grows a non-compose runtime (native/k3s).
type serviceUninstall struct {
	units  []string // systemd units to disable --now (bare-native services)
	script []string // extra ad-hoc remote commands (native process kills, helm/kubectl)
}

var serviceUninstalls = map[string]serviceUninstall{
	// native (bare) services
	"pgsql-bare": {units: []string{"postgresql"}, script: []string{
		"sudo pg_ctlcluster 18 main stop 2>/dev/null || sudo systemctl stop postgresql 2>/dev/null || true",
	}},
	"nats-bare": {units: []string{"nats-server"}},
	"df-bare":   {units: []string{"dragonfly-primary", "dragonfly-replica-1", "dragonfly-replica-2", "haproxy"}},
	"etcd-bare": {units: []string{"etcd"}},
	// k3s cluster services (release/CR + namespace; the dragonfly operator
	// itself stays — it is shared infrastructure)
	"yuga-cluster": {script: []string{
		"helm uninstall yb-demo -n yb-demo 2>/dev/null || true",
		"sudo k3s kubectl delete ns yb-demo --force --grace-period=0 2>/dev/null || true",
	}},
	"nats-cluster": {script: []string{
		"sudo /usr/local/bin/helm uninstall nats -n nats 2>/dev/null || true",
		"sudo k3s kubectl delete ns nats --force --grace-period=0 2>/dev/null || true",
	}},
	"etcd-cluster": {script: []string{
		"sudo /usr/local/bin/helm uninstall etcd -n etcd 2>/dev/null || true",
		"sudo k3s kubectl delete ns etcd --force --grace-period=0 2>/dev/null || true",
	}},
	"df-cluster": {script: []string{
		// The dragonfly operator stays (shared); the CR + namespace go.
		"sudo k3s kubectl delete dragonfly df -n df --force --grace-period=0 2>/dev/null || true",
		"sudo k3s kubectl delete ns df --force --grace-period=0 2>/dev/null || true",
	}},
	"valkey-cluster": {script: []string{
		"sudo k3s kubectl delete ns valkey --force --grace-period=0 2>/dev/null || true",
	}},
	// native process families without clean units (yuga bare processes)
	"yuga-docker": {script: []string{
		"sudo pkill -f yugabyted 2>/dev/null || true; sudo pkill -f yb-master 2>/dev/null || true; sudo pkill -f yb-tserver 2>/dev/null || true",
	}},
	"yuga-bare": {script: []string{
		"sudo pkill -f yugabyted 2>/dev/null || true; sudo pkill -f yb-master 2>/dev/null || true; sudo pkill -f yb-tserver 2>/dev/null || true",
	}},
}

// uninstallService removes one stale service: its family-specific bits first
// (systemd units, helm releases, native processes), then the compose stack if
// any, then the service directory.
func uninstallService(conn *golang_ssh.Client, name string) {
	svcDir := "/opt/sdk-ops/services/" + name
	// Compose stacks (pgsql-docker, yuga-docker, pgsql-cluster, etcd, df...).
	if _, _, err := ssh.Run(conn, fmt.Sprintf("test -f %s/docker-compose.yml", svcDir)); err == nil {
		_, _, _ = ssh.Run(conn, fmt.Sprintf("cd %s && sudo docker compose down -v 2>/dev/null || true", svcDir))
	}
	if u, ok := serviceUninstalls[name]; ok {
		for _, unit := range u.units {
			_, _, _ = ssh.Run(conn, fmt.Sprintf("sudo systemctl disable --now %s 2>/dev/null || true", unit))
		}
		for _, cmd := range u.script {
			_, _, _ = ssh.Run(conn, cmd)
		}
	}
	_, _, _ = ssh.Run(conn, "sudo rm -rf "+svcDir+" 2>/dev/null || true")
}

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

	cleanupStaleServices(conn, r.services)

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
	order := []string{
		"etcd", "etcd-bare", "etcd-cluster",
		"pgsql-cluster",
		"nats", "nats-bare", "nats-cluster",
		"df", "df-bare", "df-cluster",
		"valkey-cluster",
		"libsql",
	}
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
	case "nats", "nats-bare":
		return wireNATSOn(conn, svcDir, nodeName)
	case "etcd":
		return wireEtcdOn(conn, svcDir, nodeName)
	case "pgsql-cluster":
		return wirePGOn(conn, svcDir, nodeName, cfg, pf, h)
	case "df-cluster":
		return wireDFOn(conn, svcDir)
	default:
		// Dockerized templates (yugabyte, libsql, df, ...) need no special
		// wiring — they are self-contained compose stacks driven by init.sh.
		verbosef("service %s: no extra wiring (dockerized template)", name)
		return nil
	}
}

// wireDFOn writes the service .env with the CR password and the S3 credentials
// the per-service scripts use. Secrets never live in the fleet YAML.
func wireDFOn(conn *golang_ssh.Client, svcDir string) error {
	pw := os.Getenv("DF_PASSWORD")
	if pw == "" {
		pw = "dragonfly"
	}
	lines := []string{fmt.Sprintf("DF_PASSWORD='%s'", strings.ReplaceAll(pw, "'", `'\''`))}
	for _, k := range []string{"S3_BUCKET", "S3_ENDPOINT", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_PREFIX"} {
		if v := os.Getenv(k); v != "" {
			v = strings.ReplaceAll(v, "'", `'\''`)
			lines = append(lines, fmt.Sprintf("%s='%s'", k, v))
		}
	}
	cmd := fmt.Sprintf("umask 077; cat > %s/.env <<'SDKOPS_DF_ENV'\n%s\nSDKOPS_DF_ENV", svcDir, strings.Join(lines, "\n"))
	if _, _, err := ssh.Run(conn, cmd); err != nil {
		return fmt.Errorf("write df-cluster .env: %w", err)
	}
	return nil
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
		return fmt.Errorf("no template for service %q (available: nats, df, libsql, pg, etcd, postgres)", name)
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

	return deployFromRender(conn, pf, h, name, renderDir, svcDir, recreate)
}

// deployFromRender dispatches the final deploy step: native init, init.sh
// (certs/quorum), or plain compose up.
func deployFromRender(conn *golang_ssh.Client, pf ProvisionFile, h ProvisionHost, name, renderDir, svcDir string, recreate bool) error {
	if _, err := os.Stat(filepath.Join(renderDir, "docker-compose.yml")); err != nil {
		return deployNativeService(conn, name, pf, h, svcDir)
	}
	if _, err := os.Stat(filepath.Join(renderDir, "init.sh")); err == nil {
		return deployViaInit(conn, name, pf, h, renderDir, svcDir)
	}
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

func deployViaInit(conn *golang_ssh.Client, name string, pf ProvisionFile, h ProvisionHost, renderDir, svcDir string) error {
	verbosef("service %s on %s: init.sh up", name, h.Name)
	if _, _, err := ssh.Run(conn, fmt.Sprintf("cd %s && sudo bash init.sh", svcDir)); err != nil {
		return fmt.Errorf("init %s: %w", name, err)
	}
	if err := waitServiceUp(conn, name, pf, h); err != nil {
		return err
	}
	if err := exposeServicePorts(conn, renderDir, pf, h); err != nil {
		return err
	}
	return nil
}

// deployNativeService handles templates without docker-compose (pgsql-bare,
// yuga-bare) by running their init.sh directly on the host.
func deployNativeService(conn *golang_ssh.Client, name string, pf ProvisionFile, h ProvisionHost, svcDir string) error {
	verbosef("service %s on %s: native init (no docker-compose)", name, h.Name)
	if _, _, err := ssh.Run(conn, fmt.Sprintf("cd %s && sudo bash init.sh", svcDir)); err != nil {
		return fmt.Errorf("native init %s: %w", name, err)
	}
	if err := waitServiceUp(conn, name, pf, h); err != nil {
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
		"nats":          {"nats.conf", "nats-0.conf"},
		"etcd":          {"docker-compose.yml"},
		"pgsql-cluster": {"patroni.yml", "pgdog.toml", "docker-compose.yml", "pgbackrest.conf"},
		"nats-bare":     {"nats.conf"},
		"etcd-bare":     {"etcd.conf.yml"},
		"nats-cluster":  {"values.yaml"},
		"etcd-cluster":  {"values.yaml"},
		"df-cluster":    {"dragonfly.yaml"},
	}
	files, ok := cfgFiles[name]
	if !ok {
		return false, nil
	}
	root, err := os.OpenRoot(renderDir)
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	for _, cfgFile := range files {
		in, err := root.Open(cfgFile)
		if err != nil {
			continue // this config is not rendered for the service — try the next
		}
		rendered, err := io.ReadAll(in)
		_ = in.Close()
		if err != nil {
			continue
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
	// yuga-bare / pgsql-bare are native installs driven by env-var defaults —
	// no profiles.yaml exists; render data is light (resource hints only).
	if dirName == "yuga-bare" || dirName == "pgsql-bare" {
		return map[string]any{
			"MemLimit":  "512m",
			"Cpus":      "1",
			"Provision": true,
		}, nil
	}
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
	case strings.HasPrefix(dirName, "nats-cluster"):
		return natsClusterRenderData(prof)
	case strings.HasPrefix(dirName, "nats-bare"):
		// Native nats-server on the host reuses the docker topology context:
		// the same routes/advertise/certs logic, different systemd install.
		return natsRenderData(pf, h, prof, cfg)
	case dirName == "df-cluster":
		return dfClusterRenderData(prof)
	case dirName == "df-bare":
		return dfRenderData(prof)
	case dirName == "valkey-cluster":
		return valkeyClusterRenderData(prof)
	case dirName == "etcd-cluster":
		return etcdClusterRenderData(prof)
	case dirName == "etcd-bare":
		// Native etcd on the host: same static bootstrap topology as the DCS.
		return etcdRenderData(pf, h, prof, cfg)
	case strings.HasPrefix(dirName, "nats"):
		return natsRenderData(pf, h, prof, cfg)
	case dirName == "etcd":
		return etcdRenderData(pf, h, prof, cfg)
	case dirName == "pgsql-cluster":
		return pgRenderData(pf, h, prof, cfg)
	case strings.HasPrefix(dirName, "pgsql"):
		// pgsql-docker / pgsql-bare are self-contained (compose stack or
		// native scripts driven by env-var defaults) — render data is light.
		return map[string]any{
			"MemLimit":  prof["mem_limit"],
			"Cpus":      prof["cpus"],
			"Provision": true,
		}, nil
	case strings.HasPrefix(dirName, "yuga"):
		return yugabyteRenderData(pf, h, prof, cfg)
	case strings.HasPrefix(dirName, "libsql"):
		return libsqlRenderData(prof)
	case strings.HasPrefix(dirName, "df"):
		return dfRenderData(prof)
	default:
		return nil, fmt.Errorf("no render builder for template %q", dirName)
	}
}

// libsqlRenderData builds the render context for templates/libsql-dockerized.
// The profile variables (SQLD_MEM, ETCD_MEM, ...) become the Go-template vars
// the compose file consumes ({{ .SQLD_MEM }}), so a fleet YAML profile
// (lite/normal/medium/large) sizes the whole 3-node stack deterministically.
func libsqlRenderData(prof map[string]any) (map[string]any, error) {
	envOr := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	data := map[string]any{
		"SQLD_MEM":             prof["SQLD_MEM"],
		"SQLD_CPUS":            prof["SQLD_CPUS"],
		"ETCD_MEM":             prof["ETCD_MEM"],
		"ETCD_CPUS":            prof["ETCD_CPUS"],
		"CONTROLLER_MEM":       prof["CONTROLLER_MEM"],
		"CONTROLLER_CPUS":      prof["CONTROLLER_CPUS"],
		"ROUTER_MEM":           prof["ROUTER_MEM"],
		"ROUTER_CPUS":          prof["ROUTER_CPUS"],
		"REPLICAS":             prof["REPLICAS"],
		"LIBSQL_HTTP":          envOr("LIBSQL_HTTP", "8080"),
		"LIBSQL_REPLICA_HTTP":  envOr("LIBSQL_REPLICA_HTTP", "8081"),
		"LIBSQL_REPLICA2_HTTP": envOr("LIBSQL_REPLICA2_HTTP", "8082"),
		"LIBSQL_HTTP_TLS":      envOr("LIBSQL_HTTP_TLS", "8443"),
		"CONTROLLER_PORT":      envOr("CONTROLLER_PORT", "9090"),
		"Provision":            true,
	}
	return data, nil
}

// dfRenderData builds the render context for templates/df-dockerized.
// Same pattern as libsqlRenderData: the profile variables (DF_MEM,
// HAPROXY_MEM, ...) become the Go-template vars the compose file consumes
// ({{ .DF_MEM }}), so a fleet YAML profile sizes the KV stack deterministically.
func dfRenderData(prof map[string]any) (map[string]any, error) {
	envOr := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	data := map[string]any{
		"DF_MEM":          prof["DF_MEM"],
		"DF_CPUS":         prof["DF_CPUS"],
		"HAPROXY_MEM":     prof["HAPROXY_MEM"],
		"HAPROXY_CPUS":    prof["HAPROXY_CPUS"],
		"DF_PASSWORD":     envOr("DF_PASSWORD", "dragonfly"),
		"DF_PORT":         envOr("DF_PORT", "6379"),
		"DF_REPLICA_PORT": envOr("DF_REPLICA_PORT", "6380"),
		"Provision":       true,
	}
	return data, nil
}

// natsClusterRenderData builds the render context for templates/nats-cluster
// (k3s via the official nats helm chart). The fleet YAML profile sizes the
// statefulset; namespace/release/tag come from the environment (never YAML).
func natsClusterRenderData(prof map[string]any) (map[string]any, error) {
	envOr := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	data := map[string]any{
		"Namespace":       envOr("NATS_K8S_NAMESPACE", "nats"),
		"Release":         envOr("NATS_K8S_RELEASE", "nats"),
		"Tag":             envOr("NATS_K8S_TAG", "2.14-alpine"),
		"Replicas":        envOr("NATS_K8S_REPLICAS", "3"),
		"Cpu":             prof["cpu"],
		"Mem":             prof["mem"],
		"JSStorage":       prof["js_storage"],
		"StorageClass":    envOr("NATS_K8S_STORAGE_CLASS", "local-path"),
		"MaxPayload":      prof["max_payload"],
		"NatsBox":         envOr("NATS_K8S_NATS_BOX", "true"),
		"HelmVersion":     envOr("NATS_K8S_HELM_VERSION", "v3.15.4"),
		"Nack":            envOr("NATS_K8S_NACK", "true"),
		"NackTag":         envOr("NATS_K8S_NACK_TAG", "0.24.0"),
		"NackControlLoop": envOr("NATS_K8S_NACK_CONTROL_LOOP", "false"),
		"CliVersion":      envOr("NATS_CLI_VERSION", "0.4.0"),
		"Provision":       true,
	}
	return data, nil
}

// dfClusterRenderData builds the render context for templates/df-cluster
// (k3s via the official dragonflydb operator). The operator manages the
// primary/replica set declaratively; the service points at the master.
func dfClusterRenderData(prof map[string]any) (map[string]any, error) {
	envOr := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	data := map[string]any{
		"Namespace":  envOr("DF_K8S_NAMESPACE", "df"),
		"Name":       envOr("DF_K8S_NAME", "df"),
		"Tag":        envOr("DF_K8S_TAG", "v1.30.1"),
		"Replicas":   envOr("DF_K8S_REPLICAS", "3"),
		"CPU":        prof["cpu"],
		"Mem":        prof["mem"],
		"DFPassword": envOr("DF_PASSWORD", "dragonfly"),
		// Native S3 snapshots (operator feature, dragonfly >= v1.12): only when
		// the operator env carries the S3 settings — otherwise DR is the
		// explicit backup-s3.sh/restore-s3.sh cycle.
		"S3Snapshot":   os.Getenv("S3_BUCKET") != "" && os.Getenv("S3_ENDPOINT") != "",
		"S3Bucket":     envOr("S3_BUCKET", ""),
		"S3Prefix":     envOr("S3_PREFIX", "df"),
		"S3Endpoint":   envOr("S3_ENDPOINT", ""),
		"S3Region":     envOr("S3_REGION", "us-east-005"),
		"S3AccessKey":  envOr("S3_ACCESS_KEY", ""),
		"S3SecretKey":  envOr("S3_SECRET_KEY", ""),
		"SnapshotCron": envOr("DF_K8S_SNAPSHOT_CRON", "0 */6 * * *"),
		"OperatorTag":  envOr("DF_K8S_OPERATOR_TAG", "v1.1.4"),
		"OperatorManifest": envOr("DF_K8S_OPERATOR_MANIFEST",
			"https://raw.githubusercontent.com/dragonflydb/dragonfly-operator/v1.1.4/manifests/dragonfly-operator.yaml"),
		"Provision": true,
	}
	return data, nil
}

// valkeyClusterRenderData builds the render context for templates/valkey-cluster
// (k3s via StatefulSet + Sentinel). Valkey is Redis-compatible, no operator needed.
func valkeyClusterRenderData(prof map[string]any) (map[string]any, error) {
	envOr := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	data := map[string]any{
		"Namespace":     envOr("VK_K8S_NAMESPACE", "valkey"),
		"Name":          envOr("VK_K8S_NAME", "valkey"),
		"Tag":           envOr("VK_K8S_TAG", "8.1.3"),
		"Replicas":      envOr("VK_K8S_REPLICAS", "3"),
		"CPU":           prof["CPU"],
		"Mem":           prof["Mem"],
		"MaxMemory":     envOr("VK_K8S_MAX_MEMORY", "256mb"),
		"Password":      envOr("VK_PASSWORD", "valkey"),
		"SentinelQuorum": envOr("VK_K8S_SENTINEL_QUORUM", "2"),
	}
	return data, nil
}

// etcdClusterRenderData builds the render context for templates/etcd-cluster
// (k3s via the bitnami etcd helm chart) — an external DCS for services inside
// the cluster that need etcd (k3s itself ships its own embedded one).
func etcdClusterRenderData(prof map[string]any) (map[string]any, error) {
	envOr := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	data := map[string]any{
		"Namespace":   envOr("ETCD_K8S_NAMESPACE", "etcd"),
		"Release":     envOr("ETCD_K8S_RELEASE", "etcd"),
		"Tag":         envOr("ETCD_K8S_TAG", "3.5.15"),
		"Replicas":    envOr("ETCD_K8S_REPLICAS", "3"),
		"CPU":         prof["cpu"],
		"Mem":         prof["mem"],
		"Storage":     prof["storage"],
		"HelmVersion": envOr("ETCD_K8S_HELM_VERSION", "v3.15.4"),
		"Provision":   true,
	}
	return data, nil
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
		if !isServiceVariant(resolveHostConfig(&pf, other).services, "nats") {
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
		if isServiceVariant(resolveHostConfig(&pf, other).services, "nats") {
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
		if !isServiceVariant(resolveHostConfig(&pf, other).services, "nats") {
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
	if a.PeerIP == "" || b.PeerIP == "" {
		return b.Host
	}
	// Same-family peers reach each other directly via peer_ip: both on the
	// private VLAN, or both with global addresses (v6 keeps v4 free for
	// 80/443). Mixed families (private v4 <-> global v6) fall back to the
	// reachable public host.
	aPriv, bPriv := isPrivateIP(a.PeerIP), isPrivateIP(b.PeerIP)
	if aPriv == bPriv {
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

func isIPv6(ip string) bool {
	return strings.Contains(ip, ":")
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
// readRenderedService reads the rendered service.yaml from a MkdirTemp render
// dir via os.Root (fixed filename, no traversal possible).
func readRenderedService(renderDir string) ([]byte, error) {
	root, err := os.OpenRoot(renderDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	in, err := root.Open("service.yaml")
	if err != nil {
		return nil, err
	}
	defer func() { _ = in.Close() }()
	return io.ReadAll(in)
}

// exposeServicePorts opens the service.yaml ports to the operator AND the
// cluster peers in a single ips-scope call, so the two never wipe each other
// (AllowlistExposePort rebuilds a port's chain rules per call).
func exposeServicePorts(conn *golang_ssh.Client, renderDir string, pf ProvisionFile, h ProvisionHost) error {
	if pf.Hardening != nil && !*pf.Hardening {
		return nil // no firewall in the no-hardening mode — the ports are open
	}
	data, err := readRenderedService(renderDir)
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
		hostPort, _, _ := strings.Cut(p, ":")
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
		fmt.Printf("[check] [WARN] env vars NOT set (deploy would fail): %s\n", strings.Join(missing, ", "))
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
				fmt.Printf("[check] [WARN] %s has no IPv6 (%s) — the docker-mode install (docker, images, S3) needs a public route (IPv6 or a NAT egress); without it the deploy fails at the install step\n", h.Name, h.Host)
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
