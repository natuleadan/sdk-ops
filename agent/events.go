package main

import (
	"database/sql"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// 4. Docker event watcher
type DockerEvent struct {
	Type      string `json:"type"`
	Action    string `json:"action"`
	ActorID   string `json:"actor_id"`
	ActorName string `json:"actor_name"`
	Time      string `json:"time"`
}

func watchDockerEvents(db *sql.DB, stop chan struct{}) {
	log.Println("events: docker event watcher started")

	// Use a buffered approach: poll every 30s for recent events
	// instead of --follow (long-lived SSH-like connection is fragile)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var since string
	for {
		select {
		case <-stop:
			log.Println("events: watcher stopped")
			return
		case <-ticker.C:
			cmd := "docker events --since '30s' --format '{{.Type}}|{{.Action}}|{{.Actor.ID}}|{{.Actor.Attributes.name}}' 2>/dev/null"
			if since != "" {
				cmd = fmt.Sprintf("docker events --since '%s' --format '{{.Type}}|{{.Action}}|{{.Actor.ID}}|{{.Actor.Attributes.name}}' 2>/dev/null", since)
			}

			out, err := exec.Command("sh", "-c", cmd).Output()
			if err != nil {
				continue
			}

			now := time.Now().Format(time.RFC3339)
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "|", 4)
				if len(parts) < 3 {
					continue
				}

				evt := DockerEvent{
					Type:    parts[0],
					Action:  parts[1],
					ActorID: parts[2],
					Time:    now,
				}
				if len(parts) >= 4 {
					evt.ActorName = parts[3]
				}

				// Log important events to audit
				isImportant := evt.Action == "die" || evt.Action == "oom" || evt.Action == "destroy" ||
					evt.Action == "kill" || evt.Action == "health_status: unhealthy"

				if isImportant && db != nil {
					msg := fmt.Sprintf("%s %s: %s (%s)", evt.Type, evt.Action, evt.ActorName, evt.ActorID[:12])
					insertAudit(db, AuditRow{
						Timestamp: time.Now(),
						Action:    fmt.Sprintf("docker:%s", evt.Action),
						Status:    "info",
						Message:   msg,
					})
				}
			}
			since = now
		}
	}
}

// 5. Container log watcher (error patterns)
type LogMatch struct {
	Container string `json:"container"`
	Pattern   string `json:"pattern"`
	Line      string `json:"line"`
	Time      string `json:"time"`
}

var logPatterns = []string{"error", "panic", "FATAL", "OOM", "out of memory", "killed", "segmentation fault", "cannot allocate memory"}

func watchContainerLogs(db *sql.DB, stop chan struct{}) {
	log.Println("logs: container log watcher started")

	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	// Track last line count per container to avoid re-scanning
	lastLines := make(map[string]int)

	for {
		select {
		case <-stop:
			log.Println("logs: watcher stopped")
			return
		case <-ticker.C:
			// Get running containers
			out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
			if err != nil {
				continue
			}

			names := strings.Fields(string(out))
			for _, name := range names {
				if name == "sdk-ops-agent" {
					continue
				}

				// Get recent logs
				logOut, err := exec.Command("docker", "logs", "--tail", "50", name).Output()
				if err != nil {
					continue
				}

				lines := strings.Split(string(logOut), "\n")
				currentLines := len(lines)

				// Check if we've already seen these lines
				prevLines, seen := lastLines[name]
				if seen && currentLines <= prevLines {
					continue
				}

				// Check error patterns in new lines
				startIdx := 0
				if seen {
					startIdx = prevLines
				}

				for i := startIdx; i < len(lines); i++ {
					line := strings.TrimSpace(lines[i])
					if line == "" {
						continue
					}
					for _, pattern := range logPatterns {
						if strings.Contains(strings.ToLower(line), strings.ToLower(pattern)) {
							log.Printf("logs: [%s] matched %q: %s", name, pattern, line)
							if db != nil {
								insertAudit(db, AuditRow{
									Timestamp: time.Now(),
									Action:    fmt.Sprintf("log:%s", name),
									Status:    "warning",
									Message:   fmt.Sprintf("matched %q: %s", pattern, line[:min(len(line), 200)]),
								})
							}
							break
						}
					}
				}

				lastLines[name] = currentLines
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


