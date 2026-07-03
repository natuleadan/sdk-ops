package monitor

import (
	"fmt"
	"strconv"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type NodeStats struct {
	Hostname string
	Kernel   string
	Uptime   string
	CPU      string
	Memory   string
	Disk     string
	NetIn    string
	NetOut   string
	CPUCores int
	CPULoad  string
	MemUsed  string
	MemTotal string
	DiskUsed string
	DiskSize string
}

type RuntimeStatus struct {
	K3sVersion string
	K3sRunning string
	DockerVer  string
	DockerOK   string
	PodCount   string
}

type Process struct {
	PID  int
	CPU  float64
	MEM  float64
	Cmd  string
	User string
}

func getStatsScript() string {
	return `#!/bin/bash
echo "HOSTNAME=$(hostname)"
echo "KERNEL=$(uname -r)"
echo "UPTIME=$(uptime -p | sed 's/up //')"
echo "CPUS=$(nproc)"
echo "LOAD=$(uptime | awk -F'load average:' '{print $2}' | xargs)"
echo "MEM_TOTAL=$(free -h | awk '/^Mem:/ {print $2}')"
echo "MEM_USED=$(free -h | awk '/^Mem:/ {print $3}')"
echo "MEM_PCT=$(free | awk '/^Mem:/ {printf "%.0f", $3/$2 * 100}')"
echo "DISK_TOTAL=$(df -h / | awk 'NR==2 {print $2}')"
echo "DISK_USED=$(df -h / | awk 'NR==2 {print $3}')"
echo "DISK_PCT=$(df -h / | awk 'NR==2 {print $5}' | tr -d '%')"
echo "NET_IN=$(cat /proc/net/dev | awk '/eth0/ {print $2}' || cat /proc/net/dev | awk '/ens/ {print $2}' || echo 0)"
echo "NET_OUT=$(cat /proc/net/dev | awk '/eth0/ {print $10}' || cat /proc/net/dev | awk '/ens/ {print $10}' || echo 0)"
`
}

func setStatPct(s *NodeStats, val string) {
	if pct, err := strconv.Atoi(val); err == nil {
		s.Memory = fmt.Sprintf("%d%%", pct)
	}
}

func setStatDiskPct(s *NodeStats, val string) {
	if pct, err := strconv.Atoi(val); err == nil {
		s.Disk = fmt.Sprintf("%d%%", pct)
	}
}

func setStatNet(s *NodeStats, key, val string) {
	b, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return
	}
	switch key {
	case "NET_IN":
		s.NetIn = formatBytes(b)
	case "NET_OUT":
		s.NetOut = formatBytes(b)
	}
}

func setStatField(s *NodeStats, key, val string) {
	switch key {
	case "HOSTNAME":
		s.Hostname = val
	case "KERNEL":
		s.Kernel = val
	case "UPTIME":
		s.Uptime = val
	case "CPUS":
		s.CPUCores, _ = strconv.Atoi(val)
	case "LOAD":
		s.CPULoad = val
	case "MEM_TOTAL":
		s.MemTotal = val
	case "MEM_USED":
		s.MemUsed = val
	case "MEM_PCT":
		setStatPct(s, val)
	case "DISK_TOTAL":
		s.DiskSize = val
	case "DISK_USED":
		s.DiskUsed = val
	case "DISK_PCT":
		setStatDiskPct(s, val)
	case "NET_IN":
		setStatNet(s, key, val)
	case "NET_OUT":
		setStatNet(s, key, val)
	}
}

func parseStatsOutput(out string) *NodeStats {
	stats := &NodeStats{}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		setStatField(stats, strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}
	return stats
}

func GetStats(client *goss.Client) (*NodeStats, error) {
	out, _, err := ssh.Run(client, getStatsScript())
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}
	return parseStatsOutput(out), nil
}

func getRuntimeScript() string {
	return `
K3S_VER=$(k3s --version 2>/dev/null | head -1 | awk '{print $3}' | sed 's/^v//' || echo "")
K3S_OK=$(systemctl is-active k3s 2>/dev/null || echo "inactive")
DOCKER_VER=$(docker --version 2>/dev/null | awk '{print $3}' | tr -d , || echo "")
DOCKER_OK=$(systemctl is-active docker 2>/dev/null || echo "inactive")
POD_COUNT=$(KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl get pods --all-namespaces 2>/dev/null | tail -n +2 | wc -l || echo "0")
echo "K3S_VER=$K3S_VER"
echo "K3S_OK=$K3S_OK"
echo "DOCKER_VER=$DOCKER_VER"
echo "DOCKER_OK=$DOCKER_OK"
echo "POD_COUNT=$POD_COUNT"
`
}

func parseRuntimeOutput(out string) *RuntimeStatus {
	rs := &RuntimeStatus{}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		switch strings.TrimSpace(parts[0]) {
		case "K3S_VER":
			rs.K3sVersion = val
		case "K3S_OK":
			rs.K3sRunning = val
		case "DOCKER_VER":
			rs.DockerVer = val
		case "DOCKER_OK":
			rs.DockerOK = val
		case "POD_COUNT":
			rs.PodCount = val
		}
	}
	return rs
}

func GetRuntimeStatus(client *goss.Client) *RuntimeStatus {
	out, _, _ := ssh.Run(client, getRuntimeScript())
	return parseRuntimeOutput(out)
}

func GetTopProcesses(client *goss.Client, count int) ([]Process, error) {
	script := fmt.Sprintf(`ps aux --sort=-%%cpu | head -n %d | tail -n +2 | awk '{printf "%%s|%%s|%%s|%%s|%%s\n", $2, $1, $3, $4, $11}'`, count+1)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return nil, fmt.Errorf("top processes: %w", err)
	}

	var procs []Process
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		pid, _ := strconv.Atoi(parts[0])
		cpu, _ := strconv.ParseFloat(parts[2], 64)
		mem, _ := strconv.ParseFloat(parts[3], 64)
		cmd := parts[4]
		if len(cmd) > 30 {
			cmd = cmd[:30]
		}
		procs = append(procs, Process{
			PID:  pid,
			CPU:  cpu,
			MEM:  mem,
			Cmd:  cmd,
			User: parts[1],
		})
	}
	return procs, nil
}

func RunCommand(client *goss.Client, cmd string) (string, string, error) {
	return ssh.Run(client, cmd)
}

func RunInteractive(client *goss.Client, cmd string) error {
	return ssh.RunPTY(client, cmd)
}

func bar(pct int, width int) string {
	filled := max(min(pct*width/100, width), 0)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func statusIcon(status string) string {
	if status == "active" || status == "yes" || status == "OK" {
		return "✅"
	}
	return "❌"
}

func FormatStats(stats *NodeStats, runtime *RuntimeStatus, procs []Process) string {
	memPct := 0
	if pct, err := strconv.Atoi(strings.TrimSuffix(stats.Memory, "%")); err == nil {
		memPct = pct
	}
	diskPct := 0
	if pct, err := strconv.Atoi(strings.TrimSuffix(stats.Disk, "%")); err == nil {
		diskPct = pct
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n  ┌─ Node: %s (%s)\n", stats.Hostname, stats.Kernel)
	fmt.Fprintf(&b, "  ├─ Uptime: %s\n", stats.Uptime)
	fmt.Fprintf(&b, "  ├─ CPU:    %s %3s  (%d cores, load: %s)\n", bar(memPct, 20), stats.Memory, stats.CPUCores, stats.CPULoad)
	fmt.Fprintf(&b, "  ├─ RAM:    %s %3s  (%s / %s)\n", bar(memPct, 20), stats.Memory, stats.MemUsed, stats.MemTotal)
	fmt.Fprintf(&b, "  ├─ DISK:   %s %3s  (%s / %s)\n", bar(diskPct, 20), stats.Disk, stats.DiskUsed, stats.DiskSize)
	fmt.Fprintf(&b, "  ├─ NET:    ↑ %s  ↓ %s\n", stats.NetOut, stats.NetIn)

	if runtime != nil {
		if runtime.K3sVersion != "" {
			fmt.Fprintf(&b, "  ├─ K3s:    %s v%s  (%s)\n", statusIcon(runtime.K3sRunning), runtime.K3sVersion, runtime.K3sRunning)
		}
		if runtime.DockerVer != "" {
			fmt.Fprintf(&b, "  ├─ Docker: %s v%s  (%s)\n", statusIcon(runtime.DockerOK), runtime.DockerVer, runtime.DockerOK)
		}
		if runtime.K3sRunning == "active" {
			fmt.Fprintf(&b, "  ├─ Pods:   %s running\n", runtime.PodCount)
		}
	}

	if len(procs) > 0 {
		b.WriteString("  └─ Top processes:\n")
		b.WriteString("       PID    CPU%   MEM%  USER       COMMAND\n")
		for _, p := range procs {
			fmt.Fprintf(&b, "       %-5d  %5.1f  %5.1f  %-10s %s\n", p.PID, p.CPU, p.MEM, p.User, p.Cmd)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
