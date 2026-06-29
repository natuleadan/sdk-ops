package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func EncryptFile(path, ageKey string) error {
	cmd := exec.Command("sops", "--encrypt",
		"--age", ageKey,
		"--in-place", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sops encrypt: %w", err)
	}
	return nil
}

func DecryptFile(path string) ([]byte, error) {
	cmd := exec.Command("sops", "--decrypt", path)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sops decrypt: %w", err)
	}
	return out, nil
}

func FileIsEncrypted(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	// SOPS-encrypted files start with a `sops` block in YAML
	content := string(data)
	return len(content) > 0 && (content[0] == '{' || len(content) >= 5 && content[:5] == "sops:")
}

func DecryptFileInPlace(path string) error {
	cmd := exec.Command("sops", "--decrypt", "--in-place", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sops decrypt in-place: %w", err)
	}
	return nil
}

func CreateSOPSConfig(ageKey string) error {
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "sops")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "age.yaml")
	config := fmt.Sprintf(`creation_rules:
  - age: %s
`, ageKey)

	return os.WriteFile(configPath, []byte(config), 0600)
}
