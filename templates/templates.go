package templates

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed pg-full-bm kv-full-bm libsql-full-bm
var infraTemplates embed.FS

type Template struct {
	Name        string
	Description string
	Files       map[string]string // for string-based templates
	IsDir       bool              // true for embedded directory templates
	DirName     string            // subdirectory name under templates/
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
	"pg-full-bm": {
		Name:        "pg-full-bm",
		Description: "PostgreSQL 18 + PgDog + SSL + pgbackrest (bare metal)",
		IsDir:       true,
		DirName:     "pg-full-bm",
	},
	"kv-full-bm": {
		Name:        "kv-full-bm",
		Description: "Dragonfly KV cluster + replication + TLS + admin (bare metal)",
		IsDir:       true,
		DirName:     "kv-full-bm",
	},
	"libsql-full-bm": {
		Name:        "libsql-full-bm",
		Description: "libSQL (sqld) primary + replica + HAProxy TLS (bare metal)",
		IsDir:       true,
		DirName:     "libsql-full-bm",
	},
}

func List() {
	fmt.Println("Available templates:")
	for _, t := range Templates {
		mark := " "
		if t.IsDir {
			mark = "i"
		}
		fmt.Printf("  %s %-20s %s\n", mark, t.Name, t.Description)
	}
	fmt.Println("  i = infrastructure template (Docker Compose services)")
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

	if t.IsDir {
		return scaffoldDir(t.DirName, absDir)
	}

	for filename, content := range t.Files {
		path := filepath.Join(absDir, filename)
		d := filepath.Dir(path)
		if err := os.MkdirAll(d, 0750); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
		if content == "" {
			f, err := os.Create(filepath.Clean(path))
			if err != nil {
				return fmt.Errorf("create %s: %w", filename, err)
			}
			if err := f.Close(); err != nil { log.Printf("file close: %v", err) }
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			return fmt.Errorf("write %s: %w", filename, err)
		}
		fmt.Printf("  ✓ %s\n", path)
	}
	return nil
}

func RunTest(name, dir string) error {
	t, ok := Templates[name]
	if !ok {
		return fmt.Errorf("unknown template %q", name)
	}
	if !t.IsDir {
		return fmt.Errorf("template %q has no test (not a directory template)", name)
	}
	testScript := filepath.Join(dir, "test", "test.sh")
	if _, err := os.Stat(testScript); err != nil {
		return fmt.Errorf("test script not found: %s (run deploy init first)", testScript)
	}
	cmd := exec.CommandContext(context.Background(), "/bin/sh", testScript) //nolint:gosec
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

func scaffoldDir(dirName, absDir string) error {
	entries, err := infraTemplates.ReadDir(dirName)
	if err != nil {
		return fmt.Errorf("read template dir: %w", err)
	}
	for _, entry := range entries {
		entryPath := filepath.Join(dirName, entry.Name())
		outPath := filepath.Join(absDir, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(outPath, 0750); err != nil {
				return fmt.Errorf("create dir %s: %w", outPath, err)
			}
			if err := scaffoldDir(entryPath, outPath); err != nil {
				return err
			}
			continue
		}
		data, err := infraTemplates.ReadFile(entryPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		mode := os.FileMode(0600)
		if strings.HasSuffix(entry.Name(), ".sh") {
			mode = 0700
		}
		if err := os.WriteFile(outPath, data, mode); err != nil {
			return fmt.Errorf("write %s: %w", entry.Name(), err)
		}
		fmt.Printf("  ✓ %s\n", outPath)
	}
	return nil
}

func InitServiceYAML(dir, appName string) error {
	path := filepath.Join(dir, "service.yaml")
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	content := fmt.Sprintf(`name: %s
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
