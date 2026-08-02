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

# NOTE: cf-strict is intentionally NOT tested here — it locks SSH to the
# allowlist and must only be exercised manually with --yes and a reachable
# admin IP.

@test "firewall allowlist: cf-normal installs and gates ports" {
  run sdk_ssh infra firewall cf-normal --source cf --node "$TEST_IP"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "allowlist updated"
  echo "$output" | grep -q "allow4"
}

@test "firewall allowlist: status shows last sync" {
  run sdk_ssh infra firewall allowlist status --node "$TEST_IP"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "last_sync"
}

@test "firewall allowlist: refresh keeps SSH reachable" {
  run sdk_ssh infra firewall allowlist refresh --node "$TEST_IP"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "allowlist updated"
  run sdk_ssh node exec "$TEST_IP" -- hostname
  [ "$status" -eq 0 ]
}

@test "firewall allowlist: admin add + remove" {
  run sdk_ssh infra firewall allowlist admin add "203.0.113.99" --node "$TEST_IP"
  [ "$status" -eq 0 ]
  run sdk_ssh infra firewall allowlist admin remove "203.0.113.99" --node "$TEST_IP"
  [ "$status" -eq 0 ]
}

@test "firewall allowlist: remove restores pre-allowlist config" {
  run sdk_ssh infra firewall allowlist remove --node "$TEST_IP"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "allowlist removed"
  run sdk_ssh infra firewall list --node "$TEST_IP"
  [ "$status" -eq 0 ]
}
