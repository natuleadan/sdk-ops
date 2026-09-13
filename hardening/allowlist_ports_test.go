package hardening

import "testing"

// TestValidateAllowlistPort locks the peer-port guard: exposing kubelet, etcd
// or flannel through the allowlist writes a catch-all drop into the `exposed`
// chain that shadows the local accepts (kubectl exec hangs) and the state
// watchdog re-applies it from the registry every 5 minutes.
func TestValidateAllowlistPort(t *testing.T) {
	for _, port := range []int{8472, 2379, 2380, 10250} {
		if err := validateAllowlistPort(port); err == nil {
			t.Errorf("port %d must be rejected (cluster peer port)", port)
		}
	}
	for _, port := range []int{80, 443, 5432, 6379, 8080} {
		if err := validateAllowlistPort(port); err != nil {
			t.Errorf("port %d must be allowed: %v", port, err)
		}
	}
}
