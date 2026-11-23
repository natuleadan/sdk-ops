package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type goModule struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
	Main    bool   `json:"Main"`
}

func runNotices(root string, args []string) error {
	checkOnly := len(args) > 0 && args[0] == "--check"
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	var deps []string
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var m goModule
		if err := dec.Decode(&m); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if !m.Main {
			deps = append(deps, fmt.Sprintf("- %s (%s)", m.Path, m.Version))
		}
	}
	sort.Strings(deps)

	var b strings.Builder
	b.WriteString("# Third-Party Notices\n\n")
	if len(deps) == 0 {
		b.WriteString("No external Go module dependencies (standard library only).\n")
	} else {
		for _, dep := range deps {
			b.WriteString(dep)
			b.WriteString("\n")
		}
	}

	target := filepath.Join(root, "ThirdPartyNotices.txt")
	expected := b.String()
	if checkOnly {
		actual, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		if string(actual) != expected {
			return fmt.Errorf("ThirdPartyNotices.txt is out of date; run go run ./cmd/xtask notices")
		}
		return nil
	}
	return os.WriteFile(target, []byte(expected), 0o644)
}
