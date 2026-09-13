package k3s

import (
	"strings"
	"testing"
)

func TestHelmEnsureScript(t *testing.T) {
	s := helmEnsureScript("v3.15.4")

	for _, want := range []string{
		`WANT="v3.15.4"`,
		"helm version --short",
		"already installed",
		"amd64",
		"arm64",
		"get.helm.sh/helm-${WANT}-linux-${HARCH}.tar.gz",
		"curl",
		"wget",
		"install -m 0755",
		"/usr/local/bin/helm",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("helmEnsureScript missing %q", want)
		}
	}
	if strings.Contains(s, "unzip") {
		t.Error("helmEnsureScript should not depend on unzip")
	}
}

func TestHelmVersionPinned(t *testing.T) {
	if HelmVersion == "" || !strings.HasPrefix(HelmVersion, "v") {
		t.Fatalf("HelmVersion must be a pinned release tag, got %q", HelmVersion)
	}
}
