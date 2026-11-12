#!/usr/bin/env bash
# ==============================================================================
# SSLE Project 1: TLS / mTLS Certificate Generator (Practical Guide 5 - Threat 3)
# Generates Root CA, Server Certificates, Client Certificates, and Rogue MitM CA
# ==============================================================================
set -euo pipefail

CERT_DIR="./certs"
mkdir -p "$CERT_DIR"
cd "$CERT_DIR"

echo "=== [1/4] Generating Trusted Root Certificate Authority (CA) ==="
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 365 \
  -subj "/C=PT/ST=Coimbra/L=Coimbra/O=SSLE-University/OU=Cybersecurity/CN=SSLE-Root-CA" \
  -out ca.crt

echo "=== [2/4] Generating Ingestion Server Certificate (Signed by CA) ==="
openssl genrsa -out server.key 2048

cat > server_ext.cnf <<EOF
[req]
default_bits = 2048
prompt = no
default_md = sha256
req_extensions = req_ext
distinguished_name = dn

[dn]
C = PT
ST = Coimbra
L = Coimbra
O = SSLE-University
OU = Ingestion
CN = ssle-ingestion.local

[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = ssle-ingestion.local
DNS.2 = localhost
DNS.3 = ssle-ingestion
IP.1 = 127.0.0.1
IP.2 = 10.20.2.10
EOF

openssl req -new -key server.key -out server.csr -config server_ext.cnf
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 365 -sha256 -extfile server_ext.cnf -extensions req_ext

echo "=== [3/4] Generating Client Certificate for Mutual TLS (mTLS) ==="
openssl genrsa -out client.key 2048
openssl req -new -key client.key -out client.csr \
  -subj "/C=PT/ST=Coimbra/L=Coimbra/O=SSLE-University/OU=EdgeSensors/CN=sensor-temperature-edge"
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days 365 -sha256

echo "=== [4/4] Generating Rogue / Fake MitM CA (Simulating Untrusted Interceptor) ==="
openssl genrsa -out rogue_ca.key 2048
openssl req -x509 -new -nodes -key rogue_ca.key -sha256 -days 30 \
  -subj "/C=XX/ST=AttackerLand/L=MitM/O=RogueProxy/CN=Untrusted-Fake-CA" \
  -out rogue_ca.crt

echo ""
echo "=== Certificate Generation Complete ==="
echo "Files created in $(pwd):"
ls -la "$PWD"
