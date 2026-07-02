package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type MetricRow struct {
	Timestamp   time.Time
	CPUPercent  float64
	MemoryTotal uint64
	MemoryUsed  uint64
	DiskTotal   uint64
	DiskUsed    uint64
	NetRx       uint64
	NetTx       uint64
}

type AuditRow struct {
	Timestamp  time.Time
	Action     string
	Status     string
	Message    string
	DurationMs int64
}

type ScheduleRow struct {
	ID         int
	Name       string
	CronExpr   string
	TaskType   string
	TaskConfig string
	Enabled    bool
	LastRun    *time.Time
}

func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		cpu_percent REAL,
		memory_total INTEGER,
		memory_used INTEGER,
		disk_total INTEGER,
		disk_used INTEGER,
		net_rx INTEGER,
		net_tx INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_metrics_ts ON metrics(timestamp);

	CREATE TABLE IF NOT EXISTS audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		action TEXT NOT NULL,
		status TEXT NOT NULL,
		message TEXT,
		duration_ms INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_action ON audit(action);

	CREATE TABLE IF NOT EXISTS schedules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		cron_expr TEXT NOT NULL,
		task_type TEXT NOT NULL,
		task_config TEXT DEFAULT '',
		notify_on TEXT DEFAULT 'failure',
		enabled INTEGER DEFAULT 1,
		last_run TEXT,
		next_run TEXT
	);
	`
	_, err := db.ExecContext(context.Background(), schema)
	return err
}

func insertMetric(db *sql.DB, m MetricRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO metrics (timestamp, cpu_percent, memory_total, memory_used, disk_total, disk_used, net_rx, net_tx) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Timestamp.Format(time.RFC3339), m.CPUPercent, m.MemoryTotal, m.MemoryUsed, m.DiskTotal, m.DiskUsed, m.NetRx, m.NetTx)
	return err
}

func queryMetrics(db *sql.DB, since time.Time) ([]MetricRow, error) {
	rows, err := db.QueryContext(context.Background(), `SELECT timestamp, cpu_percent, memory_total, memory_used, disk_total, disk_used, net_rx, net_tx FROM metrics WHERE timestamp >= ? ORDER BY timestamp ASC`, since.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MetricRow
	for rows.Next() {
		var m MetricRow
		var ts string
		if err := rows.Scan(&ts, &m.CPUPercent, &m.MemoryTotal, &m.MemoryUsed, &m.DiskTotal, &m.DiskUsed, &m.NetRx, &m.NetTx); err != nil {
			return nil, err
		}
		m.Timestamp, _ = time.Parse(time.RFC3339, ts)
		results = append(results, m)
	}
	return results, nil
}

func insertAudit(db *sql.DB, a AuditRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO audit (timestamp, action, status, message, duration_ms) VALUES (?, ?, ?, ?, ?)`,
		a.Timestamp.Format(time.RFC3339), a.Action, a.Status, a.Message, a.DurationMs)
	return err
}

func queryAudit(db *sql.DB, since time.Time) ([]AuditRow, error) {
	rows, err := db.QueryContext(context.Background(), `SELECT timestamp, action, status, message, duration_ms FROM audit WHERE timestamp >= ? ORDER BY timestamp DESC LIMIT 500`, since.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AuditRow
	for rows.Next() {
		var a AuditRow
		var ts string
		if err := rows.Scan(&ts, &a.Action, &a.Status, &a.Message, &a.DurationMs); err != nil {
			return nil, err
		}
		a.Timestamp, _ = time.Parse(time.RFC3339, ts)
		results = append(results, a)
	}
	return results, nil
}

func addSchedule(db *sql.DB, s ScheduleRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT OR REPLACE INTO schedules (name, cron_expr, task_type, task_config, notify_on, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
		s.Name, s.CronExpr, s.TaskType, s.TaskConfig, "failure", true)
	return err
}

func listSchedules(db *sql.DB) ([]ScheduleRow, error) {
	rows, err := db.QueryContext(context.Background(), `SELECT id, name, cron_expr, task_type, task_config, enabled, last_run FROM schedules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ScheduleRow
	for rows.Next() {
		var s ScheduleRow
		var lastRun sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.CronExpr, &s.TaskType, &s.TaskConfig, &s.Enabled, &lastRun); err != nil {
			return nil, err
		}
		if lastRun.Valid {
			t, _ := time.Parse(time.RFC3339, lastRun.String)
			s.LastRun = &t
		}
		results = append(results, s)
	}
	return results, nil
}

func removeSchedule(db *sql.DB, id int) error {
	_, err := db.ExecContext(context.Background(), `DELETE FROM schedules WHERE id = ?`, id)
	return err
}

func purgeOldMetrics(db *sql.DB, retention time.Duration) error {
	cutoff := time.Now().Add(-retention)
	_, err := db.ExecContext(context.Background(), `DELETE FROM metrics WHERE timestamp < ?`, cutoff.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("purge metrics: %w", err)
	}

	_, err = db.ExecContext(context.Background(), `DELETE FROM audit WHERE timestamp < ?`, cutoff.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("purge audit: %w", err)
	}
	return nil
}
