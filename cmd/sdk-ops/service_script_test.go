package main

import "testing"

// TestServiceScriptPath locks the node-side script resolution: valid names map
// to the shipped acceptance/DR scripts, invalid names can never escape the
// service directory.
func TestServiceScriptPath(t *testing.T) {
	cases := []struct {
		name, kind, want string
	}{
		{"df-cluster", "validate", "/opt/sdk-ops/services/df-cluster/validate.sh"},
		{"pgsql-cnpg", "test", "/opt/sdk-ops/services/pgsql-cnpg/test/test.sh"},
		{"valkey-cluster", "backup", "/opt/sdk-ops/services/valkey-cluster/backup-s3.sh"},
		{"nats-cluster", "restore", "/opt/sdk-ops/services/nats-cluster/restore-s3.sh"},
	}
	for _, c := range cases {
		got, err := serviceScriptPath(c.name, c.kind)
		if err != nil || got != c.want {
			t.Errorf("serviceScriptPath(%q, %q) = %q, %v; want %q", c.name, c.kind, got, err, c.want)
		}
	}
	for _, bad := range []string{"../etc", "a/b", "a b", "", "UPPER"} {
		if _, err := serviceScriptPath(bad, "validate"); err == nil {
			t.Errorf("serviceScriptPath(%q) accepted an invalid name", bad)
		}
	}
	if _, err := serviceScriptPath("df-cluster", "bogus"); err == nil {
		t.Error("an unknown script kind must fail")
	}
}
