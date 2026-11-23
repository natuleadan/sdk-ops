package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/natuleadan/sdk-ops/internal/modules/math"
)

func TestUnitGreetDefault(t *testing.T) {
	if got := Greet("   "); got != "hello, world" {
		t.Fatalf("expected 'hello, world', got %q", got)
	}
}

func TestUnitGreetNamed(t *testing.T) {
	if got := Greet("Ada"); got != "hello, Ada" {
		t.Fatalf("expected 'hello, Ada', got %q", got)
	}
}

func TestUnitMathModule(t *testing.T) {
	mod := math.NewModule()
	res, err := mod.Controller.HandleAdd(15, 27)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Sum != 42 || res.HistoryCount != 1 {
		t.Fatalf("unexpected math result: %+v", res)
	}
	if _, err := mod.Controller.HandleAdd(2_000_000_000, 1); err == nil {
		t.Fatal("expected out-of-bounds error")
	}
}

func TestIntegrationHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	NewMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("unexpected status: %v", body)
	}
}

func TestIntegrationMathAddEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/math/add?a=15&b=27", nil)
	rec := httptest.NewRecorder()
	NewMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body math.AddResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Sum != 42 || body.HistoryCount != 1 {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestArchitectureNoConsolePrintInCore(t *testing.T) {
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(raw)
		if strings.Contains(text, "fmt.Print") || strings.Contains(text, "panic(") {
			t.Fatalf("forbidden print or panic in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk failed: %v", err)
	}
}

func TestArchitectureModularLayerBoundaries(t *testing.T) {
	mathDir := filepath.Join("..", "modules", "math")
	readLayer := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(mathDir, name))
		if err != nil {
			t.Fatalf("failed reading %s: %v", name, err)
		}
		return string(raw)
	}
	entity := readLayer("entity.go")
	dto := readLayer("dto.go")
	repo := readLayer("repository.go")
	service := readLayer("service.go")
	controller := readLayer("controller.go")

	for _, forbidden := range []string{"Service", "Controller", "Repository"} {
		if strings.Contains(entity, forbidden) || strings.Contains(dto, forbidden) {
			t.Fatalf("entity/dto leaked %s", forbidden)
		}
	}
	if strings.Contains(repo, "Service") || strings.Contains(repo, "Controller") {
		t.Fatal("repository leaked service/controller")
	}
	if strings.Contains(service, "Controller") {
		t.Fatal("service leaked controller")
	}
	if strings.Contains(controller, "Repository") {
		t.Fatal("controller leaked repository")
	}
}
