package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "dev"

type Agent struct {
	version   string
	startTime time.Time
	config    AgentConfig
	db        *sql.DB
	scheduler *Scheduler
	notifier  *notifyDispatcher
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("sdk-ops-agent v%s starting", version)

	cfg := loadConfig()
	db, err := initDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("db init: %v", err)
	}
	defer db.Close()

	nd := newNotifyDispatcher(cfg)

	sched := newScheduler(db, nd)

	agent := &Agent{
		version:   version,
		startTime: time.Now(),
		config:    cfg,
		db:        db,
		scheduler: sched,
		notifier:  nd,
	}

	// Start scheduler
	sched.start()
	log.Println("scheduler started")

	// Start API server
	srv := startAPI(cfg.APIAddr, db, agent)
	log.Printf("API server started on %s", cfg.APIAddr)

	// Send startup notification
	nd.send("sdk-ops-agent started", fmt.Sprintf("Agent v%s started on %s", version, getLocalIP()))

	// Log startup
	insertAudit(db, AuditRow{
		Timestamp: time.Now(),
		Action:    "agent:start",
		Status:    "ok",
		Message:   fmt.Sprintf("Agent v%s started", version),
	})

	// Parse interval
	interval, err := time.ParseDuration(cfg.Interval)
	if err != nil {
		interval = 60 * time.Second
	}

	// Parse retention
	retention, err := time.ParseDuration(cfg.Retention)
	if err != nil {
		retention = 720 * time.Hour // 30 days
	}

	// Metrics collection ticker
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Purge ticker (daily)
	purgeTicker := time.NewTicker(24 * time.Hour)
	defer purgeTicker.Stop()

	// Schedules reload ticker (every 5 minutes)
	reloadTicker := time.NewTicker(5 * time.Minute)
	defer reloadTicker.Stop()

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Println("agent running")

	for {
		select {
		case <-ticker.C:
			m, err := collectMetrics()
			if err != nil {
				log.Printf("metrics: %v", err)
				continue
			}
			if err := insertMetric(db, m); err != nil {
				log.Printf("metrics insert: %v", err)
			}

		case <-purgeTicker.C:
			if err := purgeOldMetrics(db, retention); err != nil {
				log.Printf("purge: %v", err)
			}

		case <-reloadTicker.C:
			sched.loadSchedules()

		case sig := <-sigCh:
			log.Printf("signal: %v, shutting down", sig)
			sched.stop()
			srv.Close()
			insertAudit(db, AuditRow{
				Timestamp: time.Now(),
				Action:    "agent:stop",
				Status:    "ok",
				Message:   fmt.Sprintf("signal: %v", sig),
			})
			nd.send("sdk-ops-agent stopped", fmt.Sprintf("Agent stopped (signal: %v)", sig))
			os.Exit(0)
		}
	}
}

func (a *Agent) uptime() string {
	return time.Since(a.startTime).Round(time.Second).String()
}
