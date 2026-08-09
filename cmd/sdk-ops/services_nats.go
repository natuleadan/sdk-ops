package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	golang_ssh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

var safeNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// safeName validates a node/service name used to build remote paths, so a
// hostile provision.yaml entry can never escape the intended directory.
func safeName(s string) error {
	if s == "" || len(s) > 64 || !safeNameRe.MatchString(s) {
		return fmt.Errorf("invalid name %q (allowed: a-zA-Z0-9._-)", s)
	}
	return nil
}

// wireNATSOn installs the NATS node runtime: certs, secrets (.env + nkeys),
// the NATS CLI, and the validate/backup systemd timers.
func wireNATSOn(conn *golang_ssh.Client, svcDir, nodeName string) error {
	if err := safeName(nodeName); err != nil {
		return err
	}
	if err := uploadNATSCerts(conn, svcDir, nodeName); err != nil {
		return err
	}
	if err := uploadNATSKeys(conn, svcDir); err != nil {
		return err
	}
	if err := writeNATSEnv(conn, svcDir); err != nil {
		return err
	}
	if err := writeS3Cfg(conn); err != nil {
		return err
	}
	if err := installNATSClI(conn, svcDir); err != nil {
		return err
	}
	if err := installNATSTimers(conn, svcDir); err != nil {
		return err
	}
	return nil
}

// uploadNATSCerts uploads the node's server cert + the CA + the client certs
// (app/sys/svc) from the operator's cert store (NATS_CERT_DIR).
func uploadNATSCerts(conn *golang_ssh.Client, svcDir, nodeName string) error {
	certDir := os.Getenv("NATS_CERT_DIR")
	if certDir == "" {
		return fmt.Errorf("NATS_CERT_DIR not set")
	}
	stage, err := os.MkdirTemp("", "sdk-ops-nats-certs-")
	if err != nil {
		return err
	}
	defer removeAll(stage)

	//nolint:gosec // nodeName sanitized by safeName; base dir is the operator's NATS_CERT_DIR; dest is our stage dir
	copies := map[string]string{
		filepath.Join(certDir, "ca.pem"):                  filepath.Join(stage, "ca.pem"),
		filepath.Join(certDir, "server", nodeName+".pem"): filepath.Join(stage, "server.pem"),
		filepath.Join(certDir, "server", nodeName+".key"): filepath.Join(stage, "server.key"),
		filepath.Join(certDir, "client", "app-cert.pem"):  filepath.Join(stage, "app-cert.pem"),
		filepath.Join(certDir, "client", "app-key.pem"):   filepath.Join(stage, "app-key.pem"),
		filepath.Join(certDir, "client", "sys-cert.pem"):  filepath.Join(stage, "sys-cert.pem"),
		filepath.Join(certDir, "client", "sys-key.pem"):   filepath.Join(stage, "sys-key.pem"),
		filepath.Join(certDir, "client", "svc-cert.pem"):  filepath.Join(stage, "svc-cert.pem"),
		filepath.Join(certDir, "client", "svc-key.pem"):   filepath.Join(stage, "svc-key.pem"),
	}
	for src, dst := range copies {
		//nolint:gosec // fixed paths under the operator's own NATS_CERT_DIR
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("cert %s: %w", src, err)
		}
		//nolint:gosec // dst is within our stage dir
		if err := os.WriteFile(dst, data, 0600); err != nil {
			return err
		}
	}
	return uploadDir(conn, stage, filepath.Join(svcDir, "certs"))
}

// uploadNATSKeys uploads the seal sender NKey + the recipient public key to
// the node (the recipient NKey itself never leaves the operator's machine).
func uploadNATSKeys(conn *golang_ssh.Client, svcDir string) error {
	sender := os.Getenv("NATS_SENDER_NK")
	recipientPub := os.Getenv("NATS_RECIPIENT_PUB")
	if sender == "" || recipientPub == "" {
		return fmt.Errorf("NATS_SENDER_NK / NATS_RECIPIENT_PUB not set")
	}
	stage, err := os.MkdirTemp("", "sdk-ops-nats-keys-")
	if err != nil {
		return err
	}
	defer removeAll(stage)
	sData, err := os.ReadFile(sender) //nolint:gosec // operator-controlled env path
	if err != nil {
		return fmt.Errorf("sender key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "backup-sender.nk"), sData, 0600); err != nil { //nolint:gosec // stage is our MkdirTemp dir
		return err
	}
	rData, err := os.ReadFile(recipientPub) //nolint:gosec // operator-controlled env path
	if err != nil {
		return fmt.Errorf("recipient pub: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "backup-recipient.pub"), rData, 0600); err != nil { //nolint:gosec // stage is our MkdirTemp dir
		return err
	}
	return uploadDir(conn, stage, svcDir)
}

// writeNATSEnv writes the node-side secrets used by the backup/restore/validate
// scripts (all values come from the operator environment — never the YAML).
func writeNATSEnv(conn *golang_ssh.Client, svcDir string) error {
	env := os.Getenv
	lines := []string{
		"NATS_URL=tls://127.0.0.1:4222",
		"NATS_USER=app",
		"NATS_PASSWORD=" + env("NATS_APP_PASSWORD"),
		"NATS_SEAL_SENDER_NK=" + svcDir + "/backup-sender.nk",
		"NATS_SEAL_RECIPIENT_PUB=" + svcDir + "/backup-recipient.pub",
		"S3_BUCKET=" + env("S3_BUCKET"),
		"S3_ENDPOINT=" + env("S3_ENDPOINT"),
		"S3_ACCESS_KEY=" + env("S3_ACCESS_KEY"),
		"S3_SECRET_KEY=" + env("S3_SECRET_KEY"),
		"S3_PREFIX=" + orDefault(env("S3_PREFIX"), "nats"),
	}
	return writeRemoteFile(conn, filepath.Join(svcDir, ".env"), strings.Join(lines, "\n")+"\n")
}

// writeS3Cfg writes the sdkops s3cmd config for the operator's S3 (B2).
func writeS3Cfg(conn *golang_ssh.Client) error {
	env := os.Getenv
	endpoint := env("S3_ENDPOINT")
	if endpoint == "" {
		return fmt.Errorf("S3_ENDPOINT not set")
	}
	cfg := fmt.Sprintf(`[default]
access_key = %s
secret_key = %s
host_base = %s
host_bucket = %%(bucket)s.%s
use_https = True
`, env("S3_ACCESS_KEY"), env("S3_SECRET_KEY"), endpoint, endpoint)
	_, _, err := ssh.RunWithStdin(conn, "sudo tee /home/sdkops/.s3cfg >/dev/null && sudo chown sdkops:sdkops /home/sdkops/.s3cfg && sudo chmod 0600 /home/sdkops/.s3cfg", cfg)
	return err
}

// installNATSClI downloads the pinned NATS CLI + installs s3cmd (backups).
func installNATSClI(conn *golang_ssh.Client, svcDir string) error {
	const url = "https://github.com/nats-io/natscli/releases/download/v0.4.0/nats-0.4.0-linux-amd64.zip"
	cmd := fmt.Sprintf(`set -e
sudo apt-get install -y -qq s3cmd >/dev/null 2>&1 || true
sudo curl -sL -o /tmp/nats.zip %q
python3 -m zipfile -e /tmp/nats.zip /tmp/
sudo install -m 0755 /tmp/nats-0.4.0-linux-amd64/nats %s/nats
sudo chown sdkops:sdkops %s/nats
sudo rm -f /tmp/nats.zip`, url, svcDir, svcDir)
	_, _, err := ssh.Run(conn, cmd)
	return err
}

// installNATSTimers installs the validate (5 min) and backup (daily) timers.
func installNATSTimers(conn *golang_ssh.Client, svcDir string) error {
	units := map[string]string{
		"nats-validate.service": fmt.Sprintf(`[Unit]
Description=NATS cluster node validate
[Service]
Type=oneshot
User=sdkops
ExecStart=/bin/bash %s/validate.sh
`, svcDir),
		"nats-validate.timer": `[Unit]
Description=NATS validate timer
[Timer]
OnCalendar=*:0/5
Persistent=true
[Install]
WantedBy=timers.target
`,
		"nats-backup.service": fmt.Sprintf(`[Unit]
Description=NATS cluster node backup (seal -> S3)
[Service]
Type=oneshot
User=sdkops
ExecStart=/bin/bash %s/backup-cron.sh
`, svcDir),
		"nats-backup.timer": `[Unit]
Description=NATS backup timer
[Timer]
OnCalendar=*-*-* 00:15:00
Persistent=true
[Install]
WantedBy=timers.target
`,
	}
	for unit, body := range units {
		if err := writeRemoteFile(conn, "/etc/systemd/system/"+unit, body); err != nil {
			return err
		}
	}
	_, _, err := ssh.Run(conn, "sudo systemctl daemon-reload && sudo systemctl enable --now nats-validate.timer nats-backup.timer")
	return err
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
