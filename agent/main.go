package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
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
	defer func() { if err := db.Close(); err != nil { fmt.Fprintf(os.Stderr, "db close error: %v\n", err) } }()

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

	sched.start()
	log.Println("scheduler started")

	srv := startAPI(cfg.APIAddr, db, agent)
	log.Printf("API server started on %s", cfg.APIAddr)

	nd.send("sdk-ops-agent started", fmt.Sprintf("Agent v%s started on %s", version, getLocalIP()))

	if err := insertAudit(db, AuditRow{
		Timestamp: time.Now(),
		Action:    "agent:start",
		Status:    "ok",
		Message:   fmt.Sprintf("Agent v%s started", version),
	}); err != nil {
		log.Printf("agent: audit error: %v", err)
	}

	runEventLoop(cfg, db, sched, nd, srv)
}

func (a *Agent) uptime() string {
	return time.Since(a.startTime).Round(time.Second).String()
}

func runEventLoop(cfg AgentConfig, db *sql.DB, sched *Scheduler, nd *notifyDispatcher, srv *http.Server) {
	interval, err := time.ParseDuration(cfg.Interval)
	if err != nil {
		interval = 60 * time.Second
	}

	retention, err := time.ParseDuration(cfg.Retention)
	if err != nil {
		retention = 720 * time.Hour
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	purgeTicker := time.NewTicker(24 * time.Hour)
	defer purgeTicker.Stop()
	reloadTicker := time.NewTicker(5 * time.Minute)
	defer reloadTicker.Stop()
	updateTicker := time.NewTicker(24 * time.Hour)
	defer updateTicker.Stop()
	containerHealthTicker := time.NewTicker(2 * time.Minute)
	defer containerHealthTicker.Stop()
	diskCheckTicker := time.NewTicker(10 * time.Minute)
	defer diskCheckTicker.Stop()
	certCheckTicker := time.NewTicker(6 * time.Hour)
	defer certCheckTicker.Stop()
	latencyTicker := time.NewTicker(5 * time.Minute)
	defer latencyTicker.Stop()
	tempTicker := time.NewTicker(10 * time.Minute)
	defer tempTicker.Stop()

	autoUpdateCheck(cfg)

	eventStop := make(chan struct{})
	go watchDockerEvents(db, eventStop)
	logStop := make(chan struct{})
	go watchContainerLogs(db, logStop)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Println("agent running")

	for {
		select {
		case <-ticker.C:
			handleMetricsTick(db)
		case <-purgeTicker.C:
			if err := purgeOldMetrics(db, retention); err != nil {
				log.Printf("purge: %v", err)
			}
		case <-reloadTicker.C:
			sched.loadSchedules()
		case <-updateTicker.C:
			autoUpdateCheck(cfg)
		case <-containerHealthTicker.C:
			handleContainerHealthTick(db)
		case <-diskCheckTicker.C:
			handleDiskCheckTick(db)
		case <-certCheckTicker.C:
			handleCertCheckTick(db)
		case <-latencyTicker.C:
			handleLatencyTick(db)
		case <-tempTicker.C:
			handleTempTick(db)
		case sig := <-sigCh:
			handleShutdownSignal(sig, db, sched, nd, srv, eventStop, logStop)
		}
	}
}

func handleMetricsTick(db *sql.DB) {
	m := collectMetrics()
	if err := insertMetric(db, m); err != nil {
		log.Printf("metrics insert: %v", err)
	}
}

func handleContainerHealthTick(db *sql.DB) {
	results := checkContainerHealth()
	for _, r := range results {
		if r.AutoHealed {
			if err := insertAudit(db, AuditRow{
				Timestamp: time.Now(),
				Action:    "health:auto-heal",
				Status:    "warning",
				Message:   fmt.Sprintf("%s restarted (was unhealthy)", r.Name),
			}); err != nil {
				log.Printf("agent: audit error: %v", err)
			}
		}
		if !r.Healthy && r.Status == "running" {
			log.Printf("health: %s unhealthy (port %s), auto-heal triggered", r.Name, r.Ports)
		}
	}
}

func handleDiskCheckTick(db *sql.DB) {
	disks := checkDiskUsage()
	for _, d := range disks {
		switch d.Status {
		case "critical":
			if err := insertAudit(db, AuditRow{
				Timestamp: time.Now(),
				Action:    "disk:critical",
				Status:    "critical",
				Message:   fmt.Sprintf("%s at %.0f%%", d.Mount, d.UsedPercent),
			}); err != nil {
				log.Printf("agent: audit error: %v", err)
			}
			log.Printf("disk: CRITICAL %s at %.0f%% - running prune", d.Mount, d.UsedPercent)
			autoPruneDisk()
		case "warning":
			log.Printf("disk: WARNING %s at %.0f%%", d.Mount, d.UsedPercent)
		}
	}
}

func handleCertCheckTick(db *sql.DB) {
	certs := checkSSLCerts()
	for _, c := range certs {
		switch c.Status {
		case "critical", "expired":
			if err := insertAudit(db, AuditRow{
				Timestamp: time.Now(),
				Action:    "cert:expiring",
				Status:    "critical",
				Message:   fmt.Sprintf("%s expires in %s", c.Domain, c.ExpiresIn),
			}); err != nil {
				log.Printf("agent: audit error: %v", err)
			}
			log.Printf("cert: %s expires in %s", c.Domain, c.ExpiresIn)
		case "warning":
			log.Printf("cert: %s expires in %s (warning)", c.Domain, c.ExpiresIn)
		}
	}
}

func handleLatencyTick(db *sql.DB) {
	results := checkNetworkLatency()
	for _, pr := range results {
		switch pr.Status {
		case "critical":
			if err := insertAudit(db, AuditRow{
				Timestamp: time.Now(),
				Action:    "net:latency",
				Status:    "critical",
				Message:   fmt.Sprintf("%s: %.0fms, %.0f%% loss", pr.Target, pr.LatencyMs, pr.PacketLoss),
			}); err != nil {
				log.Printf("agent: audit error: %v", err)
			}
			log.Printf("net: %s unreachable (%.0fms, %.0f%% loss)", pr.Target, pr.LatencyMs, pr.PacketLoss)
		case "warning":
			log.Printf("net: %s slow (%.0fms)", pr.Target, pr.LatencyMs)
		}
	}
}

func handleTempTick(db *sql.DB) {
	temps := checkTemperature()
	for _, t := range temps {
		if t.Status == "critical" || t.Status == "warning" {
			log.Printf("temp: %s at %.1f°C (%s)", t.Sensor, t.TempC, t.Status)
			if err := insertAudit(db, AuditRow{
				Timestamp: time.Now(),
				Action:    "temp:high",
				Status:    t.Status,
				Message:   fmt.Sprintf("%s: %.1f°C", t.Sensor, t.TempC),
			}); err != nil {
				log.Printf("agent: audit error: %v", err)
			}
		}
	}
}

func handleShutdownSignal(sig os.Signal, db *sql.DB, sched *Scheduler, nd *notifyDispatcher, srv *http.Server, eventStop, logStop chan struct{}) {
	log.Printf("signal: %v, shutting down", sig)
	close(eventStop)
	close(logStop)
	sched.stop()
	if err := srv.Close(); err != nil {
		log.Printf("server close error: %v", err)
	}
	if err := insertAudit(db, AuditRow{
		Timestamp: time.Now(),
		Action:    "agent:stop",
		Status:    "ok",
		Message:   fmt.Sprintf("signal: %v", sig),
	}); err != nil {
		log.Printf("agent: audit error: %v", err)
	}
	nd.send("sdk-ops-agent stopped", fmt.Sprintf("Agent stopped (signal: %v)", sig))
}
