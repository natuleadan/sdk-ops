#!/bin/sh
# kv-full-bm gen-certs — generate TLS certificates for HAProxy + Dragonfly
set -e

TLS_DIR="${TLS_DIR:-./ssl}"
mkdir -p "$TLS_DIR"

echo "=== kv-full-bm gen-certs ==="

if [ -f "$TLS_DIR/server.key" ]; then
  echo "  Certs already exist in $TLS_DIR"
  exit 0
fi

echo "Generating CA..."
openssl ecparam -genkey -name prime256v1 -noout -out "$TLS_DIR/ca.key" 2>/dev/null
openssl req -x509 -new -nodes -key "$TLS_DIR/ca.key" -sha256 -days 365 \
  -out "$TLS_DIR/ca.crt" -subj "/CN=DragonflyCA" 2>/dev/null

echo "Generating server cert..."
openssl ecparam -genkey -name prime256v1 -noout -out "$TLS_DIR/server.key" 2>/dev/null
openssl req -new -key "$TLS_DIR/server.key" -out "$TLS_DIR/server.csr" \
  -subj "/CN=dragonfly" 2>/dev/null
openssl x509 -req -in "$TLS_DIR/server.csr" -CA "$TLS_DIR/ca.crt" \
  -CAkey "$TLS_DIR/ca.key" -CAcreateserial \
  -out "$TLS_DIR/server.crt" -days 365 -sha256 2>/dev/null

echo "Generating replica cert..."
openssl ecparam -genkey -name prime256v1 -noout -out "$TLS_DIR/replica.key" 2>/dev/null
openssl req -new -key "$TLS_DIR/replica.key" -out "$TLS_DIR/replica.csr" \
  -subj "/CN=dragonfly-replica" 2>/dev/null
openssl x509 -req -in "$TLS_DIR/replica.csr" -CA "$TLS_DIR/ca.crt" \
  -CAkey "$TLS_DIR/ca.key" -CAcreateserial \
  -out "$TLS_DIR/replica.crt" -days 365 -sha256 2>/dev/null

# Concatenate cert+key into PEM for HAProxy
cat "$TLS_DIR/server.crt" "$TLS_DIR/server.key" > "$TLS_DIR/server.pem"
cat "$TLS_DIR/replica.crt" "$TLS_DIR/replica.key" > "$TLS_DIR/replica.pem"

rm -f "$TLS_DIR/"*.csr "$TLS_DIR/"*.srl
cp "$TLS_DIR/ca.crt" "$TLS_DIR/root.crt"
chmod 600 "$TLS_DIR/"*.key
chmod 644 "$TLS_DIR/"*.pem "$TLS_DIR/"*.crt

echo "  PEM files for HAProxy generated"
echo "✓ TLS certs in $TLS_DIR/"
