package main

import (
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestNewScheduler(t *testing.T) {
	db := testDB(t)
	nd := newNotifyDispatcher(AgentConfig{})

	s := newScheduler(db, nd)
	if s == nil {
		t.Fatal("newScheduler returned nil")
	}

	if s.cron == nil {
		t.Error("cron instance is nil")
	}

	s.stop()
}

func TestSchedulerStartStop(t *testing.T) {
	db := testDB(t)
	nd := newNotifyDispatcher(AgentConfig{})
	s := newScheduler(db, nd)

	// Start should not panic
	s.start()

	// Give it a moment
	time.Sleep(100 * time.Millisecond)

	// Stop should not panic
	s.stop()
}

func TestSchedulerAddAndRemoveSchedule(t *testing.T) {
	db := testDB(t)
	nd := newNotifyDispatcher(AgentConfig{})
	s := newScheduler(db, nd)
	s.start()
	defer s.stop()

	// Add schedule via DB
	row := ScheduleRow{
		Name:       "test-shell",
		CronExpr:   "*/5 * * * *",
		TaskType:   "shell",
		TaskConfig: "echo hello",
		Enabled:    true,
	}
	if err := addSchedule(db, row); err != nil {
		t.Fatalf("addSchedule: %v", err)
	}

	// Load schedules
	s.loadSchedules()

	if len(s.entryIDs) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(s.entryIDs))
	}

	// Remove schedule
	for id := range s.entryIDs {
		s.removeSchedule(id)
	}

	if len(s.entryIDs) != 0 {
		t.Errorf("expected 0 entries after remove, got %d", len(s.entryIDs))
	}
}

func TestSchedulerInvalidCron(t *testing.T) {
	db := testDB(t)
	nd := newNotifyDispatcher(AgentConfig{})
	s := newScheduler(db, nd)

	// Adding invalid cron should not panic
	err := s.addSchedule(1, "bad", "not-a-cron", "shell", "", "failure")
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestRunShellCommand(t *testing.T) {
	status, message := runShellCommand("echo hello")
	if status != "ok" {
		t.Errorf("status = %q, want %q", status, "ok")
	}
	if message == "" {
		t.Error("message should not be empty")
	}
}

func TestRunShellCommandFailure(t *testing.T) {
	status, message := runShellCommand("exit 1")
	if status != "failed" {
		t.Errorf("status = %q, want %q", status, "failed")
	}
	if message == "" {
		t.Error("message should not be empty on failure")
	}
}

func TestRunDockerCleanup(t *testing.T) {
	// Docker cleanup should not panic even without Docker
	status, message := runDockerCleanup()
	// On a system without Docker, it still returns "ok" (commands silently fail)
	if status != "ok" {
		t.Errorf("status = %q, want %q", status, "ok")
	}
	_ = message
}

func TestRunBackupServicesNoDir(t *testing.T) {
	// Without /opt/sdk-ops/services, this should still not panic
	status, message := runBackupServices()
	// tar will fail because /opt/sdk-ops doesn't exist in test env
	_ = status
	_ = message
}

func TestSchedulerConcurrentAccess(t *testing.T) {
	db := testDB(t)
	nd := newNotifyDispatcher(AgentConfig{})
	s := newScheduler(db, nd)
	s.start()
	defer s.stop()

	// Add schedule
	s.addSchedule(1, "test-concurrent", "*/5 * * * *", "shell", "echo ok", "failure")

	// Access entryIDs from multiple goroutines
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			s.loadSchedules()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestScheduleRoundTrip(t *testing.T) {
	db := testDB(t)

	// Insert schedule
	s := ScheduleRow{
		Name:       "backup-test",
		CronExpr:   "0 2 * * *",
		TaskType:   "backup-services",
		TaskConfig: "",
		Enabled:    true,
	}
	if err := addSchedule(db, s); err != nil {
		t.Fatalf("addSchedule: %v", err)
	}

	// Retrieve
	schedules, err := listSchedules(db)
	if err != nil {
		t.Fatalf("listSchedules: %v", err)
	}

	if len(schedules) == 0 {
		t.Fatal("no schedules found")
	}

	got := schedules[0]
	if got.Name != "backup-test" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.CronExpr != "0 2 * * *" {
		t.Errorf("CronExpr = %q", got.CronExpr)
	}
	if got.TaskType != "backup-services" {
		t.Errorf("TaskType = %q", got.TaskType)
	}

	// Delete
	if err := removeSchedule(db, got.ID); err != nil {
		t.Fatalf("removeSchedule: %v", err)
	}

	schedules, err = listSchedules(db)
	if err != nil {
		t.Fatalf("listSchedules after delete: %v", err)
	}
	if len(schedules) != 0 {
		t.Errorf("expected empty after delete, got %d", len(schedules))
	}
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "sdk-ops-agent-test-*.db")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	db, err := initDB(f.Name())
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
