package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllTemplates(t *testing.T) {
	for name, tmpl := range Templates {
		t.Run(name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "tmpl-"+name)
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)

			if err := Scaffold(name, dir); err != nil {
				t.Fatalf("Scaffold(%q) failed: %v", name, err)
			}

			if tmpl.IsDir {
				testDir := filepath.Join(dir, "test")
				testScript := filepath.Join(testDir, "test.sh")

				// Template must have test/test.sh for integration tests
				if _, err := os.Stat(testScript); err == nil {
					// Check executable
					info, _ := os.Stat(testScript)
					if info.Mode()&0111 == 0 {
						t.Errorf("test/test.sh not executable")
					}
					// Test runner will execute this separately
					t.Logf("integration test found: %s/test/test.sh", name)
				}
			}
		})
	}
}

func TestScaffoldCommon(t *testing.T) {
	for name := range Templates {
		t.Run(name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "common-"+name)
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)

			if err := Scaffold(name, dir); err != nil {
				t.Fatalf("Scaffold(%q): %v", name, err)
			}

			filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return nil
				}
				rel := strings.TrimPrefix(path, dir+"/")

				// Check not empty (allow __init__.py)
				if info.Size() == 0 && !strings.HasSuffix(rel, "__init__.py") {
					t.Errorf("empty file: %s", rel)
				}

				// Scripts must be executable
				if strings.HasSuffix(rel, ".sh") && info.Mode()&0111 == 0 {
					t.Errorf("script not executable: %s (mode: %o)", rel, info.Mode())
				}

				return nil
			})
		})
	}
}
