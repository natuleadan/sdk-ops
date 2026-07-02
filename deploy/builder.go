package deploy

import (
	"fmt"
	"os"
	"path/filepath"
)

type BuilderType string

const (
	BuilderDockerfile BuilderType = "dockerfile"
	BuilderNixpacks   BuilderType = "nixpacks"
	BuilderPack       BuilderType = "pack"
)

type Builder interface {
	Type() BuilderType
	Detect(dir string) bool
	Build(dir, name string, reg RegistryConfig) (string, error)
}

func NewBuilder(b BuilderType) Builder {
	switch b {
	case BuilderNixpacks:
		return &NixpacksBuilder{}
	case BuilderPack:
		return &PackBuilder{}
	default:
		return &DockerfileBuilder{}
	}
}

func DetectBuilder(dir string) BuilderType {
	// Skip builder if docker-compose.yml exists (no image to build)
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err == nil {
		return ""
	}

	for _, b := range []Builder{&NixpacksBuilder{}, &PackBuilder{}, &DockerfileBuilder{}} {
		if b.Detect(dir) {
			return b.Type()
		}
	}
	return BuilderDockerfile
}


func BuildImage(dir, name string, reg RegistryConfig, builder BuilderType) (string, error) {
	b := NewBuilder(builder)

	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("directory %s not found", dir)
	}

	imageRef, err := b.Build(dir, name, reg)
	if err != nil {
		return "", fmt.Errorf("%s build: %w", builder, err)
	}

	return imageRef, nil
}


