package templates

import (
	"strings"
	"testing"
)

// TestHelmBasedTemplatesVerifyOnly locks the helm-centralization contract:
// helm is installed by the fleet provision (k3s hosts) and the templates must
// only verify the binary — never download it. A template regressing to its own
// download would silently defeat the pinned fleet version.
func TestHelmBasedTemplatesVerifyOnly(t *testing.T) {
	for _, name := range []string{"nats-cluster", "etcd-cluster"} {
		data, err := infraTemplates.ReadFile(name + "/init.sh")
		if err != nil {
			t.Fatalf("%s/init.sh: %v", name, err)
		}
		script := string(data)
		if strings.Contains(script, "get.helm.sh") {
			t.Errorf("%s/init.sh still downloads helm (get.helm.sh) — the provision owns the install", name)
		}
		if !strings.Contains(script, "command -v helm") {
			t.Errorf("%s/init.sh must verify helm with `command -v helm`", name)
		}
		if !strings.Contains(script, "helm version --short") {
			t.Errorf("%s/init.sh must verify the pinned version with `helm version --short`", name)
		}
	}
}
