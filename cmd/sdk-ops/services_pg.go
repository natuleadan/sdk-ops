package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// ---------------------------------------------------------------------------
// Topology helpers — hosts declaring the postgres/etcd services.

// pgNodes lists the hosts that declare the postgres service (the Patroni members).
func pgNodes(pf ProvisionFile) []ProvisionHost {
	var out []ProvisionHost
	for _, h := range pf.Hosts {
		if _, ok := resolveHostConfig(&pf, h).services["postgres"]; ok {
			out = append(out, h)
		}
	}
	return out
}

// etcdNodes lists the hosts that declare the etcd service (the DCS members).
func etcdNodes(pf ProvisionFile) []ProvisionHost {
	var out []ProvisionHost
	for _, h := range pf.Hosts {
		if _, ok := resolveHostConfig(&pf, h).services["etcd"]; ok {
			out = append(out, h)
		}
	}
	return out
}

// poolerNode reports whether this host runs the PgDog entry. Default: EVERY
// postgres node runs its own PgDog (the "local-read" pattern — each app reads
// via its internal pooler). An explicit `pooler: true` restricts it to one.
func poolerNode(pf ProvisionFile, h ProvisionHost, cfg ServiceConfig) bool {
	if cfg.Pooler {
		return h.Name == poolerHostName(pf)
	}
	for _, n := range pgNodes(pf) {
		if n.Name == h.Name {
			return true
		}
	}
	return false
}

func poolerHostName(pf ProvisionFile) string {
	nodes := pgNodes(pf)
	if len(nodes) == 0 {
		return ""
	}
	return nodes[0].Name
}

// pgPrimaryNode is the PRIMARY candidate: the first host that declares the
// postgres service (the restore target; the replicas clone from it).
func pgPrimaryNode(pf ProvisionFile) ProvisionHost {
	nodes := pgNodes(pf)
	if len(nodes) == 0 {
		return ProvisionHost{}
	}
	return nodes[0]
}

// ensurePGDogImage — the pgdog image must exist on the node. An IPv6-only
// provider cannot pull ghcr.io (the registry has no AAAA): the fallback
// downloads the image from the OPERATOR's machine (pure-Go OCI client — NO
// docker required) and ships the tarball over the SSH pipe (docker load).
// The tarball is a TEMP staging artifact — removed after the load; the image
// lives in the node's docker store (the re-apply skips via image inspect).
func ensurePGDogImage(conn *goss.Client, nodeName string) error {
	image := "ghcr.io/pgdogdev/pgdog:v0.1.52"
	out, _, _ := ssh.Run(conn, "docker image inspect "+image+" >/dev/null 2>&1 && echo yes || echo no")
	if strings.Contains(out, "yes") {
		return nil
	}
	// The node tries the direct pull first (v4 / vlan / v6 hosts pull normally).
	if _, _, err := ssh.Run(conn, "docker pull "+image+" >/dev/null 2>&1"); err == nil {
		_, _, _ = ssh.Run(conn, `logger -t sdk-ops-pg "pgdog image pulled directly"`)
		return nil
	}
	// Fallback: the operator's machine downloads the image (no docker) and
	// ships it over the SSH pipe (the temp staging, removed after the load).
	tmpDir, err := os.MkdirTemp("", "sdk-ops-pgdog-")
	if err != nil {
		return err
	}
	defer removeAll(tmpDir)
	img, err := crane.Pull(image)
	if err != nil {
		return fmt.Errorf("pgdog image pull (operator side): %w", err)
	}
	if err := crane.Save(img, image, filepath.Join(tmpDir, "pgdog.tar")); err != nil {
		return fmt.Errorf("pgdog image save (operator side): %w", err)
	}
	root, err := os.OpenRoot(tmpDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	in, err := root.Open("pgdog.tar")
	if err != nil {
		return err
	}
	data, err := io.ReadAll(in)
	_ = in.Close()
	if err != nil {
		return err
	}
	_, _, err = ssh.RunWithStdin(conn,
		"sudo mkdir -p /tmp/sdk-ops-images && sudo tee /tmp/sdk-ops-images/pgdog.tar >/dev/null", string(data))
	if err != nil {
		return fmt.Errorf("pgdog image ship to %s: %w", nodeName, err)
	}
	out, _, err = ssh.Run(conn,
		"sudo docker load -i /tmp/sdk-ops-images/pgdog.tar >/dev/null 2>&1 && sudo rm -f /tmp/sdk-ops-images/pgdog.tar && docker image inspect "+image+" >/dev/null 2>&1 && echo loaded || echo load-failed")
	if err != nil || !strings.Contains(out, "loaded") {
		return fmt.Errorf("pgdog image load on %s: %s", nodeName, out)
	}
	_, _, _ = ssh.Run(conn, `logger -t sdk-ops-pg "pgdog image shipped from the operator (IPv6-only fallback)"`)
	return nil
}

// pgRecreateWanted is the CLUSTER-level recreate: ANY postgres node declaring
// `recreate: true` wipes the whole service on every node (the wipe must be
// cluster-wide — a per-node wipe leaves the other nodes' stale data and the
// rewind/basebackup gets confused).
func pgRecreateWanted(pf ProvisionFile) bool {
	for _, n := range pf.Hosts {
		if cfg, ok := resolveHostConfig(&pf, n).services["postgres"]; ok && cfg.Recreate {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// helpers — IPv6-safe host formatting (URLs need [v6]:port).

// urlHost wraps an IPv6 literal in brackets for URL embedding; v4 passes through.
func urlHost(ip string) string {
	if strings.Contains(ip, ":") && !strings.HasPrefix(ip, "[") {
		return "[" + ip + "]"
	}
	return ip
}

// ---------------------------------------------------------------------------
// etcdRenderData — the per-member render context for templates/etcd.
func etcdRenderData(pf ProvisionFile, h ProvisionHost, prof map[string]any, _ ServiceConfig) (map[string]any, error) {
	nodes := etcdNodes(pf)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("etcd: no hosts declare services: etcd")
	}
	// Build the static bootstrap: etcd0=http://ip:2380,etcd1=...
	pairs := make([]string, 0, len(nodes))
	for i, n := range nodes {
		ip := n.PeerIP
		if ip == "" {
			ip = n.Host
		}
		pairs = append(pairs, fmt.Sprintf("etcd%d=http://%s:2380", i, urlHost(ip)))
	}
	ip := h.PeerIP
	if ip == "" {
		ip = h.Host
	}
	member := 0
	for i, n := range nodes {
		if n.Name == h.Name {
			member = i
		}
	}
	return map[string]any{
		"EtcdName":       fmt.Sprintf("etcd%d", member),
		"EtcdIP":         urlHost(ip),
		"InitialCluster": strings.Join(pairs, ","),
		"MemLimit":       prof["mem_limit"],
		"Cpus":           prof["cpus"],
	}, nil
}

// ---------------------------------------------------------------------------
// pgRenderData — the per-node render context for templates/postgres.
func pgRenderData(pf ProvisionFile, h ProvisionHost, prof map[string]any, cfg ServiceConfig) (map[string]any, error) {
	env := os.Getenv
	nodes := pgNodes(pf)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("postgres: no hosts declare services: postgres")
	}
	mode := cfg.Mode
	if mode == "" {
		mode = "cluster"
	}
	backupMode := cfg.BackupMode
	if backupMode == "" {
		backupMode = "leader"
	}

	// etcd endpoints for the DCS. The LOCAL node's endpoint uses the host's
	// docker bridge (172.17.0.1 — the published port): a container reaching # go-check:ignore-ip (docker bridge RFC1918)
	// the node's OWN public IP is hairpinned and the firewall drops it (no
	// self-peer rule). The remote members use their peer IPs. # go-check:ignore-ip (docker bridge RFC1918)
	localDockerBridge := "172.17.0.1" // go-check:ignore-ip (docker bridge RFC1918)
	etcdEndpoints := make([]string, 0, len(etcdNodes(pf)))
	for _, n := range etcdNodes(pf) {
		ip := n.PeerIP
		if ip == "" {
			ip = n.Host
		}
		if n.Name == h.Name {
			ip = localDockerBridge
		}
		etcdEndpoints = append(etcdEndpoints, fmt.Sprintf("%s:2379", urlHost(ip)))
	}
	ip := h.PeerIP
	if ip == "" {
		ip = h.Host
	}
	// The PgDog targets: all postgres node IPs (the LOCAL node via the host's
	// docker bridge — the hairpin of the own public IP is firewall-blocked).
	poolerIPs := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		nip := n.PeerIP
		if nip == "" {
			nip = n.Host
		}
		if n.Name == h.Name {
			nip = localDockerBridge
		}
		poolerIPs = append(poolerIPs, map[string]any{"IP": nip, "Database": "postgres"})
	}

	return map[string]any{
		"Scope":             "postgres",
		"NodeName":          "pg-" + h.Name,
		"NodeIP":            urlHost(ip),
		"EtcdHosts":         strings.Join(etcdEndpoints, ","),
		"MaxConnections":    prof["max_connections"],
		"MemLimit":          prof["mem_limit"],
		"Cpus":              prof["cpus"],
		"IsPooler":          poolerNode(pf, h, cfg),
		"PoolerIPs":         poolerIPs,
		"BackupMode":        backupMode,
		"Mode":              mode,
		"Restore":           cfg.Restore,
		"Stanza":            "main",
		"PGSuperUser":       env("PG_SUPERUSER"),
		"PGAppPassword":     env("PG_APP_PASSWORD"),
		"PGReplicatorPassword": env("PG_REPLICATOR_PASSWORD"),
		"PGRewindPassword":  env("PG_REWIND_PASSWORD"),
		"S3Endpoint":        env("S3_ENDPOINT"),
		"S3Region":          pgS3Region(),
		"S3Bucket":          env("S3_BUCKET"),
		"S3AccessKey":       env("S3_ACCESS_KEY"),
		"S3SecretKey":       env("S3_SECRET_KEY"),
		"S3Prefix":          env("S3_PREFIX"),
		"PGCipherPass":      env("PG_CIPHER_PASS"),
	}, nil
}

// ---------------------------------------------------------------------------
// wireEtcdOn — etcd node wiring (validate timer).
func wireEtcdOn(conn *goss.Client, svcDir, nodeName string) error {
	if err := safeName(nodeName); err != nil {
		return err
	}
	return installTimer(conn, "etcd-validate", "5min", svcDir, "bash validate.sh")
}

// ---------------------------------------------------------------------------
// wirePGOn — postgres node wiring: certs, .env, host setup (pgxbs + pgbackrest
// + symlink), nft bridge fixes, the DR restore flag and the timers.
func wirePGOn(conn *goss.Client, svcDir, nodeName string, cfg ServiceConfig, pf ProvisionFile, h ProvisionHost) error {
	if err := safeName(nodeName); err != nil {
		return err
	}
	// Recreate: wipe the service state (containers + data + DCS keys + networks)
	// before deploying — a clean redeploy, YAML-driven. Runs ONCE on the PRIMARY
	// node (the first postgres node — the wave 1): the wipe is cluster-wide
	// (every node's stale containers/data are removed BEFORE any compose up —
	// stale leaders keep their DCS registrations and race the fresh bootstrap).
	// The replica wires never wipe: the deploy order guarantees the primary
	// deploys first and the replicas clone from it.
	if (cfg.Recreate || pgRecreateWanted(pf)) && pgPrimaryNode(pf).Name == h.Name {
		if err := cleanPGState(conn, pf, h); err != nil {
			return err
		}
	}
	if err := uploadPGCerts(conn, svcDir, nodeName); err != nil {
		return err
	}
	if err := writePGEnv(conn, svcDir); err != nil {
		return err
	}
	if err := setupPGHost(conn); err != nil {
		return err
	}
	if err := makePGConfigsReadable(conn, svcDir); err != nil {
		return err
	}
	if err := writePGLocalConf(conn); err != nil {
		return err
	}
	// DR: restore from S3 only when the flag is set, this node is the PRIMARY
	// candidate (the first postgres node — the replicas clone from the leader)
	// AND the data is missing (idempotent — an operational cluster is never
	// restored). With the per-node pooler, every node is a pooler — the
	// restore must NOT trigger on the replicas.
	if cfg.Restore && pgPrimaryNode(pf).Name == h.Name {
		if err := pgRestoreIfEmpty(conn, svcDir); err != nil {
			return err
		}
	}
	// The pgdog image must exist before the compose up: an IPv6-only provider
	// cannot pull ghcr.io (v4-only) — the fallback ships it from the operator.
	if err := ensurePGDogImage(conn, nodeName); err != nil {
		return err
	}
	return installPGTimers(conn, svcDir, cfg.Backup)
}

// waitServiceUp — post-service readiness: the postgres PRIMARY must fully
// recover before the replicas clone (a basebackup racing the PITR recovery
// grabs the pre-fork timeline — "requested timeline N is not a child of this
// server's history"). Other services: no wait.
func waitServiceUp(conn *goss.Client, name string, pf ProvisionFile, h ProvisionHost) error {
	if name != "postgres" {
		return nil
	}
	if pgPrimaryNode(pf).Name != h.Name {
		return nil
	}
	return waitPGPrimaryReady(conn, h.Name)
}

// waitPGPrimaryReady waits for the primary's PITR recovery to fully complete
// (the Patroni /primary REST answers only when the postgres is read-write),
// then forces a CHECKPOINT. The replicas' basebackup then clones the clean
// new timeline — racing the recovery grabs the pre-fork state and the clone
// fails with "requested timeline N is not a child of this server's history".
func waitPGPrimaryReady(conn *goss.Client, nodeName string) error {
	cont := "pg-pg-" + nodeName + "-patroni-1"
	script := fmt.Sprintf(`set -e
# The Patroni needs the etcd QUORUM: a single etcd member blocks the
# linearizable reads and the patroni waits forever. The next waves' etcds
# complete the 2/3 — wait for it (up to 10 min).
for i in $(seq 1 120); do
  if curl -fsS --max-time 3 "http://127.0.0.1:2379/v2/machines" >/dev/null 2>&1 \
     && [ "$(curl -s --max-time 3 'http://127.0.0.1:2379/v2/machines' 2>/dev/null | grep -oE 'http://[^,]+' | wc -l | tr -d ' ')" -ge 2 ]; then
    echo "etcd quorum ready"
    break
  fi
  echo "waiting for the etcd quorum ($i/120)..."
  sleep 5
done
for i in $(seq 1 60); do
  if curl -fsS --max-time 3 http://127.0.0.1:8008/primary >/dev/null 2>&1; then
    set -a; . /opt/sdk-ops/services/postgres/.env 2>/dev/null || true; set +a
    PGPASSWORD="$PG_SUPERUSER" psql -U postgres -h 127.0.0.1 -c 'CHECKPOINT' >/dev/null 2>&1 || true
    echo "primary ready (checkpointed)"
    exit 0
  fi
  echo "waiting for the primary recovery ($i/60)..."
  sleep 5
done
echo "ERROR: primary %s not read-write after 300s" >&2
exit 1
`, cont)
	_, _, err := ssh.Run(conn, script)
	return err
}

// cleanPGState — the recreate wipe (YAML `recreate: true`): the postgres +
// pgdog containers, the data dirs, the networks and the DCS (etcd v2) keys.
// CLUSTER-WIDE: wipes this node AND every peer postgres node BEFORE any
// compose up — otherwise the stale containers/leaders of the other nodes keep
// their DCS registrations during the bootstrap and the fresh primary races
// them ("bootstrap from leader 'pg-mia-XX'" — the OLD leader).
func cleanPGState(conn *goss.Client, pf ProvisionFile, h ProvisionHost) error {
	if err := safeName(h.Name); err != nil {
		return err
	}
	wipe := func(c *goss.Client) error {
		script := `set -e
logger -t sdk-ops-pg "recreate: wiping the postgres service (containers + data + DCS keys + networks)"
for c in $(docker ps -aq --filter name=pg- 2>/dev/null); do
  docker rm -f "$c" >/dev/null 2>&1 || true
done
docker network rm $(docker network ls -q --filter name=pg- 2>/dev/null) >/dev/null 2>&1 || true
sudo rm -rf /opt/sdk-ops/services/postgres/data /opt/sdk-ops/services/postgres/run
sudo mkdir -p /opt/sdk-ops/services/postgres/run /opt/sdk-ops/services/postgres/data/patroni/data
sudo chown -R 70:70 /opt/sdk-ops/services/postgres/run /opt/sdk-ops/services/postgres/data/patroni
sudo chmod 0700 /opt/sdk-ops/services/postgres/data/patroni/data
# DCS keys (etcd v2 keyspace) — Patroni leadership/state. --max-time: the
# wave-1 etcd has NO quorum yet (single member) — a write would hang forever.
curl -s --max-time 5 -X DELETE 'http://127.0.0.1:2379/v2/keys/service/pg?recursive=true' >/dev/null 2>&1 || true
`
		_, _, err := ssh.Run(c, script)
		return err
	}
	if err := wipe(conn); err != nil {
		return err
	}
	// The wipe covers EVERY fleet host (not just the ones declaring the
	// postgres): a smaller topology (e.g. 2 nodes) must leave the hosts that
	// LEFT the cluster empty — their stale pg containers/data would otherwise
	// survive. Idempotent: a host without pg state is a no-op.
	for _, n := range pf.Hosts {
		if n.Name == h.Name {
			continue
		}
		port := n.Port
		if port == 0 {
			port = 22
		}
		cleanUser := n.User
		if cleanUser == "" {
			cleanUser = "sdkops"
		}
		f := infraFlags{user: cleanUser, key: n.SSHKey, port: port, mode: pf.Mode}
		pc, err := infraConnect(n.Host, &f)
		if err != nil {
			return fmt.Errorf("cleanPGState %s: %w", n.Name, err)
		}
		err = wipe(pc)
		closeConn(pc)
		if err != nil {
			return fmt.Errorf("cleanPGState %s: %w", n.Name, err)
		}
	}
	return nil
}

// makePGConfigsReadable — the Patroni container runs as uid 70; the uploaded
// configs (0600 sdkops by the render) must be readable by the container.
func makePGConfigsReadable(conn *goss.Client, svcDir string) error {
	script := fmt.Sprintf(`set -e
logger -t sdk-ops-pg "wiring: configs readable (chmod 0644 + ssl chown)"
for f in patroni.yml pgdog.toml users.toml pgbackrest.conf post-bootstrap.sh; do
  [ -f %s/$f ] && sudo chmod 0644 %s/$f || true
done
sudo chown -R 70:70 %s/../ssl 2>/dev/null || true
`, svcDir, svcDir, svcDir)
	_, _, err := ssh.Run(conn, script)
	return err
}

func pgS3Region() string {
	if r := os.Getenv("S3_REGION"); r != "" {
		return r
	}
	return "us-east-005"
}

// writePGLocalConf writes the LOCAL pgbackrest configs (the restore + the
// backup): pgbackrest 2.59 must run on the postgres host (the restore.sh +
// backup.sh read them). The backup's role check (leader mode) uses the
// Patroni REST on 8008.
func writePGLocalConf(conn *goss.Client) error {
	env := os.Getenv
	// The BACKUP conf (pgbackrest.conf) uses the /var/lib symlink so the
	// queried data_directory MATCHES (the pgbackrest backup rejects a
	// different pg1-path). The RESTORE conf (.local.conf) uses the REAL path —
	// the restore's chown fails on the symlink ("Operation not permitted").
	backupPath := "/var/lib/postgresql/data"
	restorePath := "/opt/sdk-ops/services/postgres/data/patroni/data"
	base := `[main]
pg1-path=%[1]s
pg1-user=dev

[global]
repo1-type=s3
repo1-s3-endpoint=%[2]s
repo1-s3-region=%[3]s
repo1-s3-bucket=%[4]s
repo1-s3-key=%[5]s
repo1-s3-key-secret=%[6]s
repo1-s3-uri-style=path
repo1-path=/%[7]s
repo1-retention-full=2
repo1-retention-diff=4
repo1-cipher-type=aes-256-cbc
repo1-cipher-pass=%[8]s
repo1-bundle=y
repo1-block=y
buffer-path=/tmp
spool-path=/tmp

[global:archive-get]
compress-level=1
`
	backupConf := fmt.Sprintf(base, backupPath, env("S3_ENDPOINT"), pgS3Region(), env("S3_BUCKET"),
		env("S3_ACCESS_KEY"), env("S3_SECRET_KEY"), env("S3_PREFIX"), env("PG_CIPHER_PASS"))
	restoreConf := fmt.Sprintf(base, restorePath, env("S3_ENDPOINT"), pgS3Region(), env("S3_BUCKET"),
		env("S3_ACCESS_KEY"), env("S3_SECRET_KEY"), env("S3_PREFIX"), env("PG_CIPHER_PASS"))
	script := fmt.Sprintf(`sudo tee /etc/pgbackrest/pgbackrest.local.conf > /dev/null << 'CONFEOF'
%[1]s
CONFEOF
sudo tee /etc/pgbackrest/pgbackrest.conf > /dev/null << 'CONFEOF'
%[2]s
CONFEOF
sudo chown pgxbs:pgxbs /etc/pgbackrest/pgbackrest.local.conf /etc/pgbackrest/pgbackrest.conf && sudo chmod 600 /etc/pgbackrest/pgbackrest.local.conf /etc/pgbackrest/pgbackrest.conf
`, restoreConf, backupConf)
	_, _, err := ssh.Run(conn, script)
	return err
}

// pgRestoreIfEmpty — the idempotent DR: restore from S3 ONLY when the postgres
// data dir has no PG_VERSION (fresh/wiped) AND a backup exists in the repo.
func pgRestoreIfEmpty(conn *goss.Client, svcDir string) error {
	out, _, _ := ssh.Run(conn, "test -f /opt/sdk-ops/services/postgres/data/patroni/data/PG_VERSION && echo present || echo missing")
	if strings.Contains(out, "present") {
		return nil // data exists — never restore over it (idempotent)
	}
	out, _, _ = ssh.Run(conn, "sudo -u pgxbs pgbackrest --config=/etc/pgbackrest/pgbackrest.local.conf --stanza=main info 2>/dev/null | grep -q 'full backup' && echo repo-ok || echo repo-empty")
	if !strings.Contains(out, "repo-ok") {
		return nil // no backup in the repo — nothing to restore (fresh cluster)
	}
	_, _, err := ssh.Run(conn, "cd "+svcDir+" && bash restore.sh --type=immediate")
	return err
}

// uploadPGCerts uploads the CA + the node server cert/key + the client cert
// from the operator's cert store (PG_CERT_DIR) — the PEM content is written on
// the node (the files live on the Mac).
func uploadPGCerts(conn *goss.Client, svcDir, nodeName string) error {
	certDir := os.Getenv("PG_CERT_DIR")
	if certDir == "" {
		return nil // no cert store configured — the compose mounts ../ssl if present
	}
	read := func(p string) string {
		// The path is built from the operator's own PG_CERT_DIR + the node name
		// (validated by safeName) — no user-controlled input reaches it.
		b, err := os.ReadFile(filepath.Clean(p)) //nolint:gosec // operator's cert store + validated node name
		if err != nil {
			return ""
		}
		return string(b)
	}
	ca := read(filepath.Join(certDir, "ca.pem"))
	crt := read(filepath.Join(certDir, "server", nodeName+".pem"))
	key := read(filepath.Join(certDir, "server", nodeName+".key"))
	if ca == "" || crt == "" || key == "" {
		return fmt.Errorf("pg certs missing in %s (ca/server/%s.{pem,key})", certDir, nodeName)
	}
	script := fmt.Sprintf(`set -e
sudo mkdir -p %[1]s/../ssl
sudo tee %[1]s/../ssl/ca.pem > /dev/null << 'CERTEOF'
%[2]s
CERTEOF
sudo tee %[1]s/../ssl/server.crt > /dev/null << 'CERTEOF'
%[3]s
CERTEOF
sudo tee %[1]s/../ssl/server.key > /dev/null << 'CERTEOF'
%[4]s
CERTEOF
sudo chmod 600 %[1]s/../ssl/server.key
sudo chown -R 70:70 %[1]s/../ssl
`, svcDir, ca, crt, key)
	_, _, err := ssh.Run(conn, script)
	return err
}

// writePGEnv writes the node .env consumed by the scripts (backup/restore/validate).
func writePGEnv(conn *goss.Client, svcDir string) error {
	env := os.Getenv
	content := fmt.Sprintf(`PG_STANZA=main
PG_SUPERUSER=%s
PG_APP_PASSWORD=%s
PG_REPLICATOR_PASSWORD=%s
S3_ENDPOINT=%s
S3_BUCKET=%s
S3_PREFIX=%s
S3_ACCESS_KEY=%s
S3_SECRET_KEY=%s
PG_CIPHER_PASS=%s
`, env("PG_SUPERUSER"), env("PG_APP_PASSWORD"), env("PG_REPLICATOR_PASSWORD"),
		env("S3_ENDPOINT"), env("S3_BUCKET"), env("S3_PREFIX"), env("S3_ACCESS_KEY"),
		env("S3_SECRET_KEY"), env("PG_CIPHER_PASS"))
	script := fmt.Sprintf(`sudo tee %s/.env > /dev/null << 'ENVEOF'
%s
ENVEOF
id sdkops >/dev/null 2>&1 && sudo chown sdkops:sdkops %s/.env || true
sudo chmod 600 %s/.env
`, svcDir, content, svcDir, svcDir)
	_, _, err := ssh.Run(conn, script)
	return err
}

// setupPGHost — host-level requirements for the pgbackrest backup/restore:
// the pgxbs user (uid 70 — same uid as the container postgres), pgbackrest
// 2.59 (PGDG — the apt 2.50 does not support postgres 18), the data symlink
// and the nft bridge fixes (the peer drop blocks the containers' outbound).
func setupPGHost(conn *goss.Client) error {
	// The docker IPv6 ULA the bridge containers use (the egress rules). # go-check:ignore-ip (docker ULA v6)
	fd00ULA := "fd00::/8" // go-check:ignore-ip (docker ULA v6)
	script := fmt.Sprintf(`set -e
logger -t sdk-ops-pg "wire: host setup (pgxbs + pgbackrest 2.59 + symlinks + nft bridge fixes)"
# The provider mirror (mirror.<provider>) can hang on any addressing —
# always switch to the official Ubuntu/Debian archives (provider-agnostic:
# any provider) and force the apt over IPv6 ONLY on v4-less hosts (a v4-only
# host has no v6 route — the ForceIPv6 would break the apt entirely).
sudo sed -i -E 's|https?://mirror[^/ ]*/ubuntu|http://archive.ubuntu.com/ubuntu|g' /etc/apt/sources.list.d/*.sources /etc/apt/sources.list 2>/dev/null || true
sudo sed -i -E 's|mirror\+file://[^ ]*debian-security[^ ]*|http://deb.debian.org/debian-security|g' /etc/apt/sources.list.d/*.sources 2>/dev/null || true
sudo sed -i -E 's|mirror\+file://[^ ]*debian[^ ]*|http://deb.debian.org/debian|g' /etc/apt/sources.list.d/*.sources 2>/dev/null || true
# The ForceIPv6 breaks v4-only hosts (the apt would try the unreachable v6):
# remove it ALWAYS, re-add only on v4-less hosts.
sudo rm -f /etc/apt/apt.conf.d/99force-ipv6 2>/dev/null || true
if ! ip -4 addr show | grep -q 'inet '; then
  echo 'Acquire::ForceIPv6 "true";' | sudo tee /etc/apt/apt.conf.d/99force-ipv6 >/dev/null 2>&1 || true
fi
sudo apt-get update >/dev/null 2>&1 || true
# pgxbs user (uid 70 = the container postgres uid — can read the data files);
# the useradd warns/errors when the uid is outside the system range — tolerate it.
id pgxbs >/dev/null 2>&1 || (sudo useradd -u 70 -o -m -d /home/pgxbs -s /bin/bash pgxbs 2>/dev/null || true)
# psql client for the host-side validation/suite (the wrapper alone errors).
sudo apt-get install -y postgresql-client >/dev/null 2>&1 || true
# pgbackrest 2.59 (PGDG — the apt 2.50 does not support postgres 18; the
# host-side restore requires 2.59 — pinned to the exact stable).
if ! pgbackrest version 2>/dev/null | grep -q '^pgBackRest 2\.59'; then
  sudo apt-get install -y postgresql-common >/dev/null 2>&1 || true
  sudo /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y >/dev/null 2>&1 || true
  sudo apt-get install -y pgbackrest >/dev/null 2>&1 || true
fi
# data symlink (the DB reports /var/lib/postgresql/data; the files live in the volume)
sudo mkdir -p /var/lib/postgresql
[ -e /var/lib/postgresql/data ] || sudo ln -sfn /opt/sdk-ops/services/postgres/data/patroni/data /var/lib/postgresql/data
sudo mkdir -p /etc/pgbackrest
# socket symlink (the host-side pgbackrest connects via /var/run/postgresql)
sudo rm -rf /var/run/postgresql
sudo ln -sfn /opt/sdk-ops/services/postgres/run /var/run/postgresql
# pgbackrest spool/buffer must be writable by the pgxbs user (the restore lock).
sudo rm -rf /tmp/pgbackrest && sudo mkdir -p /tmp/pgbackrest && sudo chmod 777 /tmp/pgbackrest
# socket dir for the postgres (the container runs as uid 70)
sudo mkdir -p /opt/sdk-ops/services/postgres/run /opt/sdk-ops/services/postgres/data/patroni/data
sudo chown -R 70:70 /opt/sdk-ops/services/postgres/run /opt/sdk-ops/services/postgres/data/patroni
sudo chmod 0700 /opt/sdk-ops/services/postgres/data/patroni/data
# nft bridge fixes: the peer drop in the forward chain blocks the containers'
# outbound to the PG/etcd/PgDog ports — the containers must reach the peers.
# IPv4 (docker bridge 172.16.0.0/12) + IPv6 (the docker ULA fd00::/8). # go-check:ignore-ip
# The 443 egress lets the containers reach the S3 (pgbackrest archive + backup).
for port in 5432 6432 2379 2380 8008 443; do
  sudo nft list chain inet filter forward 2>/dev/null | grep -q "dport $port" || \
    sudo nft insert rule inet filter forward ip saddr 172.16.0.0/12 tcp dport $port accept 2>/dev/null || true # go-check:ignore-ip (docker bridge RFC1918)
  sudo nft list chain inet filter input 2>/dev/null | grep -q "dport $port" || \
    sudo nft insert rule inet filter input tcp dport $port ip saddr 172.16.0.0/12 accept 2>/dev/null || true # go-check:ignore-ip (docker bridge RFC1918)
  sudo nft list chain inet filter forward 2>/dev/null | grep -q "ip6 saddr %[1]s tcp dport $port" || \
    sudo nft insert rule inet filter forward ip6 saddr %[1]s tcp dport $port accept 2>/dev/null || true # go-check:ignore-ip (docker ULA v6)
  sudo nft list chain inet filter input 2>/dev/null | grep -q "ip6 saddr %[1]s tcp dport $port" || \
    sudo nft insert rule inet filter input ip6 saddr %[1]s tcp dport $port accept 2>/dev/null || true # go-check:ignore-ip (docker ULA v6)
done
`, fd00ULA)
	_, _, err := ssh.Run(conn, script)
	return err
}

// installPGTimers — the validate (5 min) + the YAML-driven backup cadence
// (`backup: { full, diff, incr }` — defaults: full daily at 00:15 + incr hourly).
func installPGTimers(conn *goss.Client, svcDir string, b *BackupSchedule) error {
	full, diff, incr := "daily", "", "hourly"
	if b != nil {
		if b.Full != "" {
			full = b.Full
		}
		if b.Diff != "" {
			diff = b.Diff
		}
		if b.Incr != "" {
			incr = b.Incr
		}
	}
	if err := installTimer(conn, "pgx-validate", "5min", svcDir, "bash validate.sh"); err != nil {
		return err
	}
	if err := installTimer(conn, "pgx-backup-full", full, svcDir, "bash backup.sh full"); err != nil {
		return err
	}
	if diff != "" {
		if err := installTimer(conn, "pgx-backup-diff", diff, svcDir, "bash backup.sh diff"); err != nil {
			return err
		}
	}
	return installTimer(conn, "pgx-backup-incr", incr, svcDir, "bash backup.sh incr")
}

// installTimer writes a systemd oneshot + timer pair for a script.
func installTimer(conn *goss.Client, name, cadence, svcDir, cmd string) error {
	script := fmt.Sprintf(`set -e
sudo tee /etc/systemd/system/%[1]s.service > /dev/null << 'UNIT'
[Unit]
Description=sdk-ops %[1]s
After=docker.service
[Service]
Type=oneshot
User=sdkops
WorkingDirectory=%[3]s
ExecStart=/bin/bash -c 'cd %[3]s && %[4]s'
UNIT
sudo tee /etc/systemd/system/%[1]s.timer > /dev/null << 'UNIT'
[Unit]
Description=%[1]s timer
[Timer]
OnBootSec=2min
%[2]s
Persistent=true
[Install]
WantedBy=timers.target
UNIT
sudo systemctl daemon-reload
sudo systemctl enable --now %[1]s.timer >/dev/null 2>&1
`, name, cadenceTimer(cadence), svcDir, cmd)
	_, _, err := ssh.Run(conn, script)
	return err
}

func cadenceTimer(cadence string) string {
	switch cadence {
	case "5min":
		return "OnUnitActiveSec=5min"
	case "15min":
		return "OnUnitActiveSec=15min"
	case "hourly":
		return "OnUnitActiveSec=1h"
	case "daily":
		return "OnCalendar=*-*-* 00:15:00"
	case "weekly":
		return "OnCalendar=Mon *-*-* 00:15:00"
	default:
		return "OnUnitActiveSec=" + cadence
	}
}
