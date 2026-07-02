package deploy

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

func RotateDBPassword(client *goss.Client, dbType DBType, containerName, newPass string) (string, error) {
	if newPass == "" {
		var err error
		newPass, err = genPassword()
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
	}

	var cmd string
	switch dbType {
	case DBPostgres:
		cmd = fmt.Sprintf(
			`DB_USER=$(docker exec %s printenv POSTGRES_USER 2>/dev/null || echo postgres) && for i in 1 2 3; do echo "SELECT 1;" | docker exec -i %s psql -h localhost -U "$DB_USER" 2>/dev/null && break; sleep 2; done && PASS='%s' && echo "ALTER USER \"$DB_USER\" WITH PASSWORD '"'"'$PASS'"'"';" | docker exec -i %s psql -h localhost -U "$DB_USER" 2>&1`,
			containerName, containerName, newPass, containerName)
	case DBMySQL:
		cmd = fmt.Sprintf(
			`ROOT_PASS=$(docker exec %s printenv MYSQL_ROOT_PASSWORD 2>/dev/null) && DB_USER=$(docker exec %s printenv MYSQL_USER 2>/dev/null || echo root) && echo "ALTER USER '$DB_USER'@'%%' IDENTIFIED BY '%s'; FLUSH PRIVILEGES;" | docker exec -i %s mysql -u root -p"$ROOT_PASS" 2>&1`,
			containerName, containerName, newPass, containerName)
	case DBMongoDB:
		cmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet --eval 'db.changeUserPassword("%s", "%s")' 2>&1`,
			containerName, containerName, newPass)
	case DBRedis:
		cmd = fmt.Sprintf(
			`docker exec %s redis-cli CONFIG SET requirepass '%s' 2>&1`,
			containerName, newPass)
	default:
		return "", fmt.Errorf("unsupported database type: %s", dbType)
	}

	out, _, err := ssh.Run(client, cmd)
	if err != nil {
		return "", fmt.Errorf("rotate password on %s: %w\n%s", containerName, err, strings.TrimSpace(out))
	}

	return newPass, nil
}

func RotateServiceEnv(client *goss.Client, serviceName, key, newValue string) error {
	script := fmt.Sprintf(`
SERVICE_DIR="/opt/sdk-ops/services/%s/current"
if [ ! -d "$SERVICE_DIR" ]; then
  echo "error: service directory not found"
  exit 1
fi
`, serviceName)

	if newValue == "" {
		newValue = randomEnvValue(32)
	}

	// Update env in docker-compose.yml or run.sh
	script += fmt.Sprintf(`
# Update env in docker-compose.yml if exists
COMPOSE_FILE="$SERVICE_DIR/docker-compose.yml"
if [ -f "$COMPOSE_FILE" ]; then
  if grep -q '%s:' "$COMPOSE_FILE" 2>/dev/null; then
    sed -i 's|%s=.*|%s=%s|g' "$COMPOSE_FILE"
    echo "Updated env in docker-compose.yml"
  else
    echo "ok: key not found in compose (maybe set at runtime)"
  fi
fi

# Update env in service.yaml if exists
SVC_YAML="$SERVICE_DIR/service.yaml"
if [ -f "$SVC_YAML" ]; then
  if grep -q '%s:' "$SVC_YAML" 2>/dev/null; then
    sed -i 's|%s:.*|%s: %s|g' "$SVC_YAML"
    echo "Updated env in service.yaml"
  fi
fi

# Restart service
if docker ps --format='{{.Names}}' 2>/dev/null | grep -q '%s'; then
  COMPOSE_DIR=$(dirname "$COMPOSE_FILE")
  cd "$COMPOSE_DIR" && docker compose up -d --remove-orphans 2>&1 | tail -1
fi

echo "done"
`, key, key, key, newValue, key, key, key, newValue, serviceName)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("rotate env: %w\n%s", err, strings.TrimSpace(out))
	}
	fmt.Printf("  %s\n", strings.TrimSpace(out))
	return nil
}

func randomEnvValue(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}
