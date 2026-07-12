#!/bin/sh
# libsql-dockerized gen-certs — generate TLS certificates for HAProxy
set -e

TLS_DIR="${TLS_DIR:-./tls}"
mkdir -p "$TLS_DIR"

echo "=== libsql-dockerized gen-certs ==="

if [ -f "$TLS_DIR/server.key" ]; then
  echo "  Certs already exist in $TLS_DIR"
  exit 0
fi

echo "Generating CA..."
openssl ecparam -genkey -name prime256v1 -noout -out "$TLS_DIR/ca_key.pem" 2>/dev/null
openssl req -x509 -new -nodes -key "$TLS_DIR/ca_key.pem" -sha256 -days 365 \
  -out "$TLS_DIR/ca_cert.pem" -subj "/CN=libsqlCA" 2>/dev/null

echo "Generating server cert..."
openssl ecparam -genkey -name prime256v1 -noout -out "$TLS_DIR/server_key.pem" 2>/dev/null
openssl req -new -key "$TLS_DIR/server_key.pem" -out "$TLS_DIR/server.csr" \
  -subj "/CN=sqld-primary" 2>/dev/null
openssl x509 -req -in "$TLS_DIR/server.csr" -CA "$TLS_DIR/ca_cert.pem" \
  -CAkey "$TLS_DIR/ca_key.pem" -CAcreateserial \
  -out "$TLS_DIR/server_cert.pem" -days 365 -sha256 2>/dev/null

echo "Generating replica cert..."
openssl ecparam -genkey -name prime256v1 -noout -out "$TLS_DIR/client_key.pem" 2>/dev/null
openssl req -new -key "$TLS_DIR/client_key.pem" -out "$TLS_DIR/client.csr" \
  -subj "/CN=sqld-replica" 2>/dev/null
openssl x509 -req -in "$TLS_DIR/client.csr" -CA "$TLS_DIR/ca_cert.pem" \
  -CAkey "$TLS_DIR/ca_key.pem" -CAcreateserial \
  -out "$TLS_DIR/client_cert.pem" -days 365 -sha256 2>/dev/null

# Concatenate cert+key into PEM for HAProxy
cat "$TLS_DIR/server_cert.pem" "$TLS_DIR/server_key.pem" > "$TLS_DIR/server.pem"
cat "$TLS_DIR/client_cert.pem" "$TLS_DIR/client_key.pem" > "$TLS_DIR/replica.pem"

rm -f "$TLS_DIR/"*.csr "$TLS_DIR/"*.srl
chmod 600 "$TLS_DIR/"*_key.pem
chmod 644 "$TLS_DIR/"*.pem "$TLS_DIR/"*_cert.pem "$TLS_DIR/"ca_cert.pem

echo "  PEM files for HAProxy generated"
echo "✓ TLS certs in $TLS_DIR/"
