package deploy

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type DBType string

const (
	DBPostgres DBType = "postgres"
	DBMySQL    DBType = "mysql"
	DBRedis    DBType = "redis"
	DBMongoDB  DBType = "mongodb"
)

type DBConfig struct {
	Type    DBType
	Name    string
	Version string
	Port    int  // 0 = no external port (internal only)
	Network string
	User    string
	Pass    string
}

type DBResult struct {
	ConnString    string
	ContainerName string
	Image         string
	InternalPort  int
	ExposedPort   int
	User          string
	Pass          string
	Database      string
}

func ProvisionDatabase(client *goss.Client, cfg DBConfig) (*DBResult, error) {
	if cfg.Name == "" {
		cfg.Name = string(cfg.Type)
	}
	if cfg.User == "" {
		cfg.User = cfg.Name
	}
	if cfg.Pass == "" {
		pass, err := genPassword()
		if err != nil {
			return nil, fmt.Errorf("generate password: %w", err)
		}
		cfg.Pass = pass
	}
	if cfg.Version == "" {
		cfg.Version = latestVersion(cfg.Type)
	}

	containerName := fmt.Sprintf("sdk-db-%s", cfg.Name)

	// Pull image silently
	ssh.Run(client, fmt.Sprintf("docker pull %s:%s 2>/dev/null || true", imageName(cfg.Type), cfg.Version))

	var result DBResult

	switch cfg.Type {
	case DBPostgres:
		result = provisionPostgres(client, cfg, containerName)
	case DBMySQL:
		result = provisionMySQL(client, cfg, containerName)
	case DBRedis:
		result = provisionRedis(client, cfg, containerName)
	case DBMongoDB:
		result = provisionMongoDB(client, cfg, containerName)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Type)
	}

	return &result, nil
}

func provisionPostgres(client *goss.Client, cfg DBConfig, containerName string) DBResult {
	dbName := strings.ReplaceAll(cfg.Name, "-", "_")
	exposedPort := cfg.Port

	img := fmt.Sprintf("%s:%s", imageName(cfg.Type), cfg.Version)
	runCmd := fmt.Sprintf("docker rm -f %s 2>/dev/null && docker run -d --name %s -e POSTGRES_USER=%s -e POSTGRES_PASSWORD=%s -e POSTGRES_DB=%s --restart unless-stopped %s %s",
		containerName, containerName, cfg.User, cfg.Pass, dbName,
		portFlag(cfg.Port, 5432),
		img)

	ssh.Run(client, runCmd)

	connStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s", cfg.User, cfg.Pass, containerName, dbName)
	if exposedPort > 0 {
		connStr = fmt.Sprintf("postgres://%s:%s@localhost:%d/%s", cfg.User, cfg.Pass, exposedPort, dbName)
	}

	return DBResult{
		ConnString: connStr, ContainerName: containerName, Image: img,
		InternalPort: 5432, ExposedPort: exposedPort,
		User: cfg.User, Pass: cfg.Pass, Database: dbName,
	}
}

func provisionMySQL(client *goss.Client, cfg DBConfig, containerName string) DBResult {
	img := fmt.Sprintf("%s:%s", imageName(cfg.Type), cfg.Version)
	runCmd := fmt.Sprintf("docker rm -f %s 2>/dev/null && docker run -d --name %s -e MYSQL_ROOT_PASSWORD=%s -e MYSQL_USER=%s -e MYSQL_PASSWORD=%s -e MYSQL_DATABASE=%s --restart unless-stopped %s %s",
		containerName, containerName, cfg.Pass, cfg.User, cfg.Pass, cfg.Name,
		portFlag(cfg.Port, 3306),
		img)

	ssh.Run(client, runCmd)

	connStr := fmt.Sprintf("mysql://%s:%s@%s:3306/%s", cfg.User, cfg.Pass, containerName, cfg.Name)
	if cfg.Port > 0 {
		connStr = fmt.Sprintf("mysql://%s:%s@localhost:%d/%s", cfg.User, cfg.Pass, cfg.Port, cfg.Name)
	}
	return DBResult{
		ConnString: connStr, ContainerName: containerName, Image: img,
		InternalPort: 3306, ExposedPort: cfg.Port,
		User: cfg.User, Pass: cfg.Pass, Database: cfg.Name,
	}
}

func provisionRedis(client *goss.Client, cfg DBConfig, containerName string) DBResult {
	img := fmt.Sprintf("%s:%s", imageName(cfg.Type), cfg.Version)
	passFlag := ""
	if cfg.Pass != "" {
		passFlag = fmt.Sprintf("redis-server --requirepass %s", cfg.Pass)
	}

	runCmd := fmt.Sprintf("docker rm -f %s 2>/dev/null && docker run -d --name %s --restart unless-stopped %s %s %s",
		containerName, containerName,
		portFlag(cfg.Port, 6379),
		img, passFlag)

	ssh.Run(client, runCmd)

	connStr := fmt.Sprintf("redis://:%s@%s:6379/0", cfg.Pass, containerName)
	if cfg.Port > 0 {
		connStr = fmt.Sprintf("redis://:%s@localhost:%d/0", cfg.Pass, cfg.Port)
	}

	return DBResult{
		ConnString: connStr, ContainerName: containerName, Image: img,
		InternalPort: 6379, ExposedPort: cfg.Port,
		Pass: cfg.Pass,
	}
}

func provisionMongoDB(client *goss.Client, cfg DBConfig, containerName string) DBResult {
	img := fmt.Sprintf("%s:%s", imageName(cfg.Type), cfg.Version)
	runCmd := fmt.Sprintf("docker rm -f %s 2>/dev/null && docker run -d --name %s -e MONGO_INITDB_ROOT_USERNAME=%s -e MONGO_INITDB_ROOT_PASSWORD=%s -e MONGO_INITDB_DATABASE=%s --restart unless-stopped %s %s",
		containerName, containerName, cfg.User, cfg.Pass, cfg.Name,
		portFlag(cfg.Port, 27017),
		img)

	ssh.Run(client, runCmd)

	connStr := fmt.Sprintf("mongodb://%s:%s@%s:27017/%s", cfg.User, cfg.Pass, containerName, cfg.Name)
	if cfg.Port > 0 {
		connStr = fmt.Sprintf("mongodb://%s:%s@localhost:%d/%s", cfg.User, cfg.Pass, cfg.Port, cfg.Name)
	}

	return DBResult{
		ConnString: connStr, ContainerName: containerName, Image: img,
		InternalPort: 27017, ExposedPort: cfg.Port,
		User: cfg.User, Pass: cfg.Pass, Database: cfg.Name,
	}
}

func RemoveDatabase(client *goss.Client, name string) error {
	containerName := fmt.Sprintf("sdk-db-%s", name)
	script := fmt.Sprintf(`docker rm -f %s 2>/dev/null && echo "removed" || echo "not-found"`, containerName)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("remove database: %w", err)
	}
	if strings.Contains(out, "not-found") {
		return fmt.Errorf("database %s not found", name)
	}
	return nil
}

func ListDatabases(client *goss.Client) ([]string, error) {
	out, _, err := ssh.Run(client, `docker ps --format '{{.Names}}' --filter name=sdk-db- 2>/dev/null || true`)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func imageName(dbType DBType) string {
	switch dbType {
	case DBPostgres:
		return "postgres"
	case DBMySQL:
		return "mysql"
	case DBRedis:
		return "redis"
	case DBMongoDB:
		return "mongo"
	default:
		return string(dbType)
	}
}

func latestVersion(dbType DBType) string {
	switch dbType {
	case DBPostgres:
		return "17-alpine"
	case DBMySQL:
		return "8.0"
	case DBRedis:
		return "7-alpine"
	case DBMongoDB:
		return "7"
	default:
		return "latest"
	}
}

func portFlag(port, internal int) string {
	if port > 0 {
		return fmt.Sprintf("-p %d:%d", port, internal)
	}
	return ""
}

func genPassword() (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, 24)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		result[i] = chars[n.Int64()]
	}
	return string(result), nil
}
