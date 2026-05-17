#!/usr/bin/env bash
set -euo pipefail

# generate_certs.sh
# Enhanced: generates a CA, a server certificate (with SANs) and per-node client
# certificates named tls_client_cert_node<N>.pem and tls_client_key_node<N>.pem.
# Writes files to ./secrets/ and adds secrets/ to .gitignore.
#
# Usage:
#   ./scripts/generate_certs.sh [--nodes N] [--keep-ca-key]
#
# Examples:
#   ./scripts/generate_certs.sh --nodes 3
#   ./scripts/generate_certs.sh --nodes 1 --keep-ca-key

OUTDIR="$(dirname "$0")/../secrets"
mkdir -p "$OUTDIR"
cd "$OUTDIR"

GITIGNORE="$(dirname "$0")/../.gitignore"
if ! grep -q "^secrets/" "$GITIGNORE" 2>/dev/null; then
  echo "secrets/" >> "$GITIGNORE"
  echo "Added secrets/ to .gitignore"
fi

# defaults
NODES=1
KEEP_CA_KEY=0

# parse args
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --nodes)
      NODES="$2"
      shift 2
      ;;
    --keep-ca-key)
      KEEP_CA_KEY=1
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--nodes N] [--keep-ca-key]"
      exit 0
      ;;
    *)
      echo "Unknown arg: $1"
      exit 1
      ;;
  esac
done

# validate NODES
if ! [[ "$NODES" =~ ^[0-9]+$ ]] || [ "$NODES" -lt 1 ]; then
  echo "--nodes must be a positive integer"
  exit 1
fi

# 1) Create CA (if missing)
if [ ! -f tls_ca_cert.pem ]; then
  echo "Generating CA (tls_ca_cert.pem) and private key (ca.key)..."
  openssl genrsa -out ca.key 4096
  openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 -subj "/CN=mpc-ca" -out tls_ca_cert.pem
  chmod 644 tls_ca_cert.pem
else
  echo "tls_ca_cert.pem already exists, skipping CA generation"
fi

# helper: openssl conf with SANs
write_openssl_conf() {
  cat > openssl_san.cnf <<EOF
[ req ]
default_bits       = 2048
prompt             = no
default_md         = sha256
req_extensions     = req_ext

[ req_distinguished_name ]
CN = $1

[ req_ext ]
subjectAltName = $2

[ v3_ext ]
subjectAltName = $2
EOF
}

# 2) Server cert (single server for PoC)
if [ ! -f tls_server_cert.pem ] || [ ! -f tls_server_key.pem ]; then
  echo "Generating server key and certificate..."
  SERVER_CN="mpc-server"
  SERVER_SANS="DNS:server-a,DNS:server-b,DNS:server-c,IP:127.0.0.1,DNS:localhost"
  write_openssl_conf "$SERVER_CN" "$SERVER_SANS"

  openssl genrsa -out tls_server_key.pem 2048
  openssl req -new -key tls_server_key.pem -out server.csr -config openssl_san.cnf -subj "/CN=$SERVER_CN"
  openssl x509 -req -in server.csr -CA tls_ca_cert.pem -CAkey ca.key -CAcreateserial -out tls_server_cert.pem -days 365 -extensions v3_ext -extfile openssl_san.cnf
  chmod 644 tls_server_cert.pem
  chmod 600 tls_server_key.pem
  rm -f server.csr openssl_san.cnf
else
  echo "Server cert/key already exist, skipping"
fi

# 3) Per-node client certs
for i in $(seq 0 $((NODES-1))); do
  CLIENT_CERT="tls_client_cert_node${i}.pem"
  CLIENT_KEY="tls_client_key_node${i}.pem"
  CLIENT_CN="mpc-client-node${i}"
  CLIENT_SANS="DNS:mpc-node-${i},IP:127.0.0.1"

  if [ -f "$CLIENT_CERT" ] && [ -f "$CLIENT_KEY" ]; then
    echo "Client cert/key for node $i already exist, skipping"
    continue
  fi

  echo "Generating client key and certificate for node $i..."
  write_openssl_conf "$CLIENT_CN" "$CLIENT_SANS"
  openssl genrsa -out "$CLIENT_KEY" 2048
  openssl req -new -key "$CLIENT_KEY" -out client.csr -config openssl_san.cnf -subj "/CN=$CLIENT_CN"
  openssl x509 -req -in client.csr -CA tls_ca_cert.pem -CAkey ca.key -CAcreateserial -out "$CLIENT_CERT" -days 365 -extensions v3_ext -extfile openssl_san.cnf
  chmod 644 "$CLIENT_CERT"
  chmod 600 "$CLIENT_KEY"
  rm -f client.csr openssl_san.cnf

  # also write a convenience alias for the first node to preserve backward compatibility
  if [ "$i" -eq 0 ]; then
    cp -f "$CLIENT_CERT" tls_client_cert.pem
    cp -f "$CLIENT_KEY" tls_client_key.pem
  fi
done

# Optionally remove CA key for safety (but keep CA cert)
if [ "$KEEP_CA_KEY" -eq 0 ]; then
  if [ -f ca.key ]; then
    echo "Removing CA private key (ca.key) for safety. Keep a secure backup elsewhere if needed."
    rm -f ca.key
  fi
fi

# Done
echo "Certificates generated in $OUTDIR"
ls -l "$OUTDIR"

echo "Notes:"
echo " - Generated $NODES client cert(s): tls_client_cert_node0..$((NODES-1)).pem"
echo " - tls_client_cert.pem / tls_client_key.pem are copies of node0's cert/key for compatibility"
echo " - Update docker-compose to mount the specific client cert for each mpc-node if you want unique per-container identities"
