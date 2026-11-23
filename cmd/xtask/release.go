package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func runRelease(root string, args []string) error {
	if len(args) >= 2 && args[0] == "--apply" {
		nextVer := strings.TrimSpace(args[1])
		if !semverRe.MatchString(nextVer) {
			return fmt.Errorf("invalid semver %q", nextVer)
		}
		if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte(nextVer+"\n"), 0o644); err != nil {
			return err
		}
		corePath := filepath.Join(root, "internal", "core", "core.go")
		coreRaw, err := os.ReadFile(corePath)
		if err != nil {
			return err
		}
		re := regexp.MustCompile(`const Version = "[^"]+"`)
		updatedCore := re.ReplaceAllString(string(coreRaw), fmt.Sprintf(`const Version = %q`, nextVer))
		if err := os.WriteFile(corePath, []byte(updatedCore), 0o644); err != nil {
			return err
		}
		changelogPath := filepath.Join(root, "CHANGELOG.md")
		changelogRaw, err := os.ReadFile(changelogPath)
		if err != nil {
			return err
		}
		marker := "# Changelog"
		heading := fmt.Sprintf("## %s", nextVer)
		if !strings.Contains(string(changelogRaw), heading) {
			updated := strings.Replace(string(changelogRaw), marker, marker+"\n\n"+heading+"\n\n- Automated release preparation.", 1)
			if err := os.WriteFile(changelogPath, []byte(updated), 0o644); err != nil {
				return err
			}
		}
		return nil
	}

	verBytes, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return err
	}
	ver := strings.TrimSpace(string(verBytes))
	if !semverRe.MatchString(ver) {
		return fmt.Errorf("invalid VERSION %q", ver)
	}
	changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		return err
	}
	if !strings.Contains(string(changelog), fmt.Sprintf("## %s", ver)) {
		return fmt.Errorf("CHANGELOG.md missing section for %s", ver)
	}
	return nil
}
