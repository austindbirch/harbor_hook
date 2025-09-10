#!/bin/bash

set -e

# Generate CA private key (RSA format)
openssl genrsa -out ca.key 4096

# Generate CA certificate
openssl req -new -x509 -days 365 -key ca.key -out ca.crt \
  -subj "/C=US/ST=CA/L=San Francisco/O=HarborHook/CN=HarborHook CA"

# Generate server private key (RSA format) 
openssl genrsa -out server.key 4096

# Generate server certificate signing request
openssl req -new -key server.key -out server.csr \
  -subj "/C=US/ST=CA/L=San Francisco/O=HarborHook/CN=localhost"

# Create SAN config file
cat > server_san.conf <<EOF
[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = envoy
DNS.3 = *.local
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

# Generate server certificate with SAN
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 365 \
  -extensions v3_req -extfile server_san.conf

# Generate client private key (RSA format)
openssl genrsa -out client.key 4096

# Generate client certificate signing request
openssl req -new -key client.key -out client.csr \
  -subj "/C=US/ST=CA/L=San Francisco/O=HarborHook/CN=HarborHook Client"

# Generate client certificate
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days 365

# Clean up files
rm -f server.csr client.csr server_san.conf

# Verify the certificates
echo "Verifying certificates..."
openssl x509 -in server.crt -text -noout | grep -A 5 "Subject Alternative Name" || true
openssl rsa -in server.key -check -noout
echo "Certificates generated successfully!"

echo "Certificates generated successfully!"
echo "CA: ca.crt"
echo "Server: server.crt, server.key"
echo "Client: client.crt, client.key"
