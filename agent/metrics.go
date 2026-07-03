package main

import (
	"context"
	"fmt"
	"log"
	gosnet "net"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	gopsnet "github.com/shirou/gopsutil/v4/net"
)

func collectMetrics() MetricRow {
	var m MetricRow
	m.Timestamp = time.Now()

	cpuPercents, err := cpu.Percent(0, false)
	if err == nil && len(cpuPercents) > 0 {
		m.CPUPercent = cpuPercents[0]
	}

	if vmem, err := mem.VirtualMemory(); err == nil {
		m.MemoryTotal = vmem.Total
		m.MemoryUsed = vmem.Used
	}

	partitions, err := disk.Partitions(false)
	if err == nil {
		for _, p := range partitions {
			if p.Mountpoint == "/" {
				if usage, err := disk.Usage(p.Mountpoint); err == nil {
					m.DiskTotal = usage.Total
					m.DiskUsed = usage.Used
				}
				break
			}
		}
	}

	netIO, err := gopsnet.IOCounters(false)
	if err == nil && len(netIO) > 0 {
		m.NetRx = netIO[0].BytesRecv
		m.NetTx = netIO[0].BytesSent
	}

	return m
}

func getHostInfo() map[string]string {
	hostname, _ := os.Hostname()
	return map[string]string{
		"hostname":  hostname,
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
		"go_version": runtime.Version(),
		"uptime":    getUptime(),
	}
}

func getUptime() string {
	out, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "unknown"
	}
	var uptimeSec float64
	if _, err := fmt.Sscanf(string(out), "%f", &uptimeSec); err != nil { log.Printf("metrics: parse uptime error: %v", err) }
	uptime := time.Duration(uptimeSec) * time.Second
	days := int(uptime.Hours()) / 24
	hours := int(uptime.Hours()) % 24
	mins := int(uptime.Minutes()) % 60
	return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
}

func getLocalIP() string {
	conn, err := (&gosnet.Dialer{}).DialContext(context.Background(), "udp", "8.8.8.8:80")
	if err != nil {
		return "unknown"
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "metrics: conn close error: %v\n", err) } }()
	addr := conn.LocalAddr().(*gosnet.UDPAddr)
	return addr.IP.String()
}
