package compose

import (
	"os"
	"testing"
)

func tempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "compose-*.yml")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	if content != "" {
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			t.Fatalf("write: %v", err)
		}
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestInit(t *testing.T) {
	path := tempFile(t, "")
	if err := Init(path, "test-app"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(f.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(f.Services))
	}
	if _, ok := f.Services["test-app"]; !ok {
		t.Errorf("service test-app not found")
	}
}

func TestAddService(t *testing.T) {
	path := tempFile(t, "")
	if err := Init(path, "app"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := AddService(path, "db", "postgres:17", 5432); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(f.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(f.Services))
	}
	svc, ok := f.Services["db"]
	if !ok {
		t.Fatal("service db not found")
	}
	if svc.Image != "postgres:17" {
		t.Errorf("Image = %q, want %q", svc.Image, "postgres:17")
	}
	if len(svc.Ports) != 1 || svc.Ports[0] != "5432:5432" {
		t.Errorf("Ports = %v, want [5432:5432]", svc.Ports)
	}
}

func TestRemoveService(t *testing.T) {
	path := tempFile(t, "")
	if err := Init(path, "app"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := RemoveService(path, "app"); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(f.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(f.Services))
	}
}

func TestSetEnv(t *testing.T) {
	path := tempFile(t, "")
	if err := Init(path, "app"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := SetEnv(path, "app", "FOO", "bar"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if f.Services["app"].Environment["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", f.Services["app"].Environment["FOO"], "bar")
	}
}

func TestUnsetEnv(t *testing.T) {
	path := tempFile(t, "")
	if err := Init(path, "app"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	SetEnv(path, "app", "FOO", "bar")
	if err := UnsetEnv(path, "app", "FOO"); err != nil {
		t.Fatalf("UnsetEnv: %v", err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := f.Services["app"].Environment["FOO"]; ok {
		t.Error("FOO should be unset")
	}
}

func TestListServices(t *testing.T) {
	path := tempFile(t, "")
	Init(path, "web")
	AddService(path, "api", "nginx:alpine", 80)
	AddService(path, "db", "postgres:17", 5432)

	services, err := ListServices(path)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 3 {
		t.Errorf("expected 3 services, got %d: %v", len(services), services)
	}
}

func TestValidateEmpty(t *testing.T) {
	path := tempFile(t, "services: {}\n")
	err := Validate(path)
	if err == nil {
		t.Error("expected error for empty services")
	}
}

func TestValidateMissingImage(t *testing.T) {
	path := tempFile(t, "services:\n  web:\n    restart: always\n")
	err := Validate(path)
	if err == nil {
		t.Error("expected error for missing image/build")
	}
}

func TestAddDuplicateService(t *testing.T) {
	path := tempFile(t, "")
	Init(path, "app")
	err := AddService(path, "app", "nginx", 80)
	if err == nil {
		t.Error("expected error for duplicate service")
	}
}

func TestRemoveMissingService(t *testing.T) {
	path := tempFile(t, "")
	Init(path, "app")
	err := RemoveService(path, "nonexistent")
	if err == nil {
		t.Error("expected error for missing service")
	}
}

func TestReadNonExistent(t *testing.T) {
	_, err := Read("/tmp/nonexistent-compose-file.yml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestInitDefaultName(t *testing.T) {
	path := tempFile(t, "")
	if err := Init(path, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := f.Services["app"]; !ok {
		t.Errorf("expected default service name 'app'")
	}
}
