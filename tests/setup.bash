# Common setup for all integration tests
export TEST_IP="${TEST_IP:-152.53.169.115}"
export TEST_USER="${TEST_USER:-root}"
export TEST_KEY="${TEST_KEY:-/Users/nla/Documents/nla-go/_data/claves/test-key}"
export SDK_OPS="${SDK_OPS:-$(pwd)/sdk-ops}"

setup() {
  if [ ! -x "$SDK_OPS" ]; then
    echo "ERROR: sdk-ops binary not found at $SDK_OPS" >&2
    return 1
  fi
}

# Run a command that does NOT need SSH
sdk() {
  "$SDK_OPS" "$@" 2>&1
}

# Run a command that needs SSH (inserts SSH flags before -- separator)
sdk_ssh() {
  local args=()
  local sep_found=false
  for arg in "$@"; do
    if [ "$arg" = "--" ]; then
      args+=(--user "$TEST_USER" --key "$TEST_KEY" --insecure)
      sep_found=true
    fi
    args+=("$arg")
  done
  if ! $sep_found; then
    args+=(--user "$TEST_USER" --key "$TEST_KEY" --insecure)
  fi
  "$SDK_OPS" "${args[@]}" 2>&1
}

ssh_cmd() {
  ssh -i "$TEST_KEY" -o StrictHostKeyChecking=no "$TEST_USER@$TEST_IP" "$@" 2>&1
}
