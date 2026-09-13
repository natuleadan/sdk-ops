package templates

import "testing"

// TestAcceptanceTestsShip locks the deploy contract: the acceptance tooling
// (validate.sh in the root, test/ with the integration tests) must reach the
// node, so an operator can validate a service without the repo at hand.
// Template metadata (README/profiles/bench) stays out of the deploy.
func TestAcceptanceTestsShip(t *testing.T) {
	if skipRender["test"] {
		t.Fatal("test/ must ship with the service (remove it from skipRender)")
	}
	for _, name := range []string{"crowdsec-cluster", "nats-cluster", "valkey-cluster", "df-cluster", "pgsql-cnpg"} {
		if _, err := infraTemplates.ReadFile(name + "/test/test.sh"); err != nil {
			t.Errorf("%s/test/test.sh missing from the embedded templates: %v", name, err)
		}
	}
	for _, no := range []string{"README.md", "profiles.yaml"} {
		if !skipRender[no] {
			t.Errorf("%s is metadata and must not be rendered", no)
		}
	}
}
