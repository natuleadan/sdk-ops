#!/bin/bash
# etcd-bare validate - native member + full-cluster health (run by the 5-min
# timer or the `validate` command). No Docker: systemctl + host etcdctl.
# No hardcoded IPs: the local member answers on 127.0.0.1:2379 and the full
# member list (client URLs) comes from the cluster itself (`member list`);
# the fallback when the member list is unreachable parses the rendered
# etcd.conf.yml (initial-cluster peer :2380 -> client :2379).
set -u

DIR="${ETCD_BARE_DIR:-/opt/sdk-ops/services/etcd-bare}"
CONF="$DIR/etcd.conf.yml"
CTL="${ETCDCTL:-/usr/local/bin/etcdctl}"
LOCAL_EP="http://127.0.0.1:2379"
export ETCDCTL_API=3

FAILED=0
ok()   { echo "  [PASS] $*"; }
bad()  { echo "  [FAIL] $*"; FAILED=1; }

ectld() { "$CTL" --command-timeout=6s "$@"; }

echo "[etcd-validate] $DIR"

# 1. systemd unit active (the dockerized check was the container).
if systemctl is-active --quiet etcd 2>/dev/null; then
  ok "systemd unit etcd active"
else
  bad "systemd unit etcd not active"
fi

# 2. local member health (hard requirement - without it there is nothing to ask).
if ectld --endpoints="$LOCAL_EP" endpoint health 2>/dev/null | grep -q healthy; then
  ok "local endpoint healthy ($LOCAL_EP)"
else
  bad "local endpoint unhealthy ($LOCAL_EP)"
  echo "[etcd-validate] FAILED"
  exit 1
fi

# 3. member list - the expected count comes from the local etcd.conf.yml
#    (entries of initial-cluster), never from a hardcoded 3.
MEMBER_JSON=$(ectld --endpoints="$LOCAL_EP" member list -w json 2>/dev/null || true)
EXPECTED=0
if [ -f "$CONF" ]; then
  IC=$(sed -n 's/^initial-cluster:[[:space:]]*//p' "$CONF" | head -1 | tr -d '"' | tr -d "'")
  EXPECTED=$(printf '%s' "$IC" | tr ',' '\n' | grep -c . || true)
fi
if [ -n "$MEMBER_JSON" ]; then
  MEMBER_COUNT=$(printf '%s' "$MEMBER_JSON" | grep -oE '"name":"[^"]+"' | wc -l | tr -d ' ')
  if [ "$EXPECTED" -gt 0 ] && [ "$MEMBER_COUNT" != "$EXPECTED" ]; then
    bad "member list: $MEMBER_COUNT member(s), expected $EXPECTED"
  else
    ok "member list: $MEMBER_COUNT member(s)"
  fi
else
  bad "member list unreadable"
fi

# 4. health of EVERY member - client URLs from member list, never hardcoded.
CLIENT_EPS=$(printf '%s' "$MEMBER_JSON" | grep -oE '"clientURLs":\[[^]]*\]' | grep -oE 'https?://[^"]+' | paste -sd, -)
if [ -n "$CLIENT_EPS" ]; then
  OUT=$(ectld --endpoints="$CLIENT_EPS" endpoint health 2>&1 || true)
  HEALTHY=$(printf '%s\n' "$OUT" | grep -c ' is healthy' || true)
  TOTAL=$(printf '%s' "$CLIENT_EPS" | tr ',' '\n' | grep -c . || true)
  if [ "$HEALTHY" -eq "$TOTAL" ]; then
    ok "all members healthy ($HEALTHY/$TOTAL)"
  else
    bad "member health degraded ($HEALTHY/$TOTAL)"
  fi
else
  # Fallback: parse initial-cluster from the local conf (peers use :2380,
  # clients :2379 in this topology).
  FALLBACK_EPS=$(printf '%s' "${IC:-}" | tr ',' '\n' | sed -n 's/.*=//p' | sed 's/:2380$/:2379/' | paste -sd, -)
  if [ -n "$FALLBACK_EPS" ]; then
    OUT=$(ectld --endpoints="$FALLBACK_EPS" endpoint health 2>&1 || true)
    HEALTHY=$(printf '%s\n' "$OUT" | grep -c ' is healthy' || true)
    TOTAL=$(printf '%s' "$FALLBACK_EPS" | tr ',' '\n' | grep -c . || true)
    if [ "$HEALTHY" -eq "$TOTAL" ]; then
      ok "all members healthy per conf ($HEALTHY/$TOTAL)"
    else
      bad "member health degraded per conf ($HEALTHY/$TOTAL)"
    fi
  else
    bad "no member endpoints found (conf + cluster)"
  fi
fi

if [ "$FAILED" -ne 0 ]; then echo "[etcd-validate] FAILED"; exit 1; fi
echo "[etcd-validate] OK"
exit 0
