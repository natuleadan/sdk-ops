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
	"nextjs": {
		Name:        "nextjs",
		Description: "Next.js app (standalone output)",
		Files: map[string]string{
			"Dockerfile":       nextjsDockerfile,
			"package.json":     nextjsPackageJSON,
			"next.config.js":   `module.exports = { output: "standalone" }`,
			"pages/index.jsx":  `export default function Home() { return <div><h1>Deployed with SDK Ops</h1></div> }`,
			"pages/api/health.js": `export default function handler(req, res) { res.status(200).json({ status: "healthy" }) }`,
		},
	},
	"python-fastapi": {
		Name:        "python-fastapi",
		Description: "FastAPI async Python app (uvicorn)",
		Files: map[string]string{
			"Dockerfile":      fastapiDockerfile,
			"requirements.txt": fastapiRequirements,
			"main.py":         fastapiMain,
		},
	},
	"django": {
		Name:        "django",
		Description: "Django project (gunicorn + settings)",
		Files: map[string]string{
			"Dockerfile":          djangoDockerfile,
			"requirements.txt":    djangoRequirements,
			"manage.py":           djangoManagePy,
			"project/__init__.py": "",
			"project/settings.py": djangoSettings,
			"project/urls.py":     djangoUrls,
			"project/wsgi.py":     djangoWsgi,
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

	if err := os.MkdirAll(absDir, 0750); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	for filename, content := range t.Files {
		path := filepath.Join(absDir, filename)
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
		if content == "" {
			// Create empty file (e.g., __init__.py)
			f, err := os.Create(filepath.Clean(path))
			if err != nil {
				return fmt.Errorf("create %s: %w", filename, err)
			}
			f.Close()
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
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
# registry: your-registry.example.com
ports:
  - "80:80"
health:
  path: /
  interval: 30
`, appName)
	return os.WriteFile(filepath.Clean(path), []byte(content), 0600)
}

func InitCICD(dir, ciType string) error {
	var content string
	var path string
	switch ciType {
	case "github":
		content = ghDeployYAML
		path = filepath.Join(dir, ".github", "workflows", "deploy.yml")
	case "gitlab":
		content = glDeployYAML
		path = filepath.Join(dir, ".gitlab-ci.yml")
	default:
		return fmt.Errorf("unsupported CI: %s (use: github, gitlab)", ciType)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(path), []byte(content), 0600)
}

func ValidateName(name string) error {
	if strings.Contains(name, " ") {
		return fmt.Errorf("name must not contain spaces")
	}
	return nil
}
