package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	root, err := findRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL root: %v\n", err)
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "validate":
		os.Exit(runValidate(root))
	case "lint":
		exitErr(runLint(root))
	case "typecheck":
		exitErr(runTypecheck(root))
	case "test":
		exitErr(runUnitTests(root))
	case "integration":
		exitErr(runIntegrationTests(root))
	case "cooldown":
		exitErr(runCooldown(root))
	case "hygiene":
		exitErr(runHygiene(root))
	case "notices":
		exitErr(runNotices(root, args))
	case "release":
		exitErr(runRelease(root, args))
	case "commit-check":
		exitErr(runCommitCheck(root, args))
	default:
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println("Usage: go run ./cmd/xtask <validate|lint|typecheck|test|integration|cooldown|hygiene|notices|release|commit-check>")
}

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func runValidate(root string) int {
	type gate struct {
		name string
		fn   func() error
	}
	gates := []gate{
		{"lint", func() error { return runLint(root) }},
		{"typecheck", func() error { return runTypecheck(root) }},
		{"unit tests", func() error { return runUnitTests(root) }},
		{"integration and architecture tests", func() error { return runIntegrationTests(root) }},
		{"cooldown", func() error { return runCooldown(root) }},
		{"hygiene", func() error { return runHygiene(root) }},
		{"notices", func() error { return runNotices(root, []string{"--check"}) }},
		{"release", func() error { return runRelease(root, []string{"--check"}) }},
	}

	passed := 0
	failed := 0
	for _, g := range gates {
		if err := g.fn(); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", g.name, err)
			failed++
		} else {
			fmt.Printf("PASS %s\n", g.name)
			passed++
		}
	}
	fmt.Printf("TOTAL PASS=%d FAIL=%d\n", passed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

func runTypecheck(root string) error {
	return runExec(root, "go", "build", "./...")
}

func runUnitTests(root string) error {
	return runExec(root, "go", "test", "-run", "TestUnit", "./internal/...")
}

func runIntegrationTests(root string) error {
	return runExec(root, "go", "test", "-run", "TestIntegration|TestArchitecture", "./internal/...")
}

func runCooldown(root string) error {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	text := string(raw)
	if strings.Contains(text, "replace ") {
		return fmt.Errorf("local replace directives are forbidden in go.mod")
	}
	if strings.Contains(text, "@latest") {
		return fmt.Errorf("floating @latest dependency is forbidden")
	}
	dependabot, err := os.ReadFile(filepath.Join(root, ".github", "dependabot.yml"))
	if err != nil {
		return err
	}
	if !strings.Contains(string(dependabot), "default-days: 3") {
		return fmt.Errorf("dependabot.yml must enforce 3-day cooldown")
	}
	return nil
}

func runExec(root string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func exitErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("PASS")
}
