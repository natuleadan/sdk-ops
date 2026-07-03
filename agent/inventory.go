package main

import (
	"context"
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
	out, err := exec.CommandContext(context.Background(), "docker", "ps", "--format", "{{.Names}}|{{.Ports}}").Output()
	if err != nil {
		return results
	}

	lines := strings.SplitSeq(strings.TrimSpace(string(out)), "\n")
	for line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		portsStr := parts[1]

		// Parse "0.0.0.0:8080->8080/tcp, :::8080->8080/tcp"
		// Deduplicate by port
		seenPorts := make(map[int]bool)
		portMappings := strings.SplitSeq(portsStr, ",")
		for pm := range portMappings {
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
					if _, err := fmt.Sscanf(hostPart[idx+1:], "%d", &p); err != nil { log.Printf("inventory: parse port error: %v", err) }
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

	out, err := exec.CommandContext(context.Background(), "ss", "-tlnp", "4").Output()
	if err != nil {
		out, err = exec.CommandContext(context.Background(), "netstat", "-tlnp").Output()
		if err != nil {
			return results
		}
	}

	lines := strings.SplitSeq(string(out), "\n")
	for line := range lines {
		si, ok := parseHostPortLine(line, results)
		if !ok {
			continue
		}
		results = append(results, si)
	}

	return results
}

func parseHostPortLine(line string, results []ServiceInventory) (ServiceInventory, bool) {
	line = strings.TrimSpace(line)
	if skipLineFilter(line) {
		return ServiceInventory{}, false
	}

	fields := strings.Fields(line)
	if len(fields) < 4 {
		return ServiceInventory{}, false
	}

	localAddr := fields[len(fields)-3]
	idx := strings.LastIndex(localAddr, ":")
	if idx < 0 {
		return ServiceInventory{}, false
	}

	portStr := localAddr[idx+1:]
	if portStr == "0" || portStr == "*" {
		return ServiceInventory{}, false
	}

	var p int
	if _, err := fmt.Sscanf(portStr, "%d", &p); err != nil {
		log.Printf("inventory: parse port error: %v", err)
		return ServiceInventory{}, false
	}
	if p <= 0 || p > 65535 {
		return ServiceInventory{}, false
	}

	if portAlreadyExists(results, p) {
		return ServiceInventory{}, false
	}

	si := ServiceInventory{
		Port: p, Protocol: "tcp",
		Process: "", Type: "host",
	}
	if len(fields) > 0 && strings.HasPrefix(fields[0], "udp") {
		si.Protocol = "udp"
	}
	if len(fields) >= 5 {
		si.Process = fields[len(fields)-1]
	}

	return si, true
}

func skipLineFilter(line string) bool {
	return line == "" ||
		strings.HasPrefix(line, "State") ||
		strings.HasPrefix(line, "Active") ||
		strings.HasPrefix(line, "Proto")
}

func portAlreadyExists(results []ServiceInventory, p int) bool {
	for _, existing := range results {
		if existing.Port == p {
			return true
		}
	}
	return false
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
