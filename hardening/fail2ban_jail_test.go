package hardening

import (
	"strings"
	"testing"
)

func TestFail2banJailScriptDefaults(t *testing.T) {
	out := Fail2banJailScript(Fail2banJailConfig{})
	for _, want := range []string{
		"bantime = 3600",
		"bantime = 82800",
		"maxretry = 5",
		"ignoreip = 127.0.0.1/8 ::1",
		"[recidive]",
		"banaction = %(banaction_allports)s",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("jail.local missing %q\n%s", want, out)
		}
	}
}

func TestFail2banJailScriptCustom(t *testing.T) {
	out := Fail2banJailScript(Fail2banJailConfig{
		SSHBantime:      7200,
		RecidiveBantime: 90000,
		MaxRetry:        3,
		IgnoreIPs:       []string{"203.0.113.5/32", "2001:db8::1/128"},
	})
	for _, want := range []string{
		"bantime = 7200",
		"bantime = 90000",
		"maxretry = 3",
		"ignoreip = 127.0.0.1/8 ::1 203.0.113.5/32 2001:db8::1/128",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("jail.local missing %q\n%s", want, out)
		}
	}
}
