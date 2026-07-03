package main

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 1. Container health monitor + auto-heal
type ContainerHealth struct {
	Name       string `json:"name"`
	Image      string `json:"image"`
	Ports      string `json:"ports"`
	Status     string `json:"status"`
	HealthURL  string `json:"health_url"`
	Healthy    bool   `json:"healthy"`
	LastCheck  string `json:"last_check"`
	AutoHealed bool   `json:"auto_healed"`
}

func checkContainerHealth() []ContainerHealth {
	var results []ContainerHealth
	out, err := exec.CommandContext(context.Background(), "docker", "ps", "--format", "{{.Names}}|{{.Image}}|{{.Ports}}|{{.Status}}").Output()
	if err != nil {
		log.Printf("health: docker ps failed: %v", err)
		return results
	}

	lines := strings.SplitSeq(strings.TrimSpace(string(out)), "\n")
	for line := range lines {
		ch, ok := checkSingleContainer(line)
		if !ok {
			continue
		}
		results = append(results, ch)
	}

	// Skip sdk-ops-agent container itself
	var filtered []ContainerHealth
	for _, r := range results {
		if r.Name == "sdk-ops-agent" {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

func checkSingleContainer(line string) (ContainerHealth, bool) {
	parts := strings.SplitN(line, "|", 4)
	if len(parts) < 3 {
		return ContainerHealth{}, false
	}
	ch := ContainerHealth{
		Name:   parts[0],
		Image:  parts[1],
		Ports:  parts[2],
		Status: "running",
	}
	if len(parts) >= 4 {
		ch.Status = parts[3]
	}

	port := extractContainerPort(ch.Ports)
	containerIP := getContainerIP(ch.Name)

	if port > 0 && containerIP != "" {
		urls := []string{
			fmt.Sprintf("http://%s:%d/health", containerIP, port),
			fmt.Sprintf("http://%s:%d/", containerIP, port),
		}
		for _, url := range urls {
			client := &http.Client{Timeout: 3 * time.Second}
			req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
			if err != nil {
				log.Printf("health: create request: %v", err)
				continue
			}
			resp, err := client.Do(req)
			if err == nil {
				if err := resp.Body.Close(); err != nil {
					log.Printf("health: resp body close error: %v", err)
				}
				if resp.StatusCode == 200 {
					ch.Healthy = true
					ch.HealthURL = url
					break
				}
			}
		}
	} else {
		ch.Healthy = ch.Status == "running"
	}

	if !ch.Healthy && ch.Status == "running" {
		if _, ok := validContainerName(ch.Name); !ok {
			log.Printf("health: skipping invalid container name: %q", ch.Name)
			return ContainerHealth{}, false
		}
		log.Printf("health: %s unhealthy, restarting...", ch.Name)
		restartCmd := exec.CommandContext(context.Background(), "docker")
		restartCmd.Args = append(restartCmd.Args, "restart", ch.Name)
		restartOut, err := restartCmd.CombinedOutput()
		if err != nil {
			log.Printf("health: restart %s failed: %v\n%s", ch.Name, err, string(restartOut))
		} else {
			ch.AutoHealed = true
			log.Printf("health: %s restarted successfully", ch.Name)
		}
	}

	return ch, true
}

func getContainerIP(name string) string {
	containerName, ok := validContainerName(name)
	if !ok {
		return ""
	}
	inspectCmd := exec.CommandContext(context.Background(), "docker")
	inspectCmd.Args = append(inspectCmd.Args, "inspect", "--format", "{{.NetworkSettings.IPAddress}}", containerName)
	out, err := inspectCmd.Output()
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" || ip == "<no value>" {
		// Try Networks default IP
		inspect2Cmd := exec.CommandContext(context.Background(), "docker")
		inspect2Cmd.Args = append(inspect2Cmd.Args, "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", containerName)
		out2, err2 := inspect2Cmd.Output()
		if err2 != nil {
			return ""
		}
		return strings.TrimSpace(string(out2))
	}
	return ip
}

func extractContainerPort(ports string) int {
	if ports == "" {
		return 0
	}
	// Format: "0.0.0.0:8080->8080/tcp" or "8080/tcp"
	parts := strings.Split(ports, "->")
	if len(parts) >= 2 {
		hostPart := parts[0]
		if idx := strings.LastIndex(hostPart, ":"); idx >= 0 {
			var p int
			if _, err := fmt.Sscanf(hostPart[idx+1:], "%d", &p); err != nil {
				log.Printf("health: parse port error: %v", err)
			}
			return p
		}
	}
	// Try direct port format
	var p int
	if _, err := fmt.Sscanf(strings.Split(ports, "/")[0], "%d", &p); err != nil {
		log.Printf("health: parse port error: %v", err)
	}
	return p
}

// 2. Disk usage monitor
type DiskInfo struct {
	Mount       string  `json:"mount"`
	TotalGB     float64 `json:"total_gb"`
	UsedGB      float64 `json:"used_gb"`
	UsedPercent float64 `json:"used_percent"`
	Status      string  `json:"status"`
}

func checkDiskUsage() []DiskInfo {
	var results []DiskInfo
	out, err := exec.CommandContext(context.Background(), "/bin/df", "-kP", "/").Output()
	if err != nil {
		log.Printf("disk: df failed: %v", err)
		return results
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Filesystem") || strings.HasPrefix(line, "Mounted") || strings.HasPrefix(line, "target") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}

		// `df -k` format: filesystem 1K-blocks used available use% mountpoint
		var d DiskInfo
		d.Mount = fields[len(fields)-1]
		blocks, errB := strconv.ParseFloat(fields[1], 64)
		used, errU := strconv.ParseFloat(fields[2], 64)
		if errB != nil {
			log.Printf("disk: parse blocks %q: %v", fields[1], errB)
		} else {
			d.TotalGB = blocks / 1048576
		}
		if errU != nil {
			log.Printf("disk: parse used %q: %v", fields[2], errU)
		} else {
			d.UsedGB = used / 1048576
		}
		log.Printf("disk: fields=[%s] mount=%s blocks=%s used=%s totalGB=%.1f usedGB=%.1f",
			strings.Join(fields, ","), d.Mount, fields[1], fields[2], d.TotalGB, d.UsedGB)
		if _, err := fmt.Sscanf(fields[len(fields)-2], "%f%%", &d.UsedPercent); err != nil {
			log.Printf("health: parse percent error: %v", err)
		}

		// Only monitor root mount point
		if d.Mount != "/" && d.Mount != "/data" {
			continue
		}

		d.UsedPercent = clampPercent(d.UsedPercent)
		switch {
		case d.UsedPercent >= 95:
			d.Status = "critical"
		case d.UsedPercent >= 80:
			d.Status = "warning"
		default:
			d.Status = "ok"
		}

		results = append(results, d)
	}

	return results
}

func clampPercent(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func autoPruneDisk() {
	log.Println("disk: running auto-prune (disk >95%)")
	out, err := exec.CommandContext(context.Background(), "docker", "system", "prune", "-af", "--volumes").CombinedOutput()
	if err != nil {
		log.Printf("disk: prune failed: %v\n%s", err, string(out))
		return
	}
	log.Printf("disk: prune completed:\n%s", string(out))
}

// 3. SSL expiry monitor
type CertInfo struct {
	Domain    string `json:"domain"`
	ExpiresIn string `json:"expires_in"`
	DaysLeft  int    `json:"days_left"`
	Status    string `json:"status"`
	Valid     bool   `json:"valid"`
}

func checkSSLCerts() []CertInfo {
	var results []CertInfo

	results = appendCaddyCerts(results)
	results = appendSSLCerts(results)

	return results
}

func appendCaddyCerts(results []CertInfo) []CertInfo {
	caddyDir := "/var/lib/caddy/.local/share/caddy/certificates"
	if _, err := os.Stat(caddyDir); err != nil {
		return results
	}

	if err := filepath.Walk(caddyDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".crt") {
			return nil
		}
		ci := checkCertFile(path)
		if ci != nil {
			results = append(results, *ci)
		}
		return nil
	}); err != nil {
		log.Printf("health: walk error: %v", err)
	}
	return results
}

func appendSSLCerts(results []CertInfo) []CertInfo {
	sslDir := "/etc/ssl/certs"
	if _, err := os.Stat(sslDir); err != nil {
		return results
	}

	if err := filepath.Walk(sslDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || (!strings.HasSuffix(path, ".crt") && !strings.HasSuffix(path, ".pem")) {
			return nil
		}
		ci := checkCertFile(path)
		if ci != nil {
			for _, existing := range results {
				if existing.Domain == ci.Domain {
					return nil
				}
			}
			results = append(results, *ci)
		}
		return nil
	}); err != nil {
		log.Printf("health: walk error: %v", err)
	}
	return results
}

func checkCertFile(path string) *CertInfo {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil
	}

	// Try to find the cert in PEM
	block, _ := pemDecode(data)
	if block == nil {
		return nil
	}

	cert, err := x509ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}

	ci := &CertInfo{
		Domain: strings.Join(cert.DNSNames, ", "),
		Valid:  time.Now().Before(cert.NotAfter),
	}
	if ci.Domain == "" {
		ci.Domain = cert.Subject.CommonName
	}

	ci.DaysLeft = max(int(time.Until(cert.NotAfter).Hours()/24), 0)

	switch {
	case ci.DaysLeft <= 0:
		ci.Status = "expired"
	case ci.DaysLeft <= 7:
		ci.Status = "critical"
	case ci.DaysLeft <= 30:
		ci.Status = "warning"
	default:
		ci.Status = "ok"
	}

	if ci.DaysLeft > 0 {
		ci.ExpiresIn = fmt.Sprintf("%dd", ci.DaysLeft)
	} else {
		ci.ExpiresIn = "expired"
	}

	return ci
}

// 9. Network latency monitor
type PingResult struct {
	Target     string  `json:"target"`
	LatencyMs  float64 `json:"latency_ms"`
	PacketLoss float64 `json:"packet_loss"`
	Status     string  `json:"status"`
}

func checkNetworkLatency() []PingResult {
	targets := []string{"github.com", "google.com", "8.8.8.8"}
	var results []PingResult

	for _, target := range targets {
		results = append(results, pingTarget(target))
	}

	return results
}

func pingTarget(target string) PingResult {
	pr := PingResult{Target: target}

	cmd := exec.CommandContext(context.Background(), "ping")
	cmd.Args = append(cmd.Args, "-c", "3", "-W", "2", target)
	out, err := cmd.Output()
	if err != nil {
		pr.Status = "unreachable"
		pr.PacketLoss = 100
		return pr
	}

	output := string(out)
	pr.PacketLoss = parsePingPacketLoss(output)
	pr.LatencyMs = parsePingLatency(output)

	switch {
	case pr.PacketLoss >= 50:
		pr.Status = "critical"
	case pr.PacketLoss >= 10 || pr.LatencyMs > 200:
		pr.Status = "warning"
	default:
		pr.Status = "ok"
	}

	return pr
}

func parsePingPacketLoss(output string) float64 {
	if before, _, ok := strings.Cut(output, "packet loss"); ok {
		before := before
		if lastSpace := strings.LastIndex(strings.TrimSpace(before), " "); lastSpace >= 0 {
			var loss float64
			if _, err := fmt.Sscanf(before[lastSpace:], "%f", &loss); err != nil {
				log.Printf("health: parse loss error: %v", err)
			}
			return loss
		}
	}
	return 0
}

func parsePingLatency(output string) float64 {
	if idx := strings.Index(output, "min/avg/max"); idx >= 0 {
		after := output[idx:]
		if _, after0, ok := strings.Cut(after, "="); ok {
			stats := strings.TrimSpace(after0)
			parts := strings.Split(stats, "/")
			if len(parts) >= 2 {
				var ms float64
				if _, err := fmt.Sscanf(parts[1], "%f", &ms); err != nil {
					log.Printf("health: parse latency error: %v", err)
				}
				return ms
			}
		}
	}

	if idx := strings.Index(output, "round-trip"); idx >= 0 {
		after := output[idx:]
		if _, after0, ok := strings.Cut(after, "="); ok {
			stats := strings.TrimSpace(after0)
			parts := strings.Split(stats, "/")
			if len(parts) >= 2 {
				var ms float64
				if _, err := fmt.Sscanf(parts[1], "%f", &ms); err != nil {
					log.Printf("health: parse latency error: %v", err)
				}
				return ms
			}
		}
	}

	return 0
}

// 10. Temperature monitor
type TempInfo struct {
	Sensor string  `json:"sensor"`
	TempC  float64 `json:"temp_c"`
	Status string  `json:"status"`
}

func checkTemperature() []TempInfo {
	results := checkThermalZones()
	if len(results) > 0 {
		return results
	}
	return checkLMSensors()
}

func checkThermalZones() []TempInfo {
	var results []TempInfo

	matches, err := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	if err != nil || len(matches) == 0 {
		return results
	}

	for _, m := range matches {
		data, err := os.ReadFile(filepath.Clean(m))
		if err != nil {
			continue
		}
		var millicelsius int
		if _, err := fmt.Sscanf(string(data), "%d", &millicelsius); err != nil {
			log.Printf("health: parse temp error: %v", err)
		}
		if millicelsius <= 0 {
			continue
		}

		ti := TempInfo{
			Sensor: filepath.Base(filepath.Dir(m)),
			TempC:  float64(millicelsius) / 1000.0,
		}

		switch {
		case ti.TempC >= 90:
			ti.Status = "critical"
		case ti.TempC >= 80:
			ti.Status = "warning"
		default:
			ti.Status = "ok"
		}

		results = append(results, ti)
	}

	return results
}

func checkLMSensors() []TempInfo {
	var results []TempInfo

	out, err := exec.CommandContext(context.Background(), "sensors", "-u").Output()
	if err != nil {
		return results
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	var currentSensor string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if before, ok := strings.CutSuffix(line, ":"); ok {
			currentSensor = before
		}
		if strings.HasPrefix(line, "temp1_input:") {
			var tempC float64
			if _, err := fmt.Sscanf(line, "temp1_input: %f", &tempC); err != nil {
				log.Printf("health: parse temp error: %v", err)
			}
			if tempC > 0 {
				ti := TempInfo{Sensor: currentSensor, TempC: tempC}
				switch {
				case ti.TempC >= 90:
					ti.Status = "critical"
				case ti.TempC >= 80:
					ti.Status = "warning"
				default:
					ti.Status = "ok"
				}
				results = append(results, ti)
			}
		}
	}

	return results
}

func pemDecode(data []byte) (*pem.Block, []byte) {
	return pem.Decode(data)
}

func x509ParseCertificate(der []byte) (*x509.Certificate, error) {
	return x509.ParseCertificate(der)
}
