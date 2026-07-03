package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	Prerelease  bool   `json:"prerelease"`
}

type VersionInfo struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	UpdateAvail bool   `json:"update_available"`
	ReleaseURL  string `json:"release_url"`
	CheckedAt   string `json:"checked_at"`
}

var (
	lastCheck     time.Time
	cachedLatest  string
	cachedRelease string
	ghOwner       = "natuleadan"
	ghRepo        = "sdk-ops"
)

func checkForUpdate() (*VersionInfo, error) {
	now := time.Now()
	info := &VersionInfo{
		Current:   version,
		CheckedAt: now.Format(time.RFC3339),
	}

	// Use cache if checked within last hour
	if time.Since(lastCheck) < time.Hour && cachedLatest != "" {
		info.Latest = cachedLatest
		info.ReleaseURL = cachedRelease
		info.UpdateAvail = isNewerVersion(cachedLatest, version)
		return info, nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", ghOwner, ghRepo)
	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return info, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "sdk-ops-agent/"+version)

	resp, err := client.Do(req)
	if err != nil {
		return info, fmt.Errorf("github api: %w", err)
	}
	defer func() { if err := resp.Body.Close(); err != nil { log.Printf("update: resp body close error: %v", err) } }()

	if resp.StatusCode == 404 {
		// No releases yet
		info.Latest = version
		info.UpdateAvail = false
		return info, nil
	}
	if resp.StatusCode != 200 {
		return info, fmt.Errorf("github api: HTTP %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return info, fmt.Errorf("decode: %w", err)
	}

	tag := strings.TrimPrefix(release.TagName, "v")
	cachedLatest = tag
	cachedRelease = release.HTMLURL
	lastCheck = now

	info.Latest = tag
	info.ReleaseURL = release.HTMLURL
	info.UpdateAvail = isNewerVersion(tag, version)
	return info, nil
}

func isNewerVersion(latest, current string) bool {
	if latest == "" || current == "" || latest == current {
		return false
	}
	// Strip 'v' prefix
	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")

	// Simple semver comparison
	lParts := strings.Split(latest, ".")
	cParts := strings.Split(current, ".")

	for i := range 3 {
		var lv, cv int
		if i < len(lParts) {
			if _, err := fmt.Sscanf(lParts[i], "%d", &lv); err != nil { log.Printf("update: parse version error: %v", err) }
		}
		if i < len(cParts) {
			if _, err := fmt.Sscanf(cParts[i], "%d", &cv); err != nil { log.Printf("update: parse version error: %v", err) }
		}
		if lv > cv {
			return true
		}
		if lv < cv {
			return false
		}
	}
	return false
}

func autoUpdateCheck(cfg AgentConfig) {
	if cfg.AutoUpdate == "" || cfg.AutoUpdate == "false" || cfg.AutoUpdate == "0" {
		return
	}

	log.Printf("update: checking for updates (current: %s)", version)
	info, err := checkForUpdate()
	if err != nil {
		log.Printf("update: check failed: %v", err)
		return
	}

	if info.UpdateAvail {
		log.Printf("update: new version available: %s (current: %s)", info.Latest, version)
		log.Printf("update: %s", info.ReleaseURL)
	} else {
		log.Printf("update: up to date (%s)", version)
	}
}
