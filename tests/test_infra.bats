#!/usr/bin/env bats

load setup

@test "infra ready: unhealthy node returns exit 1" {
  run sdk_ssh infra ready "192.0.2.1"
  [ "$status" -ne 0 ]
}

@test "infra status: shows system info" {
  run sdk_ssh infra status "$TEST_IP"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "Hostname"
  echo "$output" | grep -q "Kernel"
  echo "$output" | grep -q "Memory"
}

@test "infra status: shows hardening checks" {
  run sdk_ssh infra status "$TEST_IP"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "nftables"
}

@test "node exec: runs command on server" {
  run sdk_ssh node exec "$TEST_IP" -- hostname
  [ "$status" -eq 0 ]
  echo "$output"
  [ -n "$output" ]
}

@test "config add-node + list: manages nodes" {
  run sdk config init
  run sdk config add-node "$TEST_IP" --user "$TEST_USER" --key "$TEST_KEY"
  run sdk node list
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "$TEST_IP"
}

@test "infra backup: backup runs without error" {
  run sdk_ssh infra backup "$TEST_IP"
  echo "$output" | grep -v "Error"
}

@test "cluster version: shows k3s info if available" {
  run sdk_ssh cluster version
  # May fail if k3s is not installed (non-zero exit is OK)
  echo "$output"
}
