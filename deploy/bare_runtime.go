package deploy

import (
	"fmt"
	"path/filepath"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

func DeployBare(client *goss.Client, name, versionDir string) error {
	// Find the first executable file
	checkCmd := fmt.Sprintf(`ls -1 %s/ | head -5`, versionDir)
	out, _, _ := ssh.Run(client, checkCmd)
	files := strings.Fields(out)

	var binary string
	for _, f := range files {
		modeOut, _, _ := ssh.Run(client, fmt.Sprintf("stat -c '%%A' %s/%s 2>/dev/null || stat -f '%%Sp' %s/%s 2>/dev/null", versionDir, f, versionDir, f))
		if strings.Contains(modeOut, "x") {
			binary = filepath.Join(versionDir, f)
			break
		}
	}

	if binary == "" {
		// No executable found, just return OK (upload-only mode)
		fmt.Printf("  → Bare mode: files uploaded to %s\n", versionDir)
		return nil
	}

	// Create systemd service
	unitName := fmt.Sprintf("sdk-ops-%s", name)
	unitContent := fmt.Sprintf(`[Unit]
Description=sdk-ops %s
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=5
WorkingDirectory=%s

[Install]
WantedBy=multi-user.target
`, name, binary, versionDir)

	installCmd := fmt.Sprintf(`
cat > /etc/systemd/system/%s.service << 'EOF'
%s
EOF
systemctl daemon-reload
systemctl enable --now %s.service 2>/dev/null
systemctl restart %s.service 2>/dev/null
echo "ok"`, unitName, unitContent, unitName, unitName)

	out2, _, err := ssh.Run(client, installCmd)
	if err != nil {
		return fmt.Errorf("systemd install: %w", err)
	}
	if !strings.Contains(out2, "ok") {
		return fmt.Errorf("systemd install failed: %s", strings.TrimSpace(out2))
	}

	fmt.Printf("  → Bare metal: %s running as systemd service\n", name)
	return nil
}
