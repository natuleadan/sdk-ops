package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type Service struct {
	Image       string            `yaml:"image,omitempty" json:"image,omitempty"`
	Build       string            `yaml:"build,omitempty" json:"build,omitempty"`
	Ports       []string          `yaml:"ports,omitempty" json:"ports,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	DependsOn   []string          `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Restart     string            `yaml:"restart,omitempty" json:"restart,omitempty"`
}

type File struct {
	Services map[string]*Service `yaml:"services" json:"services"`
	Volumes  map[string]any      `yaml:"volumes,omitempty" json:"volumes,omitempty"`
}

func Init(path, name string) error {
	if name == "" {
		name = "app"
	}
	f := &File{
		Services: map[string]*Service{
			name: {
				Image:   "nginx:alpine",
				Ports:   []string{"8080:80"},
				Restart: "unless-stopped",
			},
		},
	}
	return Write(path, f)
}

func Read(path string) (*File, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Services == nil {
		f.Services = make(map[string]*Service)
	}
	return &f, nil
}

func Write(path string, f *File) error {
	data, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(filepath.Clean(path), data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func AddService(filePath, name, image string, port int) error {
	f, err := Read(filePath)
	if err != nil {
		return err
	}
	if _, ok := f.Services[name]; ok {
		return fmt.Errorf("service %q already exists", name)
	}
	svc := &Service{Image: image, Restart: "unless-stopped"}
	if port > 0 {
		svc.Ports = []string{fmt.Sprintf("%d:%d", port, port)}
	}
	f.Services[name] = svc
	return Write(filePath, f)
}

func RemoveService(filePath, name string) error {
	f, err := Read(filePath)
	if err != nil {
		return err
	}
	if _, ok := f.Services[name]; !ok {
		return fmt.Errorf("service %q not found", name)
	}
	delete(f.Services, name)
	return Write(filePath, f)
}

func SetEnv(filePath, service, key, value string) error {
	f, err := Read(filePath)
	if err != nil {
		return err
	}
	svc, ok := f.Services[service]
	if !ok {
		return fmt.Errorf("service %q not found", service)
	}
	if svc.Environment == nil {
		svc.Environment = make(map[string]string)
	}
	svc.Environment[key] = value
	return Write(filePath, f)
}

func UnsetEnv(filePath, service, key string) error {
	f, err := Read(filePath)
	if err != nil {
		return err
	}
	svc, ok := f.Services[service]
	if !ok {
		return fmt.Errorf("service %q not found", service)
	}
	delete(svc.Environment, key)
	return Write(filePath, f)
}

func ListServices(filePath string) ([]string, error) {
	f, err := Read(filePath)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(f.Services))
	for n := range f.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func Validate(filePath string) error {
	data, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	if len(f.Services) == 0 {
		return fmt.Errorf("no services defined")
	}
	for name, svc := range f.Services {
		if svc.Image == "" && svc.Build == "" {
			return fmt.Errorf("service %q: must have image or build", name)
		}
	}
	return nil
}
