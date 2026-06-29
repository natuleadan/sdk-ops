package deploy

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

func BackupServices(client *goss.Client, destDir string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("sdk-ops-backup-%s.tar.gz", timestamp)
	remotePath := fmt.Sprintf("/tmp/%s", filename)

	// Check if services exist first
	checkOut, _, _ := ssh.Run(client, `ls -A /opt/sdk-ops/services 2>/dev/null | head -1 || echo "empty"`)
	if strings.TrimSpace(checkOut) == "empty" {
		return "", fmt.Errorf("no services to backup")
	}

	// Create backup
	script := fmt.Sprintf(`tar czf %s -C /opt/sdk-ops services/ 2>/dev/null && echo "ok" || echo "fail"`, remotePath)
	out, _, err := ssh.Run(client, script)
	if err != nil || strings.TrimSpace(out) != "ok" {
		return "", fmt.Errorf("backup create failed: %s", strings.TrimSpace(out))
	}

	// Download via SSH using Run (not session.Output) for better error handling
	catCmd := fmt.Sprintf("cat %s", remotePath)
	localPath := fmt.Sprintf("%s/%s", destDir, filename)

	outBytes, _, err := ssh.Run(client, catCmd)
	if err != nil {
		return "", fmt.Errorf("download backup: %w", err)
	}

	if err := os.WriteFile(localPath, []byte(outBytes), 0644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}

	// Cleanup remote
	ssh.Run(client, fmt.Sprintf("rm -f %s", remotePath))

	fmt.Printf("  → Backup saved: %s (%d bytes)\n", localPath, len(outBytes))
	return localPath, nil
}

func RestoreServices(client *goss.Client, backupPath string) error {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer session.Close()

	restoreCmd := `sudo tar xzf - -C / && echo "restore_ok"`
	session.Stdin = bytes.NewReader(data)
	out, err := session.CombinedOutput(restoreCmd)
	if err != nil {
		return fmt.Errorf("restore failed: %w\n%s", err, string(out))
	}

	// Restart services
	ssh.Run(client, `for d in /opt/sdk-ops/services/*/; do
		[ -d "$d/current" ] && (cd "$d/current" && docker compose up -d 2>/dev/null || true)
	done`)

	fmt.Printf("  → Services restored from %s\n", backupPath)
	return nil
}
