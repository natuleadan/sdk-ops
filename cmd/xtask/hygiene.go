package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func runHygiene(root string) error {
	required := []string{"LICENSE", "NOTICE", "ThirdPartyNotices.txt", "README.md", "ARCHITECTURE.md", "AGENTS.md", "VERSION", "CHANGELOG.md"}
	for _, rel := range required {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			return fmt.Errorf("missing required file %s", rel)
		}
	}

	forbidden := []string{
		"/Users/",
		"/home/",
		"Co-authored-by:",
		"BEGIN PRIVATE KEY",
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "vendor" || name == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(raw)
		for _, token := range forbidden {
			if strings.Contains(text, token) {
				if filepath.Base(path) == "hygiene.go" {
					continue
				}
				return fmt.Errorf("forbidden token %q in %s", token, path)
			}
		}
		return nil
	})
}
