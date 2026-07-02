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

	// Update check ticker (every 24h, only if enabled)
	updateTicker := time.NewTicker(24 * time.Hour)
	defer updateTicker.Stop()

	// 1. Container health check (every 2 minutes)
	containerHealthTicker := time.NewTicker(2 * time.Minute)
	defer containerHealthTicker.Stop()

	// 2. Disk usage check (every 10 minutes)
	diskCheckTicker := time.NewTicker(10 * time.Minute)
	defer diskCheckTicker.Stop()

	// 3. SSL cert check (every 6 hours)
	certCheckTicker := time.NewTicker(6 * time.Hour)
	defer certCheckTicker.Stop()

	// 9. Network latency check (every 5 minutes)
	latencyTicker := time.NewTicker(5 * time.Minute)
	defer latencyTicker.Stop()

	// 10. Temperature check (every 10 minutes)
	tempTicker := time.NewTicker(10 * time.Minute)
	defer tempTicker.Stop()

	// Run update check once at startup if enabled
	autoUpdateCheck(cfg)

	// Start 4. Docker event watcher (separate goroutine)
	eventStop := make(chan struct{})
	go watchDockerEvents(db, eventStop)

	// Start 5. Container log watcher (separate goroutine)
	logStop := make(chan struct{})
	go watchContainerLogs(db, logStop)

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Println("agent running")

	for {
		select {
		case <-ticker.C:
			m := collectMetrics()
			_ = m
			if err := insertMetric(db, m); err != nil {
				log.Printf("metrics insert: %v", err)
			}

		case <-purgeTicker.C:
			if err := purgeOldMetrics(db, retention); err != nil {
				log.Printf("purge: %v", err)
			}

		case <-reloadTicker.C:
			sched.loadSchedules()

		case <-updateTicker.C:
			autoUpdateCheck(cfg)

		case <-containerHealthTicker.C:
			results := checkContainerHealth()
			for _, r := range results {
				if r.AutoHealed {
					insertAudit(db, AuditRow{
						Timestamp: time.Now(),
						Action:    "health:auto-heal",
						Status:    "warning",
						Message:   fmt.Sprintf("%s restarted (was unhealthy)", r.Name),
					})
				}
				if !r.Healthy && r.Status == "running" {
					log.Printf("health: %s unhealthy (port %s), auto-heal triggered", r.Name, r.Ports)
				}
			}

		case <-diskCheckTicker.C:
			disks := checkDiskUsage()
			for _, d := range disks {
				switch d.Status {
				case "critical":
					insertAudit(db, AuditRow{
						Timestamp: time.Now(),
						Action:    "disk:critical",
						Status:    "critical",
						Message:   fmt.Sprintf("%s at %.0f%%", d.Mount, d.UsedPercent),
					})
					log.Printf("disk: CRITICAL %s at %.0f%% - running prune", d.Mount, d.UsedPercent)
					autoPruneDisk()
				case "warning":
					log.Printf("disk: WARNING %s at %.0f%%", d.Mount, d.UsedPercent)
				}
			}

		case <-certCheckTicker.C:
			certs := checkSSLCerts()
			for _, c := range certs {
				switch c.Status {
				case "critical", "expired":
					insertAudit(db, AuditRow{
						Timestamp: time.Now(),
						Action:    "cert:expiring",
						Status:    "critical",
						Message:   fmt.Sprintf("%s expires in %s", c.Domain, c.ExpiresIn),
					})
					log.Printf("cert: %s expires in %s", c.Domain, c.ExpiresIn)
				case "warning":
					log.Printf("cert: %s expires in %s (warning)", c.Domain, c.ExpiresIn)
				}
			}

		case <-latencyTicker.C:
			results := checkNetworkLatency()
			for _, pr := range results {
				switch pr.Status {
				case "critical":
					insertAudit(db, AuditRow{
						Timestamp: time.Now(),
						Action:    "net:latency",
						Status:    "critical",
						Message:   fmt.Sprintf("%s: %.0fms, %.0f%% loss", pr.Target, pr.LatencyMs, pr.PacketLoss),
					})
					log.Printf("net: %s unreachable (%.0fms, %.0f%% loss)", pr.Target, pr.LatencyMs, pr.PacketLoss)
				case "warning":
					log.Printf("net: %s slow (%.0fms)", pr.Target, pr.LatencyMs)
				}
			}

		case <-tempTicker.C:
			temps := checkTemperature()
			for _, t := range temps {
				if t.Status == "critical" || t.Status == "warning" {
					log.Printf("temp: %s at %.1f°C (%s)", t.Sensor, t.TempC, t.Status)
					insertAudit(db, AuditRow{
						Timestamp: time.Now(),
						Action:    "temp:high",
						Status:    t.Status,
						Message:   fmt.Sprintf("%s: %.1f°C", t.Sensor, t.TempC),
					})
				}
			}

		case sig := <-sigCh:
			log.Printf("signal: %v, shutting down", sig)
			close(eventStop)
			close(logStop)
			sched.stop()
			srv.Close()
			insertAudit(db, AuditRow{
				Timestamp: time.Now(),
				Action:    "agent:stop",
				Status:    "ok",
				Message:   fmt.Sprintf("signal: %v", sig),
			})
			nd.send("sdk-ops-agent stopped", fmt.Sprintf("Agent stopped (signal: %v)", sig))
			return
		}
	}
}

func (a *Agent) uptime() string {
	return time.Since(a.startTime).Round(time.Second).String()
}
