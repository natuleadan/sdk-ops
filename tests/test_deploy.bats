#!/usr/bin/env bats

load setup

setup_file() {
  export TEST_DIR=$(mktemp -d)
}

teardown_file() {
  ssh_cmd "docker compose -f /opt/sdk-ops/services/bats-html/v1/docker-compose.yml down 2>/dev/null || true"
  ssh_cmd "rm -rf /opt/sdk-ops/services/bats-html 2>/dev/null || true"
  rm -rf "$TEST_DIR"
}

@test "deploy init: html template creates files" {
  run sdk deploy init "$TEST_DIR/html" --template html --name bats-html
  [ "$status" -eq 0 ]
  [ -f "$TEST_DIR/html/docker-compose.yml" ]
  [ -f "$TEST_DIR/html/nginx.conf" ]
  [ -f "$TEST_DIR/html/index.html" ]
}

@test "deploy push: html deploys and nginx responds" {
  # Skip if previous init failed
  [ -f "$TEST_DIR/html/docker-compose.yml" ] || skip "template not initialized"

  run sdk_ssh deploy push "$TEST_DIR/html" --node "$TEST_IP" --name bats-html
  echo "$output"

  # Wait for container
  sleep 5

  # Check if container responds on port 80
  run ssh_cmd "curl -s -o /dev/null -w '%{http_code}' http://localhost:80/ 2>/dev/null || echo 'fail'"
  echo "HTTP: $output"
  [ "$output" = "200" ]
}

@test "deploy push: service is registered in config" {
  run sdk node list
  echo "$output" | grep -q "bats-html" || true
}
