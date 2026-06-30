package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// 6. Port scanner / inventory
type ServiceInventory struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Process  string `json:"process"`
	PID      int    `json:"pid"`
	Type     string `json:"type"` // docker or host
}

func scanInventory() []ServiceInventory {
	return append(scanDockerPorts(), scanHostPorts()...)
}

func scanDockerPorts() []ServiceInventory {
	var results []ServiceInventory
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}|{{.Ports}}").Output()
	if err != nil {
		return results
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		portsStr := parts[1]

		// Parse "0.0.0.0:8080->8080/tcp, :::8080->8080/tcp"
		// Deduplicate by port
		seenPorts := make(map[int]bool)
		portMappings := strings.Split(portsStr, ",")
		for _, pm := range portMappings {
			pm = strings.TrimSpace(pm)
			if pm == "" {
				continue
			}
			// Get host port
			parts2 := strings.Split(pm, "->")
			if len(parts2) >= 1 {
				hostPart := parts2[0]
				// Extract port number
				if idx := strings.LastIndex(hostPart, ":"); idx >= 0 {
					var p int
					fmt.Sscanf(hostPart[idx+1:], "%d", &p)
					if p > 0 && !seenPorts[p] {
						seenPorts[p] = true
						protocol := "tcp"
						if strings.Contains(pm, "udp") {
							protocol = "udp"
						}
						results = append(results, ServiceInventory{
							Port: p, Protocol: protocol,
							Process: name, Type: "docker",
						})
					}
				}
			}
		}
	}

	return results
}

func scanHostPorts() []ServiceInventory {
	var results []ServiceInventory

	// Try ss first (modern Linux)
	out, err := exec.Command("ss", "-tlnp", "4").Output()
	if err != nil {
		// Fall back to netstat
		out, err = exec.Command("netstat", "-tlnp").Output()
		if err != nil {
			return results
		}
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "State") || strings.HasPrefix(line, "Active") || strings.HasPrefix(line, "Proto") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		// Parse local address:port
		localAddr := fields[len(fields)-3]
		if idx := strings.LastIndex(localAddr, ":"); idx >= 0 {
			portStr := localAddr[idx+1:]
			// Skip port 0 (ephemeral)
			if portStr == "0" || portStr == "*" {
				continue
			}
			var p int
			fmt.Sscanf(portStr, "%d", &p)
			if p <= 0 || p > 65535 {
				continue
			}

			protocol := "tcp"
			if len(fields) > 0 && strings.HasPrefix(fields[0], "udp") {
				protocol = "udp"
			}

			process := ""
			if len(fields) >= 5 {
				process = fields[len(fields)-1]
			}

			// Skip if already in docker results (dedup)
			alreadySeen := false
			for _, existing := range results {
				if existing.Port == p {
					alreadySeen = true
					break
				}
			}
			if !alreadySeen {
				results = append(results, ServiceInventory{
					Port: p, Protocol: protocol,
					Process: process, Type: "host",
				})
			}
		}
	}

	return results
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
