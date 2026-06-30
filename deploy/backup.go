package deploy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	goss "golang.org/x/crypto/ssh"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/natuleadan/sdk-ops/ssh"
)

// BackupType represents what to backup.
type BackupType string

const (
	BackupTypeServices  BackupType = "services"
	BackupTypePostgres  BackupType = "postgres"
	BackupTypeMySQL     BackupType = "mysql"
	BackupTypeMongoDB   BackupType = "mongodb"
	BackupTypeRedis     BackupType = "redis" // RDB dump via SAVE
)

// S3Config holds S3-compatible storage settings.
type S3Config struct {
	Endpoint  string // e.g., https://s3.amazonaws.com or https://ewr.vultrcr.com
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	Path      string // optional prefix path
}

// BackupServices creates a tarball of all services on a node and downloads it.
func BackupServices(client *goss.Client, destDir string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("sdk-ops-backup-%s.tar.gz", timestamp)
	remotePath := fmt.Sprintf("/tmp/%s", filename)

	checkOut, _, _ := ssh.Run(client, `ls -A /opt/sdk-ops/services 2>/dev/null | head -1 || echo "empty"`)
	if strings.TrimSpace(checkOut) == "empty" {
		return "", fmt.Errorf("no services to backup")
	}

	script := fmt.Sprintf(`tar czf %s -C /opt/sdk-ops services/ 2>/dev/null && echo "ok" || echo "fail"`, remotePath)
	out, _, err := ssh.Run(client, script)
	if err != nil || strings.TrimSpace(out) != "ok" {
		return "", fmt.Errorf("backup create failed: %s", strings.TrimSpace(out))
	}

	catCmd := fmt.Sprintf("cat %s", remotePath)
	localPath := fmt.Sprintf("%s/%s", destDir, filename)

	outBytes, _, err := ssh.Run(client, catCmd)
	if err != nil {
		return "", fmt.Errorf("download backup: %w", err)
	}

	if err := os.WriteFile(localPath, []byte(outBytes), 0644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}

	ssh.Run(client, fmt.Sprintf("rm -f %s", remotePath))

	fmt.Printf("  → Backup saved: %s (%d bytes)\n", localPath, len(outBytes))
	return localPath, nil
}

// BackupDatabase dumps a database via pg_dump/mysqldump/mongodump inside its container.
// Returns the local path to the dump file.
func BackupDatabase(client *goss.Client, dbType DBType, dbName, containerName string, destDir string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("db-%s-%s-%s.sql.gz", containerName, dbType, timestamp)
	remotePath := fmt.Sprintf("/tmp/%s", filename)

	var dumpCmd string
	switch dbType {
	case DBPostgres:
		dumpCmd = fmt.Sprintf(
			`docker exec %s sh -c 'pg_dump -U postgres %s' | gzip > %s 2>/dev/null && echo "ok" || echo "fail"`,
			containerName, dbName, remotePath)
	case DBMySQL:
		dumpCmd = fmt.Sprintf(
			`docker exec %s sh -c 'mysqldump --all-databases' 2>/dev/null | gzip > %s 2>/dev/null && echo "ok" || echo "fail"`,
			containerName, remotePath)
	case DBMongoDB:
		dumpCmd = fmt.Sprintf(
			`docker exec %s sh -c 'mongodump --archive' 2>/dev/null | gzip > %s 2>/dev/null && echo "ok" || echo "fail"`,
			containerName, remotePath)
	case DBRedis:
		// Redis: trigger SAVE and copy the RDB
		dumpCmd = fmt.Sprintf(
			`docker exec %s sh -c 'echo "SAVE" | redis-cli && cp /data/dump.rdb /tmp/dump.rdb' 2>/dev/null && gzip -c /tmp/dump.rdb > %s && echo "ok" || echo "fail"`,
			containerName, remotePath)
	default:
		return "", fmt.Errorf("unsupported database type: %s", dbType)
	}

	out, _, err := ssh.Run(client, dumpCmd)
	if err != nil || strings.TrimSpace(out) != "ok" {
		return "", fmt.Errorf("database backup failed: %s", strings.TrimSpace(out))
	}

	// Download
	localPath := fmt.Sprintf("%s/%s", destDir, filename)
	catBytes, _, err := ssh.Run(client, fmt.Sprintf("cat %s", remotePath))
	if err != nil {
		return "", fmt.Errorf("download db backup: %w", err)
	}
	if err := os.WriteFile(localPath, []byte(catBytes), 0644); err != nil {
		return "", fmt.Errorf("write db backup: %w", err)
	}

	ssh.Run(client, fmt.Sprintf("rm -f %s", remotePath))
	fmt.Printf("  → Database backup saved: %s (%d bytes)\n", localPath, len(catBytes))
	return localPath, nil
}

// UploadToS3 uploads a local file to S3-compatible storage.
func UploadToS3(localPath string, cfg S3Config) error {
	ctx := context.Background()

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // needed for S3-compatible (MinIO, Vultr, etc.)
		}
	})

	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	fileInfo, _ := file.Stat()
	key := localPath
	if cfg.Path != "" {
		key = fmt.Sprintf("%s/%s", strings.TrimPrefix(cfg.Path, "/"), localPath)
	}

	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(cfg.Bucket),
		Key:           aws.String(key),
		Body:          file,
		ContentLength: aws.Int64(fileInfo.Size()),
	})
	if err != nil {
		return fmt.Errorf("s3 upload: %w", err)
	}

	fmt.Printf("  → Uploaded to s3://%s/%s\n", cfg.Bucket, key)
	return nil
}

// ScheduleBackup installs a systemd timer on the remote node that runs a backup script on a cron schedule.
func ScheduleBackup(client *goss.Client, backupType BackupType, dbName, containerName, cronExpr string, s3Cfg *S3Config) error {
	unitName := fmt.Sprintf("sdk-ops-backup-%s", backupType)
	timestamp := time.Now().Format("20060102-150405")

	var backupCmd string
	switch backupType {
	case BackupTypeServices:
		backupCmd = fmt.Sprintf(`tar czf /tmp/sdk-ops-backup-%s.tar.gz -C /opt/sdk-ops services/`, timestamp)
	case BackupTypePostgres:
		backupCmd = fmt.Sprintf(`docker exec %s pg_dump -U postgres %s | gzip > /tmp/db-%s-%s.sql.gz`, containerName, dbName, containerName, timestamp)
	case BackupTypeMySQL:
		backupCmd = fmt.Sprintf(`docker exec %s mysqldump --all-databases | gzip > /tmp/db-%s-%s.sql.gz`, containerName, containerName, timestamp)
	case BackupTypeMongoDB:
		backupCmd = fmt.Sprintf(`docker exec %s mongodump --archive | gzip > /tmp/db-%s-%s.archive.gz`, containerName, containerName, timestamp)
	case BackupTypeRedis:
		backupCmd = fmt.Sprintf(`docker exec %s sh -c 'redis-cli SAVE && cp /data/dump.rdb /tmp/dump.rdb' && gzip -c /tmp/dump.rdb > /tmp/db-%s-%s.rdb.gz`, containerName, containerName, timestamp)
	}

	// Cleanup old backups (keep last 7 by default)
	cleanupCmd := fmt.Sprintf(`ls -t /tmp/sdk-ops-backup-*.tar.gz /tmp/db-*.sql.gz /tmp/db-*.archive.gz /tmp/db-*.rdb.gz 2>/dev/null | tail -n +8 | xargs -r rm -f`)

	// Parse cron expression into OnCalendar format
	cal := cronToSystemdCalendar(cronExpr)

	serviceContent := fmt.Sprintf(`[Unit]
Description=sdk-ops backup - %s
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
ExecStart=/bin/bash -c '%s && %s'
`, unitName, backupCmd, cleanupCmd)

	timerContent := fmt.Sprintf(`[Unit]
Description=sdk-ops backup timer - %s

[Timer]
OnCalendar=%s
Persistent=true

[Install]
WantedBy=timers.target
`, unitName, cal)

	installScript := fmt.Sprintf(`
mkdir -p /opt/sdk-ops/backups
cat > /etc/systemd/system/%s.service << 'SERVICEEOF'
%s
SERVICEEOF
cat > /etc/systemd/system/%s.timer << 'TIMEREOF'
%s
TIMEREOF
systemctl daemon-reload
systemctl enable --now %s.timer 2>/dev/null
systemctl restart %s.timer 2>/dev/null
echo "ok"
`, unitName, serviceContent, unitName, timerContent, unitName, unitName)

	out, _, err := ssh.Run(client, installScript)
	if err != nil {
		return fmt.Errorf("schedule install: %w", err)
	}
	if !strings.Contains(out, "ok") {
		return fmt.Errorf("schedule install failed: %s", strings.TrimSpace(out))
	}

	fmt.Printf("  → Backup scheduled: %s (%s)\n", unitName, cronExpr)
	fmt.Printf("  → Systemd timer: %s.timer\n", unitName)
	return nil
}

// UnscheduleBackup removes a systemd timer and service.
func UnscheduleBackup(client *goss.Client, backupType BackupType) error {
	unitName := fmt.Sprintf("sdk-ops-backup-%s", backupType)
	script := fmt.Sprintf(`
systemctl stop %s.timer 2>/dev/null || true
systemctl disable %s.timer 2>/dev/null || true
rm -f /etc/systemd/system/%s.service /etc/systemd/system/%s.timer
systemctl daemon-reload
echo "ok"
`, unitName, unitName, unitName, unitName)

	out, _, err := ssh.Run(client, script)
	if err != nil || !strings.Contains(out, "ok") {
		return fmt.Errorf("unschedule: %w", err)
	}
	fmt.Printf("  → Backup schedule removed: %s\n", unitName)
	return nil
}

// ListBackupSchedules lists active systemd backup timers.
func ListBackupSchedules(client *goss.Client) ([]string, error) {
	out, _, err := ssh.Run(client, `systemctl list-timers --all --no-pager 2>/dev/null | grep "sdk-ops-backup" || echo ""`)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// S3ConfigFromEnv populates S3Config from environment variables.
func S3ConfigFromEnv() S3Config {
	return S3Config{
		Endpoint:  os.Getenv("SDK_OPS_S3_ENDPOINT"),
		Region:    os.Getenv("SDK_OPS_S3_REGION"),
		Bucket:    os.Getenv("SDK_OPS_S3_BUCKET"),
		AccessKey: os.Getenv("SDK_OPS_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("SDK_OPS_S3_SECRET_KEY"),
		Path:      os.Getenv("SDK_OPS_S3_PATH"),
	}
}

func RestoreServices(client *goss.Client, backupPath string) error {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer session.Close()

	restoreCmd := `sudo tar xzf - -C / && echo "restore_ok"`
	session.Stdin = bytes.NewReader(data)
	out, err := session.CombinedOutput(restoreCmd)
	if err != nil {
		return fmt.Errorf("restore failed: %w\n%s", err, string(out))
	}

	ssh.Run(client, `for d in /opt/sdk-ops/services/*/; do
		[ -d "$d/current" ] && (cd "$d/current" && docker compose up -d 2>/dev/null || true)
	done`)

	fmt.Printf("  → Services restored from %s\n", backupPath)
	return nil
}

// cronToSystemdCalendar converts a crontab expression to systemd OnCalendar format.
func cronToSystemdCalendar(expr string) string {
	parts := strings.Fields(expr)
	if len(parts) < 5 {
		return "daily"
	}

	minute := parts[0]
	hour := parts[1]
	day := parts[2]
	month := parts[3]
	weekday := parts[4]

	// Handle step expressions: "*/30" → "0/30", "*/2" → "0/2"
	if strings.HasPrefix(minute, "*/") {
		minute = "0/" + minute[2:]
	}
	if strings.HasPrefix(hour, "*/") {
		hour = "0/" + hour[2:]
	}

	// Pad single digits
	if len(minute) == 1 && minute != "*" {
		minute = "0" + minute
	}
	if len(hour) == 1 && hour != "*" {
		hour = "0" + hour
	}

	if day == "*" {
		day = "*"
	}
	if month == "*" {
		month = "*"
	}
	if weekday == "*" {
		weekday = "*"
	}

	return fmt.Sprintf("%s-%s-%s %s:%s:00", weekday, month, day, hour, minute)
}

// NewBackupCmd stuff
var _ = (*goss.Client)(nil)
