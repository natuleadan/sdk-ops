package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// skipRender lists files that are template metadata, not runtime artifacts.
// The rendered output is what gets deployed to a node, so bench/readme/test
// material is excluded.
var skipRender = map[string]bool{
	"profiles.yaml": true,
	"README.md":     true,
	"bench.md":      true,
	"rps.sh":        true,
	"test":          true,
}

// LoadProfiles parses a directory template's profiles.yaml
// (map of profile name -> variables). Profiles let one scalable template
// serve nodes of different sizes (e.g. lite vs rs) without copying configs.
func LoadProfiles(dirName string) (map[string]map[string]any, error) {
	data, err := infraTemplates.ReadFile(filepath.Join(dirName, "profiles.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read %s/profiles.yaml: %w", dirName, err)
	}
	var profiles map[string]map[string]any
	if err := yaml.Unmarshal(data, &profiles); err != nil {
		return nil, fmt.Errorf("parse %s/profiles.yaml: %w", dirName, err)
	}
	return profiles, nil
}

// RenderDir renders an embedded directory template into outDir applying
// text/template with the given data context. Files without template markers
// are copied verbatim. Missing keys render as empty values (missingkey=zero).
func RenderDir(dirName, outDir string, data any) error {
	if err := os.MkdirAll(filepath.Clean(outDir), 0o750); err != nil {
		return fmt.Errorf("create render dir: %w", err)
	}
	return renderWalk(dirName, outDir, data)
}

func renderWalk(src, dst string, data any) error {
	entries, err := infraTemplates.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	for _, e := range entries {
		if skipRender[e.Name()] {
			continue
		}
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(dstPath, 0o750); err != nil {
				return fmt.Errorf("create dir %s: %w", dstPath, err)
			}
			if err := renderWalk(srcPath, dstPath, data); err != nil {
				return err
			}
			continue
		}
		content, err := infraTemplates.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", srcPath, err)
		}
		var rendered []byte
		if strings.Contains(string(content), "{{") {
			t, err := template.New(e.Name()).Option("missingkey=zero").Parse(string(content))
			if err != nil {
				return fmt.Errorf("parse template %s: %w", e.Name(), err)
			}
			var buf strings.Builder
			if err := t.Execute(&buf, data); err != nil {
				return fmt.Errorf("render %s: %w", e.Name(), err)
			}
			rendered = []byte(buf.String())
		} else {
			rendered = content
		}
		mode := os.FileMode(0o600)
		if strings.HasSuffix(e.Name(), ".sh") {
			mode = 0o700
		}
		if err := os.WriteFile(dstPath, rendered, mode); err != nil {
			return fmt.Errorf("write %s: %w", dstPath, err)
		}
	}
	return nil
}
