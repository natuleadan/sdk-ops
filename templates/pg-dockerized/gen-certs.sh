#!/bin/sh
# pg-dockerized gen-certs — generate self-signed SSL certificates
set -e

mkdir -p ssl
echo "Generating SSL certificates..."
openssl req -x509 -newkey rsa:4096 -keyout ssl/server.key -out ssl/server.crt \
  -days 365 -nodes -subj "/CN=postgres" \
  -addext "subjectAltName = DNS:postgres,DNS:pgdog,DNS:pg-replica,DNS:pg-replica-2" 2>/dev/null
cp ssl/server.crt ssl/root.crt
echo "  SSL certs generated in ssl/"
