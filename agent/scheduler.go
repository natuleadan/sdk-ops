package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron   *cron.Cron
	db     *sql.DB
	notify *notifyDispatcher
	entryIDs map[int]cron.EntryID
}

func newScheduler(db *sql.DB, nd *notifyDispatcher) *Scheduler {
	return &Scheduler{
		cron:     cron.New(cron.WithParser(cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor))),
		db:       db,
		notify:   nd,
		entryIDs: make(map[int]cron.EntryID),
	}
}

func (s *Scheduler) start() {
	s.cron.Start()
	s.loadSchedules()
}

func (s *Scheduler) stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

func (s *Scheduler) loadSchedules() {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, name, cron_expr, task_type, task_config, notify_on FROM schedules WHERE enabled = 1`)
	if err != nil {
		log.Printf("scheduler: load schedules: %v", err)
		return
	}
	defer func() { if err := rows.Close(); err != nil { log.Printf("scheduler: rows close error: %v", err) } }()

	for rows.Next() {
		var id int
		var name, cronExpr, taskType, taskConfig, notifyOn string
		if err := rows.Scan(&id, &name, &cronExpr, &taskType, &taskConfig, &notifyOn); err != nil {
			log.Printf("scheduler: scan: %v", err)
			continue
		}

		if _, exists := s.entryIDs[id]; exists {
			continue
		}

		if err := s.addSchedule(id, name, cronExpr, taskType, taskConfig, notifyOn); err != nil { log.Printf("scheduler: add schedule error: %v", err) }
	}
}

func (s *Scheduler) addSchedule(id int, name, cronExpr, taskType, taskConfig, notifyOn string) error {
	entryID, err := s.cron.AddFunc(cronExpr, func() {
		s.executeTask(id, name, taskType, taskConfig, notifyOn)
	})
	if err != nil {
		return fmt.Errorf("add cron: %w", err)
	}
	s.entryIDs[id] = entryID
	log.Printf("scheduler: added task %q (%s): %s", name, cronExpr, taskType)
	return nil
}

func (s *Scheduler) removeSchedule(id int) {
	if eid, ok := s.entryIDs[id]; ok {
		s.cron.Remove(eid)
		delete(s.entryIDs, id)
		log.Printf("scheduler: removed task %d", id)
	}
}

func (s *Scheduler) executeTask(id int, name, taskType, taskConfig, notifyOn string) {
	start := time.Now()
	log.Printf("scheduler: executing %q (%s)", name, taskType)

	var status, message string

	switch taskType {
	case "backup-services":
		status, message = runBackupServices()
	case "backup-database":
		message = runBackupDatabase(taskConfig)
		status = "ok"
	case "docker-cleanup":
		status, message = runDockerCleanup()
	case "shell":
		status, message = runShellCommand(taskConfig)
	default:
		status = "failed"
		message = fmt.Sprintf("unknown task type: %s", taskType)
	}

	duration := time.Since(start).Milliseconds()

	if _, err := 	s.db.ExecContext(context.Background(), `UPDATE schedules SET last_run = ? WHERE id = ?`, time.Now().Format(time.RFC3339), id); err != nil { log.Printf("scheduler: update last_run error: %v", err) }

	auditEntry := AuditRow{
		Timestamp:  time.Now(),
		Action:     fmt.Sprintf("scheduler:%s", taskType),
		Status:     status,
		Message:    fmt.Sprintf("%s: %s", name, message),
		DurationMs: duration,
	}
	if err := insertAudit(s.db, auditEntry); err != nil { log.Printf("scheduler: audit error: %v", err) }

	// Notify on failure or always
	if notifyOn == "always" || (notifyOn == "failure" && status != "ok") || (notifyOn == "success" && status == "ok") {
		title := fmt.Sprintf("[sdk-ops] %s: %s", name, status)
		msg := fmt.Sprintf("Task: %s\nType: %s\nDuration: %dms\n%s", name, taskType, duration, message)
		s.notify.send(title, msg)
	}
}

func runBackupServices() (string, string) {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("/tmp/sdk-ops-backup-%s.tar.gz", timestamp)
	cmd := exec.CommandContext(context.Background(), "tar")
	cmd.Args = append(cmd.Args, "czf", filename, "-C", "/opt/sdk-ops", "services")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "failed", fmt.Sprintf("tar: %s", string(out))
	}
	return "ok", fmt.Sprintf("backup saved to %s", filename)
}

func runBackupDatabase(config string) string {
	return ""
}

func runDockerCleanup() (string, string) {
	cmds := []string{
		"docker system prune -af --volumes 2>/dev/null",
		"docker image prune -af 2>/dev/null",
	}
	var output []string
	for _, c := range cmds {
		cmd := exec.CommandContext(context.Background(), "sh")
		cmd.Args = append(cmd.Args, "-c", c)
		out, _ := cmd.CombinedOutput()
		output = append(output, string(out))
	}
	return "ok", strings.Join(output, "\n")
}

func runShellCommand(cmdStr string) (string, string) {
	cmd := exec.CommandContext(context.Background(), "sh")
	cmd.Args = append(cmd.Args, "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "failed", fmt.Sprintf("exit: %v\n%s", err, string(out))
	}
	return "ok", string(out)
}


