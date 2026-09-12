#!/bin/bash
# nats-bare gen-certs - self-serve TLS material (native install layout).
# Generates a private CA + one server cert per node + client certs
# (app/sys/svc) so the template is self-contained (no pre-made assets needed).
# The server SANs must cover how the node is reached:
#   the node name, the advertise IP (peer_ip or public host) and 127.0.0.1
#   (intra-VPS loopback clients).
# init.sh invokes this only when $DIR/certs has no server cert yet; an operator
# can also run it by hand with their own SANs (gen-certs.sh skips existing files).
#
# Usage (env):
#   GEN_NODES="<name>|<san1>,<san2>  <name>|<san1>,<san2>"  (each row = one node)
#   GEN_CA_DAYS / GEN_CERT_DAYS (defaults 3650 / 825)
#   GEN_OUT (default $DIR/certs when run by init.sh, else ./certs)
# Output: $GEN_OUT/ca.pem ca.key server/<name>.pem|.key client/<u>-cert.pem|key
set -eu

DIR="${NATS_DIR:-/opt/sdk-ops/services/nats-bare}"
OUT="${GEN_OUT:-$DIR/certs}"
mkdir -p "$OUT/server" "$OUT/client"
CA_DAYS="${GEN_CA_DAYS:-3650}"
CERT_DAYS="${GEN_CERT_DAYS:-825}"

if [ ! -f "$OUT/ca.pem" ]; then
  openssl req -x509 -newkey rsa:2048 -keyout "$OUT/ca.key" -out "$OUT/ca.pem" \
    -days "$CA_DAYS" -nodes -subj "/CN=nats-bare-ca" >/dev/null 2>&1
fi

for row in ${GEN_NODES:-}; do
  name="${row%%|*}"
  sans="${row#*|}"
  [ -z "$name" ] && continue
  [ -z "$sans" ] && sans="DNS:$name,IP:127.0.0.1"
  if [ ! -f "$OUT/server/$name.pem" ]; then
    openssl req -newkey rsa:2048 -keyout "$OUT/server/$name.key" -out "/tmp/$name.csr" \
      -nodes -subj "/CN=$name" >/dev/null 2>&1
    printf 'subjectAltName=%s\n' "$sans" > "/tmp/$name.san"
    openssl x509 -req -in "/tmp/$name.csr" -CA "$OUT/ca.pem" -CAkey "$OUT/ca.key" -CAcreateserial \
      -out "$OUT/server/$name.pem" -days "$CERT_DAYS" -sha256 -extfile "/tmp/$name.san" >/dev/null 2>&1
    rm -f "/tmp/$name.csr" "/tmp/$name.san"
  fi
done

for u in app sys svc; do
  if [ ! -f "$OUT/client/$u-cert.pem" ]; then
    openssl req -newkey rsa:2048 -keyout "$OUT/client/$u-key.pem" -out "/tmp/$u.csr" \
      -nodes -subj "/CN=$u" >/dev/null 2>&1
    openssl x509 -req -in "/tmp/$u.csr" -CA "$OUT/ca.pem" -CAkey "$OUT/ca.key" -CAcreateserial \
      -out "$OUT/client/$u-cert.pem" -days "$CERT_DAYS" -sha256 >/dev/null 2>&1
    rm -f "/tmp/$u.csr"
  fi
done

echo "certs generated in $OUT/"
