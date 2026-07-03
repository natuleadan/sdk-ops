package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGenerateCompose_Basic(t *testing.T) {
	result := GenerateCompose("myreg/svc:v1", "mysvc", 8080, false)
	if !strings.Contains(string(result), "image: myreg/svc:v1") {
		t.Error("missing image reference")
	}
	if !strings.Contains(string(result), `"8080:8080"`) {
		t.Error("missing port mapping")
	}
	if strings.Contains(string(result), "postgres") {
		t.Error("unexpected postgres sidecar")
	}
}

func TestGenerateCompose_WithPostgres(t *testing.T) {
	result := GenerateCompose("myreg/svc:v1", "mysvc", 3000, true)
	if !strings.Contains(string(result), "postgres:17-alpine") {
		t.Error("missing postgres sidecar")
	}
	if !strings.Contains(string(result), "pg_isready") {
		t.Error("missing healthcheck")
	}
}

func TestGenerateCompose_DefaultPort(t *testing.T) {
	result := GenerateCompose("myreg/svc:v1", "mysvc", 0, false)
	if !strings.Contains(string(result), `"8080:8080"`) {
		t.Error("expected default port 8080")
	}
}

func TestDefaultRegistry_EnvVars(t *testing.T) {
	os.Setenv("REGISTRY_SERVER", "myreg.example.com")
	os.Setenv("REGISTRY_USER", "testuser")
	os.Setenv("REGISTRY_PASS", "testpass")

	reg := DefaultRegistry()
	if reg.Username != "testuser" {
		t.Errorf("Username = %q, want %q", reg.Username, "testuser")
	}
	if reg.Password != "testpass" {
		t.Errorf("Password = %q, want %q", reg.Password, "testpass")
	}
	if reg.Server != "myreg.example.com" {
		t.Errorf("Server = %q, want %q", reg.Server, "myreg.example.com")
	}
}

func TestDefaultRegistry_CustomServer(t *testing.T) {
	os.Setenv("REGISTRY_SERVER", "custom.example.com")
	defer os.Unsetenv("REGISTRY_SERVER")

	reg := DefaultRegistry()
	if reg.Server != "custom.example.com" {
		t.Errorf("Server = %q, want %q", reg.Server, "custom.example.com")
	}
}

func TestAtoi(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"0", 0},
		{"1", 1},
		{"42", 42},
		{"abc", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := atoi(tt.input)
		if got != tt.want {
			t.Errorf("atoi(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func createTestSourceDir(t *testing.T) string {
	t.Helper()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "service.yaml"), []byte("name: test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, ".env"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	return srcDir
}

func buildTestTar(t *testing.T, srcDir string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	excludeHidden := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			io.Copy(tw, f)
		}
		return nil
	}

	if err := filepath.Walk(srcDir, excludeHidden); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()
	return &buf
}

func verifyTarContent(t *testing.T, buf *bytes.Buffer, expected ...string) {
	t.Helper()
	gr, err := gzip.NewReader(buf)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var files []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, hdr.Name)
	}

	for _, exp := range expected {
		if !slices.Contains(files, exp) {
			t.Errorf("tar missing %s", exp)
		}
	}
	if slices.Contains(files, ".env") {
		t.Error("tar should not include .env")
	}
}

func TestUploadAndDeploy_TarContent(t *testing.T) {
	srcDir := createTestSourceDir(t)
	buf := buildTestTar(t, srcDir)
	verifyTarContent(t, buf, "main.go", "service.yaml")
}
