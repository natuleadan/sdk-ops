package secrets

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func EncryptFile(path, ageKey string) error {
	cleanPath := filepath.Clean(path)
	cmd := exec.CommandContext(context.Background(), "sops")
	cmd.Args = append(cmd.Args, "--encrypt", "--age", ageKey, "--in-place", cleanPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sops encrypt: %w", err)
	}
	return nil
}

func DecryptFile(path string) ([]byte, error) {
	cleanPath := filepath.Clean(path)
	cmd := exec.CommandContext(context.Background(), "sops")
	cmd.Args = append(cmd.Args, "--decrypt", cleanPath)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sops decrypt: %w", err)
	}
	return out, nil
}

func FileIsEncrypted(path string) bool {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return false
	}
	content := string(data)
	return len(content) > 0 && (content[0] == '{' || len(content) >= 5 && content[:5] == "sops:")
}

func DecryptFileInPlace(path string) error {
	cleanPath := filepath.Clean(path)
	cmd := exec.CommandContext(context.Background(), "sops")
	cmd.Args = append(cmd.Args, "--decrypt", "--in-place", cleanPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sops decrypt in-place: %w", err)
	}
	return nil
}

func CreateSOPSConfig(ageKey string) error {
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "sops")
	if err := os.MkdirAll(filepath.Clean(configDir), 0700); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "age.yaml")
	config := fmt.Sprintf(`creation_rules:
  - age: %s
`, ageKey)

	return os.WriteFile(filepath.Clean(configPath), []byte(config), 0600)
}
