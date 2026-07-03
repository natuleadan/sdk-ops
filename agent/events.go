package main

import (
	"context"
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

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var since string
	for {
		select {
		case <-stop:
			log.Println("events: watcher stopped")
			return
		case <-ticker.C:
			now := time.Now().Format(time.RFC3339)
			lines, err := handleDockerEvents(since)
			if err != nil {
				handleDockerError(err)
				continue
			}
			for _, line := range lines {
				handleEventLine(db, line, now)
			}
			since = now
		}
	}
}

func handleDockerEvents(since string) ([]string, error) {
	eventSince := "30s"
	if since != "" {
		eventSince = since
	}
	args := []string{"events", "--since", eventSince, "--format", "{{.Type}}|{{.Action}}|{{.Actor.ID}}|{{.Actor.Attributes.name}}"}
	out, err := exec.CommandContext(context.Background(), "docker", args...).Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines, nil
}

func handleEventLine(db *sql.DB, line, now string) {
	if line == "" {
		return
	}
	parts := strings.SplitN(line, "|", 4)
	if len(parts) < 3 {
		return
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

	isImportant := evt.Action == "die" || evt.Action == "oom" || evt.Action == "destroy" ||
		evt.Action == "kill" || evt.Action == "health_status: unhealthy"

	if isImportant && db != nil {
		msg := fmt.Sprintf("%s %s: %s (%s)", evt.Type, evt.Action, evt.ActorName, evt.ActorID[:12])
		if err := insertAudit(db, AuditRow{
			Timestamp: time.Now(),
			Action:    fmt.Sprintf("docker:%s", evt.Action),
			Status:    "info",
			Message:   msg,
		}); err != nil {
			log.Printf("events: insert audit error: %v", err)
		}
	}
}

func handleDockerError(err error) {
	log.Printf("events: docker error: %v", err)
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

	lastLines := make(map[string]int)

	for {
		select {
		case <-stop:
			log.Println("logs: watcher stopped")
			return
		case <-ticker.C:
			processContainerLogs(db, lastLines)
		}
	}
}

func processContainerLogs(db *sql.DB, lastLines map[string]int) {
	out, err := exec.CommandContext(context.Background(), "docker", "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		return
	}

	names := strings.FieldsSeq(string(out))
	for name := range names {
		if name == "sdk-ops-agent" {
			continue
		}

		if _, ok := validContainerName(name); !ok {
			continue
		}

		logCmd := exec.CommandContext(context.Background(), "docker")
		logCmd.Args = append(logCmd.Args, "logs", "--tail", "50", name)
		logOut, err := logCmd.Output()
		if err != nil {
			continue
		}

		lines := strings.Split(string(logOut), "\n")
		currentLines := len(lines)

		prevLines, seen := lastLines[name]
		if seen && currentLines <= prevLines {
			continue
		}

		startIdx := 0
		if seen {
			startIdx = prevLines
		}

		checkContainerLogPatterns(db, name, lines, startIdx)
		lastLines[name] = currentLines
	}
}

func checkContainerLogPatterns(db *sql.DB, name string, lines []string, startIdx int) {
	for i := startIdx; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		for _, pattern := range logPatterns {
			if strings.Contains(strings.ToLower(line), strings.ToLower(pattern)) {
				log.Printf("logs: [%s] matched %q: %s", name, pattern, line)
				if db != nil {
					if err := insertAudit(db, AuditRow{
						Timestamp: time.Now(),
						Action:    fmt.Sprintf("log:%s", name),
						Status:    "warning",
						Message:   fmt.Sprintf("matched %q: %s", pattern, line[:min(len(line), 200)]),
					}); err != nil {
						log.Printf("events: insert audit error: %v", err)
					}
				}
				break
			}
		}
	}
}

func validContainerName(name string) (string, bool) {
	if name == "" || strings.ContainsAny(name, "/;|&`$(){}<>!") {
		return "", false
	}
	return name, true
}
