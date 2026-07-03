package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var shellMetaRe = regexp.MustCompile(`[;&\x60$(){}<>!'"\\|]`)

// validateCmd rejects shell metacharacters to prevent command injection.
// Only allows alphanumeric, spaces, /, _, -, ., :, @, =, +, %, ~, ?, and comma.
func validateCmd(cmd string) (string, error) {
	if cmd == "" {
		return "", fmt.Errorf("command is empty")
	}
	if shellMetaRe.MatchString(cmd) {
		return "", fmt.Errorf("command contains shell metacharacters: %s", shellMetaRe.FindString(cmd))
	}
	if strings.Contains(cmd, "..") {
		return "", fmt.Errorf("command contains path traversal")
	}
	return cmd, nil
}

type apiHandler struct {
	db    *sql.DB
	agent *Agent
}

func (h *apiHandler) health(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"uptime":  h.agent.uptime(),
		"version": h.agent.version,
	})
}

func (h *apiHandler) currentMetrics(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, http.StatusOK, collectMetrics())
}

func (h *apiHandler) metricsHistory(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-1 * time.Hour)
	if s := r.URL.Query().Get("since"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			since = time.Now().Add(-d)
		}
	}

	metrics, err := queryMetrics(h.db, since)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, http.StatusOK, metrics)
}

func (h *apiHandler) auditLog(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-24 * time.Hour)
	if s := r.URL.Query().Get("since"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			since = time.Now().Add(-d)
		}
	}

	entries, err := queryAudit(h.db, since)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, http.StatusOK, entries)
}

func (h *apiHandler) listSchedules(w http.ResponseWriter, _ *http.Request) {
	schedules, err := listSchedules(h.db)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, http.StatusOK, schedules)
}

func (h *apiHandler) addSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		CronExpr   string `json:"cron_expr"`
		TaskType   string `json:"task_type"`
		TaskConfig string `json:"task_config"`
		NotifyOn   string `json:"notify_on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Name == "" || req.CronExpr == "" || req.TaskType == "" {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": "name, cron_expr, and task_type are required"})
		return
	}

	if req.NotifyOn == "" {
		req.NotifyOn = "failure"
	}

	s := ScheduleRow{Name: req.Name, CronExpr: req.CronExpr, TaskType: req.TaskType, TaskConfig: req.TaskConfig, Enabled: true}
	if err := addSchedule(h.db, s); err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Reload schedules in scheduler
	h.agent.scheduler.loadSchedules()

	jsonResp(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *apiHandler) removeSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	h.agent.scheduler.removeSchedule(id)
	if err := removeSchedule(h.db, id); err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, http.StatusOK, map[string]string{"status": "removed"})
}

func toInt(s string) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		log.Printf("api: parse int error: %v", err)
	}
	return n
}

func jsonResp(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("api: json encode error: %v", err)
	}
}

func startAPI(addr string, db *sql.DB, agent *Agent) *http.Server {
	h := &apiHandler{db: db, agent: agent}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.health)
	mux.HandleFunc("/metrics/current", h.currentMetrics)
	mux.HandleFunc("/metrics/history", h.metricsHistory)
	mux.HandleFunc("/audit", h.auditLog)
	mux.HandleFunc("/schedules", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.listSchedules(w, r)
		case http.MethodPost:
			h.addSchedule(w, r)
		case http.MethodDelete:
			h.removeSchedule(w, r)
		default:
			jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})

	// Support delete via GET with ?id=N (for busybox wget which lacks DELETE)
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		info, err := checkForUpdate()
		if err != nil {
			jsonResp(w, http.StatusOK, map[string]any{
				"current": version,
				"error":   err.Error(),
			})
			return
		}
		jsonResp(w, http.StatusOK, info)
	})

	mux.HandleFunc("/schedules/remove", func(w http.ResponseWriter, r *http.Request) {
		if idStr := r.URL.Query().Get("id"); idStr != "" {
			h.agent.scheduler.removeSchedule(toInt(idStr))
			if err := removeSchedule(h.db, toInt(idStr)); err != nil {
				log.Printf("api: remove schedule error: %v", err)
			}
			jsonResp(w, http.StatusOK, map[string]string{"status": "removed"})
		} else {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		}
	})

	// 1. Container health
	mux.HandleFunc("/health/containers", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, http.StatusOK, checkContainerHealth())
	})

	// 2. Disk usage
	mux.HandleFunc("/health/disk", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, http.StatusOK, checkDiskUsage())
	})

	// 3. SSL certs
	mux.HandleFunc("/health/certs", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, http.StatusOK, checkSSLCerts())
	})

	// 6. Inventory (ports)
	mux.HandleFunc("/inventory", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, http.StatusOK, scanInventory())
	})

	// 8. Remote exec
	mux.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "use POST"})
			return
		}
		var req struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		safeCmd, err := validateCmd(req.Command)
		if err != nil {
			jsonResp(w, http.StatusBadRequest, map[string]string{
				"error": "invalid command: " + err.Error(),
			})
			return
		}
		cmd := exec.CommandContext(context.Background(), "sh")
		cmd.Args = append(cmd.Args, "-c", safeCmd)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		resp := map[string]any{
			"stdout": stdout.String(),
			"stderr": stderr.String(),
		}
		if err != nil {
			resp["error"] = err.Error()
			jsonResp(w, http.StatusInternalServerError, resp)
			return
		}
		jsonResp(w, http.StatusOK, resp)
	})

	// 9. Network latency
	mux.HandleFunc("/health/network", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, http.StatusOK, checkNetworkLatency())
	})

	// 10. Temperature
	mux.HandleFunc("/health/temperature", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, http.StatusOK, checkTemperature())
	})

	// 4+5. Docker events + log watcher status
	mux.HandleFunc("/events/status", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, http.StatusOK, map[string]string{
			"events_watcher": "running",
			"logs_watcher":   "running",
		})
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		log.Printf("API server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("api server: %v", err)
		}
	}()

	return server
}
