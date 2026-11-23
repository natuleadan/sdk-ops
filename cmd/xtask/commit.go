package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var (
	headerRe = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)\([a-z0-9][a-z0-9._-]*\): ([a-z0-9][^\n]*)$`)
	bulletRe = regexp.MustCompile(`^- \*\*[^*]+\*\*: (.+)$`)
)

func runCommitCheck(root string, args []string) error {
	var msg string
	if len(args) >= 2 && args[0] == "--file" {
		if err := checkGitIdentity(root); err != nil {
			return err
		}
		raw, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		msg = string(raw)
	} else if len(args) >= 2 && args[0] == "--range" {
		cmd := exec.Command("git", "log", "--format=%B%x00", args[1])
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			return err
		}
		for _, entry := range strings.Split(string(out), "\x00") {
			trimmed := strings.TrimSpace(entry)
			if trimmed == "" || strings.HasPrefix(trimmed, "Merge ") {
				continue
			}
			if err := validateCommitText(trimmed); err != nil {
				return err
			}
		}
		return nil
	} else {
		raw, err := os.ReadFile(".git/COMMIT_EDITMSG")
		if err != nil {
			return fmt.Errorf("missing --file or --range: %v", err)
		}
		msg = string(raw)
	}
	return validateCommitText(msg)
}

func validateCommitText(raw string) error {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "#") {
			lines = append(lines, strings.TrimRight(line, "\r"))
		}
	}
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	parts := strings.Split(text, "\n")
	if len(parts) < 5 {
		return fmt.Errorf("commit requires header, blank line, and at least 3 bullets")
	}
	header := parts[0]
	if len(header) > 100 {
		return fmt.Errorf("commit header exceeds 100 chars")
	}
	m := headerRe.FindStringSubmatch(header)
	if m == nil {
		return fmt.Errorf("invalid conventional commit header: %q", header)
	}
	desc := m[2]
	if strings.HasSuffix(desc, ".") || desc != strings.ToLower(desc) {
		return fmt.Errorf("commit description must be lowercase without trailing period")
	}
	words := strings.Fields(desc)
	if len(words) < 3 || len(words) > 6 {
		return fmt.Errorf("commit description must be 3 to 6 words (got %d)", len(words))
	}
	if strings.TrimSpace(parts[1]) != "" {
		return fmt.Errorf("second line must be blank")
	}
	bullets := 0
	for _, line := range parts[2:] {
		if strings.TrimSpace(line) == "" {
			return fmt.Errorf("blank lines between bullets are forbidden")
		}
		if strings.Contains(strings.ToLower(line), "co-authored-by:") {
			return fmt.Errorf("Co-authored-by trailers are forbidden")
		}
		bm := bulletRe.FindStringSubmatch(line)
		if bm == nil {
			return fmt.Errorf("invalid bullet format: %q", line)
		}
		bw := strings.Fields(bm[1])
		if len(bw) < 3 || len(bw) > 10 {
			return fmt.Errorf("bullet text must be 3 to 10 words: %q", line)
		}
		bullets++
	}
	if bullets < 3 {
		return fmt.Errorf("at least 3 bullets required (got %d)", bullets)
	}
	return nil
}

// NOTE: optionally pin your organization noreply email domain or handle.
const noreplySuffix = "@users.noreply.github.com"

func checkGitIdentity(root string) error {
	nameCmd := exec.Command("git", "config", "user.name")
	nameCmd.Dir = root
	nameOut, err := nameCmd.Output()
	if err != nil {
		return fmt.Errorf("git user.name not configured: %v", err)
	}
	if strings.TrimSpace(string(nameOut)) == "" {
		return fmt.Errorf("git user.name must be configured")
	}
	emailCmd := exec.Command("git", "config", "user.email")
	emailCmd.Dir = root
	emailOut, err := emailCmd.Output()
	if err != nil {
		return fmt.Errorf("git user.email not configured: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(emailOut)), noreplySuffix) {
		return fmt.Errorf("git user.email must end with %q", noreplySuffix)
	}
	return nil
}
