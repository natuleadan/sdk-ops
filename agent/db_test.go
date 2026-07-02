package main

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"
)

func TestInitDB(t *testing.T) {
	dbPath := tempDB(t)
	db, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	// Verify tables exist
	var tables []string
	rows, err := db.QueryContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tables = append(tables, name)
	}

	expected := []string{"audit", "metrics", "schedules"}
	for _, e := range expected {
		found := slices.Contains(tables, e)
		if !found {
			t.Errorf("table %q not found in %v", e, tables)
		}
	}
}

func TestInsertAndQueryMetrics(t *testing.T) {
	dbPath := tempDB(t)
	db, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	m := MetricRow{
		Timestamp:   time.Now(),
		CPUPercent:  45.5,
		MemoryTotal: 8000000000,
		MemoryUsed:  4000000000,
		DiskTotal:   100000000000,
		DiskUsed:    50000000000,
		NetRx:       1000,
		NetTx:       500,
	}

	if err := insertMetric(db, m); err != nil {
		t.Fatalf("insertMetric: %v", err)
	}

	// Query with since = 1 hour ago
	metrics, err := queryMetrics(db, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("queryMetrics: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	if metrics[0].CPUPercent != 45.5 {
		t.Errorf("CPUPercent = %f, want 45.5", metrics[0].CPUPercent)
	}
	if metrics[0].MemoryTotal != 8000000000 {
		t.Errorf("MemoryTotal = %d, want 8000000000", metrics[0].MemoryTotal)
	}
}

func TestInsertAndQueryAudit(t *testing.T) {
	dbPath := tempDB(t)
	db, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	a := AuditRow{
		Timestamp:  time.Now(),
		Action:     "test:action",
		Status:     "ok",
		Message:    "test completed",
		DurationMs: 150,
	}

	if err := insertAudit(db, a); err != nil {
		t.Fatalf("insertAudit: %v", err)
	}

	entries, err := queryAudit(db, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("queryAudit: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	if entries[0].Action != "test:action" {
		t.Errorf("Action = %q, want %q", entries[0].Action, "test:action")
	}
	if entries[0].Status != "ok" {
		t.Errorf("Status = %q, want %q", entries[0].Status, "ok")
	}
	if entries[0].DurationMs != 150 {
		t.Errorf("DurationMs = %d, want 150", entries[0].DurationMs)
	}
}

func TestSchedules(t *testing.T) {
	dbPath := tempDB(t)
	db, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	s := ScheduleRow{
		Name:       "test-backup",
		CronExpr:   "0 3 * * *",
		TaskType:   "backup-services",
		TaskConfig: "",
		Enabled:    true,
	}

	if err := addSchedule(db, s); err != nil {
		t.Fatalf("addSchedule: %v", err)
	}

	schedules, err := listSchedules(db)
	if err != nil {
		t.Fatalf("listSchedules: %v", err)
	}

	if len(schedules) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(schedules))
	}

	if schedules[0].Name != "test-backup" {
		t.Errorf("Name = %q, want %q", schedules[0].Name, "test-backup")
	}
	if schedules[0].CronExpr != "0 3 * * *" {
		t.Errorf("CronExpr = %q, want %q", schedules[0].CronExpr, "0 3 * * *")
	}
	if !schedules[0].Enabled {
		t.Error("Enabled should be true")
	}

	// Test remove
	if err := removeSchedule(db, schedules[0].ID); err != nil {
		t.Fatalf("removeSchedule: %v", err)
	}

	schedules, err = listSchedules(db)
	if err != nil {
		t.Fatalf("listSchedules: %v", err)
	}
	if len(schedules) != 0 {
		t.Errorf("expected 0 schedules after remove, got %d", len(schedules))
	}
}

func TestPurgeOldMetrics(t *testing.T) {
	dbPath := tempDB(t)
	db, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	// Insert old metric (35 days ago)
	oldMetric := MetricRow{
		Timestamp:  time.Now().Add(-35 * 24 * time.Hour),
		CPUPercent: 10.0,
	}
	if err := insertMetric(db, oldMetric); err != nil {
		t.Fatalf("insertMetric (old): %v", err)
	}

	// Insert recent metric (1 hour ago)
	recentMetric := MetricRow{
		Timestamp:  time.Now().Add(-1 * time.Hour),
		CPUPercent: 20.0,
	}
	if err := insertMetric(db, recentMetric); err != nil {
		t.Fatalf("insertMetric (recent): %v", err)
	}

	// Insert old audit (35 days ago)
	oldAudit := AuditRow{
		Timestamp: time.Now().Add(-35 * 24 * time.Hour),
		Action:    "old:action",
		Status:    "ok",
	}
	if err := insertAudit(db, oldAudit); err != nil {
		t.Fatalf("insertAudit (old): %v", err)
	}

	// Purge with 30-day retention
	if err := purgeOldMetrics(db, 30*24*time.Hour); err != nil {
		t.Fatalf("purgeOldMetrics: %v", err)
	}

	// Check old metric is gone
	metrics, err := queryMetrics(db, time.Now().Add(-40*24*time.Hour))
	if err != nil {
		t.Fatalf("queryMetrics after purge: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric after purge, got %d", len(metrics))
	}
	if metrics[0].CPUPercent != 20.0 {
		t.Errorf("remaining metric CPUPercent = %f, want 20.0", metrics[0].CPUPercent)
	}

	// Check old audit is gone
	audit, err := queryAudit(db, time.Now().Add(-40*24*time.Hour))
	if err != nil {
		t.Fatalf("queryAudit after purge: %v", err)
	}
	if len(audit) != 0 {
		t.Errorf("expected 0 audit entries after purge, got %d", len(audit))
	}
}

func TestEmptyDB(t *testing.T) {
	dbPath := tempDB(t)
	db, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	// Query empty tables
	metrics, err := queryMetrics(db, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("queryMetrics empty: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(metrics))
	}

	audit, err := queryAudit(db, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("queryAudit empty: %v", err)
	}
	if len(audit) != 0 {
		t.Errorf("expected 0 audit, got %d", len(audit))
	}

	schedules, err := listSchedules(db)
	if err != nil {
		t.Fatalf("listSchedules empty: %v", err)
	}
	if len(schedules) != 0 {
		t.Errorf("expected 0 schedules, got %d", len(schedules))
	}

	// Query with old timestamp (no data)
	metrics, err = queryMetrics(db, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("queryMetrics future: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics with future since, got %d", len(metrics))
	}
}

func TestDoubleInsert(t *testing.T) {
	dbPath := tempDB(t)
	db, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	// Insert same schedule twice - should upsert
	s := ScheduleRow{
		Name:       "dup-test",
		CronExpr:   "0 * * * *",
		TaskType:   "shell",
		TaskConfig: "echo hello",
		Enabled:    true,
	}

	if err := addSchedule(db, s); err != nil {
		t.Fatalf("addSchedule 1: %v", err)
	}

	s.TaskConfig = "echo world"
	if err := addSchedule(db, s); err != nil {
		t.Fatalf("addSchedule 2: %v", err)
	}

	schedules, err := listSchedules(db)
	if err != nil {
		t.Fatalf("listSchedules: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("expected 1 schedule after upsert, got %d", len(schedules))
	}
	if schedules[0].TaskConfig != "echo world" {
		t.Errorf("TaskConfig after upsert = %q, want %q", schedules[0].TaskConfig, "echo world")
	}
}

func tempDB(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "sdk-ops-agent-test-*.db")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}
