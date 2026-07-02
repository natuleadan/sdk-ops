package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

func Run(client *goss.Client, phase string, vars map[string]string) error {
	hookDir := fmt.Sprintf("/opt/sdk-ops/hooks/%s", phase)

	// Check if hook directory exists and has scripts
	out, _, err := ssh.Run(client, fmt.Sprintf(
		`test -d %s && find %s -maxdepth 1 -type f -executable 2>/dev/null | sort || true`, hookDir, hookDir))
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}

	scripts := strings.FieldsSeq(strings.TrimSpace(out))
	for script := range scripts {
		name := filepath.Base(script)
		fmt.Printf("  → Hook [%s] running %s...\n", phase, name)

		// Build env vars
		var envVars strings.Builder
		fmt.Fprintf(&envVars, "SDK_OPS_PHASE=%s", phase)
		for k, v := range vars {
			fmt.Fprintf(&envVars, " SDK_OPS_%s=%s", k, v)
		}

		hookOut, _, err := ssh.Run(client, fmt.Sprintf("sudo -E %s %s", envVars.String(), script))
		if err != nil {
			fmt.Printf("  ✗ Hook %s failed: %v\n", name, err)
			fmt.Printf("    Output: %s\n", strings.TrimSpace(hookOut))
			if strings.HasPrefix(phase, "pre-") {
				return fmt.Errorf("pre-hook %s failed: %w", name, err)
			}
		} else if strings.TrimSpace(hookOut) != "" {
			fmt.Printf("    %s\n", strings.TrimSpace(hookOut))
		}
	}
	return nil
}

func InitHooksDir(client *goss.Client) error {
	cmds := []string{
		"sudo mkdir -p /opt/sdk-ops/hooks/{pre-init,post-init,pre-join,post-join,pre-deploy,post-deploy,pre-remove,post-remove}",
		"sudo chown -R $(whoami) /opt/sdk-ops/hooks 2>/dev/null || true",
	}
	for _, c := range cmds {
		if _, _, err := ssh.Run(client, c); err != nil {
			return fmt.Errorf("init hooks dir: %w", err)
		}
	}
	return nil
}

func CreateHookLocal(name, phase, content string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	hookPath := filepath.Join(home, ".sdk-ops", "hooks", phase, name)
	os.MkdirAll(filepath.Dir(hookPath), 0750)
	if err := os.WriteFile(filepath.Clean(hookPath), []byte(content), 0600); err != nil {
		return fmt.Errorf("create hook: %w", err)
	}
	return nil
}

func InstallHook(client *goss.Client, name, phase string, content []byte) error {
	hookDir := fmt.Sprintf("/opt/sdk-ops/hooks/%s", phase)
	ssh.Run(client, fmt.Sprintf("sudo mkdir -p %s", hookDir))

	tmpFile := fmt.Sprintf("/tmp/sdk-hook-%s", name)
	uploadCmd := fmt.Sprintf("sudo sh -c 'cat > %s' && sudo chmod +x %s && sudo mv %s %s/", tmpFile, tmpFile, tmpFile, hookDir)

	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	go func() {
		defer stdin.Close()
		stdin.Write(content)
	}()
	if out, err := sess.CombinedOutput(uploadCmd); err != nil {
		return fmt.Errorf("upload hook: %w\n%s", err, string(out))
	}
	fmt.Printf("  → Hook %s/%s installed\n", phase, name)
	return nil
}
