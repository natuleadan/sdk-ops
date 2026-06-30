package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Template struct {
	Name        string
	Description string
	Files       map[string]string
}

var Templates = map[string]Template{
	"html": {
		Name:        "html",
		Description: "Static HTML site with Nginx",
		Files: map[string]string{
			"docker-compose.yml": htmlCompose,
			"nginx.conf":         htmlNginxConf,
			"index.html":         htmlIndex,
		},
	},
	"node": {
		Name:        "node",
		Description: "Node.js Express app",
		Files: map[string]string{
			"package.json": nodePackageJSON,
			"server.js":    nodeServerJS,
			"Dockerfile":   nodeDockerfile,
		},
	},
	"wordpress": {
		Name:        "wordpress",
		Description: "WordPress with MySQL",
		Files: map[string]string{
			"docker-compose.yml": wpCompose,
			"service.yaml":       wpServiceYAML,
		},
	},
	"go": {
		Name:        "go",
		Description: "Go HTTP server (multi-stage build)",
		Files: map[string]string{
			"Dockerfile": goDockerfile,
			"main.go":    goMainGo,
			"go.mod":     goGoMod,
		},
	},
}

func List() {
	fmt.Println("Available templates:")
	for _, t := range Templates {
		fmt.Printf("  %-12s %s\n", t.Name, t.Description)
	}
}

func Scaffold(name, dir string) error {
	t, ok := Templates[name]
	if !ok {
		names := make([]string, 0, len(Templates))
		for n := range Templates {
			names = append(names, n)
		}
		return fmt.Errorf("unknown template %q (use: %s)", name, strings.Join(names, ", "))
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	for filename, content := range t.Files {
		path := filepath.Join(absDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", filename, err)
		}
		fmt.Printf("  ✓ %s\n", path)
	}
	return nil
}

func InitServiceYAML(dir, appName string) error {
	path := filepath.Join(dir, "service.yaml")
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	content := fmt.Sprintf(`name: %s
registry: ewr.vultrcr.com/nlaregistry
ports:
  - "80:80"
health:
  path: /
  interval: 30
`, appName)
	return os.WriteFile(path, []byte(content), 0644)
}

func ValidateName(name string) error {
	if strings.Contains(name, " ") {
		return fmt.Errorf("name must not contain spaces")
	}
	return nil
}
