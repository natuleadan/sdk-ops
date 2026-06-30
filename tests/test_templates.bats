#!/usr/bin/env bats

load setup

setup_file() {
  export TEST_DIR=$(mktemp -d)
}

teardown_file() {
  rm -rf "$TEST_DIR"
}

@test "deploy init: html template" {
  run sdk deploy init "$TEST_DIR/html" --template html
  [ "$status" -eq 0 ]
  [ -f "$TEST_DIR/html/docker-compose.yml" ]
  [ -f "$TEST_DIR/html/nginx.conf" ]
  [ -f "$TEST_DIR/html/index.html" ]
  grep -q "SDK Ops" "$TEST_DIR/html/index.html"
}

@test "deploy init: node template" {
  run sdk deploy init "$TEST_DIR/node" --template node
  [ "$status" -eq 0 ]
  [ -f "$TEST_DIR/node/package.json" ]
  [ -f "$TEST_DIR/node/server.js" ]
  [ -f "$TEST_DIR/node/Dockerfile" ]
}

@test "deploy init: go template" {
  run sdk deploy init "$TEST_DIR/go" --template go
  [ "$status" -eq 0 ]
  [ -f "$TEST_DIR/go/main.go" ]
  [ -f "$TEST_DIR/go/go.mod" ]
  [ -f "$TEST_DIR/go/Dockerfile" ]
}

@test "deploy init: wordpress template" {
  run sdk deploy init "$TEST_DIR/wp" --template wordpress
  [ "$status" -eq 0 ]
  [ -f "$TEST_DIR/wp/docker-compose.yml" ]
  [ -f "$TEST_DIR/wp/service.yaml" ]
  grep -q "wordpress" "$TEST_DIR/wp/service.yaml"
}

@test "deploy init: unknown template fails" {
  run sdk deploy init "$TEST_DIR/x" --template nonexistent
  [ "$status" -ne 0 ]
}

@test "deploy init: list templates" {
  run sdk deploy init "$TEST_DIR/x"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "html"
  echo "$output" | grep -q "node"
  echo "$output" | grep -q "go"
}
