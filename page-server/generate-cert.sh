#!/bin/bash

# Generate self-signed TLS certificate for Page Server
# For production, use certificates from a proper CA

set -e

CERT_DIR="${1:-./certs}"
CERT_FILE="${CERT_DIR}/server.crt"
KEY_FILE="${CERT_DIR}/server.key"

echo "Generating self-signed TLS certificate..."
echo "Certificate directory: $CERT_DIR"

mkdir -p "$CERT_DIR"

# Generate private key and certificate
openssl req -x509 -newkey rsa:4096 \
    -keyout "$KEY_FILE" \
    -out "$CERT_FILE" \
    -days 365 \
    -nodes \
    -subj "/CN=localhost/O=Page Server/C=US" \
    -addext "subjectAltName=DNS:localhost,DNS:*.localhost,IP:127.0.0.1"

echo ""
echo "✅ Certificate generated successfully!"
echo ""
echo "Certificate: $CERT_FILE"
echo "Private Key: $KEY_FILE"
echo ""
echo "To use with Page Server:"
echo "  ./page-server -tls -tls-cert $CERT_FILE -tls-key $KEY_FILE"
echo ""
echo "⚠️  WARNING: This is a self-signed certificate for testing only!"
echo "   For production, use certificates from a proper Certificate Authority."

