package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateCmdSafe(t *testing.T) {
	safe := []string{
		"docker ps",
		"systemctl status k3s",
		"df -h /opt",
		"free -m",
		"ls -la /opt/sdk-ops/services",
		"cat /etc/os-release",
		"uptime",
		"/usr/bin/docker compose ps",
		"journalctl -n 50 --no-pager",
		"du -sh /var/log",
	}
	for _, cmd := range safe {
		if err := validateCmd(cmd); err != nil {
			t.Errorf("validateCmd(%q) = %v, want nil", cmd, err)
		}
	}
}

func TestValidateCmdUnsafe(t *testing.T) {
	unsafe := []struct {
		cmd  string
		desc string
	}{
		{"echo hello; rm -rf /", "semicolon injection"},
		{"echo hello | sh", "pipe injection"},
		{"echo hello && whoami", "double-ampersand"},
		{"echo hello || whoami", "double-pipe"},
		{"echo hello $(whoami)", "command substitution"},
		{"`whoami`", "backtick"},
		{"whoami > /tmp/out", "redirect out"},
		{"whoami < /etc/passwd", "redirect in"},
		{"'hello'", "single quotes"},
		{`"hello"`, "double quotes"},
		{"echo $HOME", "variable expansion"},
		{"../etc/passwd", "path traversal"},
	}
	for _, tc := range unsafe {
		if err := validateCmd(tc.cmd); err == nil {
			t.Errorf("validateCmd(%q) = nil, want error for %s", tc.cmd, tc.desc)
		}
	}
}

func TestValidateCmdEmpty(t *testing.T) {
	if err := validateCmd(""); err == nil {
		t.Error("validateCmd empty: expected error")
	}
}

func TestExecRejectsMalicious(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := validateCmd(req.Command); err != nil {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	body := bytes.NewBufferString(`{"command":"echo hello; rm -rf /"}`)
	req := httptest.NewRequest(http.MethodPost, "/exec", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestExecAcceptsSafe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := validateCmd(req.Command); err != nil {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	body := bytes.NewBufferString(`{"command":"docker ps"}`)
	req := httptest.NewRequest(http.MethodPost, "/exec", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}
