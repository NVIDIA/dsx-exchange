#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CERTS_DIR="${SCRIPT_DIR}/certs"

command -v cfssl >/dev/null 2>&1 || { echo "ERROR: cfssl is required (brew install cfssl)" >&2; exit 1; }
command -v cfssljson >/dev/null 2>&1 || { echo "ERROR: cfssljson is required (brew install cfssl)" >&2; exit 1; }

clusters=(csc cpc-1 cpc-2)

echo "Generating mTLS certificates for all clusters..."

for cluster in "${clusters[@]}"; do
  cluster_dir="${CERTS_DIR}/${cluster}"
  mkdir -p "${cluster_dir}"

  echo "Generating certificates for ${cluster}..."

  # CA configuration
  cat > "${cluster_dir}/ca-config.json" <<EOF
{
  "signing": {
    "default": {
      "expiry": "87600h"
    },
    "profiles": {
      "server": {
        "usages": ["signing", "key encipherment", "server auth"],
        "expiry": "87600h"
      },
      "client": {
        "usages": ["signing", "key encipherment", "client auth"],
        "expiry": "87600h"
      }
    }
  }
}
EOF

  # CA certificate request
  cat > "${cluster_dir}/ca-csr.json" <<EOF
{
  "CN": "NATS mTLS CA ${cluster}",
  "key": {
    "algo": "rsa",
    "size": 2048
  },
  "names": [
    {
      "C": "US",
      "ST": "California",
      "L": "Local",
      "O": "DSX Event Bus",
      "OU": "mTLS ${cluster}"
    }
  ]
}
EOF

  # Generate CA certificate
  cd "${cluster_dir}"
  cfssl gencert -initca ca-csr.json | cfssljson -bare ca

  # Determine LoadBalancer IP for this cluster
  case "${cluster}" in
    csc) LB_IP="172.18.200.1" ;;
    cpc-1) LB_IP="172.18.201.1" ;;
    cpc-2) LB_IP="172.18.202.1" ;;
    *) LB_IP="127.0.0.1" ;;
  esac

  # Server certificate request
  cat > server-csr.json <<EOF
{
  "CN": "nats-mtls.event-bus.${cluster}.svc.cluster.local",
  "key": {
    "algo": "rsa",
    "size": 2048
  },
  "names": [
    {
      "C": "US",
      "ST": "California",
      "L": "Local",
      "O": "DSX Event Bus",
      "OU": "mTLS Server ${cluster}"
    }
  ],
  "hosts": [
    "nats-mtls",
    "nats-mtls.event-bus",
    "nats-mtls.event-bus.svc",
    "nats-mtls.event-bus.svc.cluster.local",
    "localhost",
    "127.0.0.1",
    "${LB_IP}"
  ]
}
EOF

  # Generate server certificate
  cfssl gencert \
    -ca=ca.pem \
    -ca-key=ca-key.pem \
    -config=ca-config.json \
    -profile=server \
    server-csr.json | cfssljson -bare server

  # Client certificate request
  cat > client-csr.json <<EOF
{
  "CN": "mqtt-client.${cluster}",
  "key": {
    "algo": "rsa",
    "size": 2048
  },
  "names": [
    {
      "C": "US",
      "ST": "California",
      "L": "Local",
      "O": "DSX Event Bus",
      "OU": "mTLS Client ${cluster}"
    }
  ]
}
EOF

  # Generate client certificate
  cfssl gencert \
    -ca=ca.pem \
    -ca-key=ca-key.pem \
    -config=ca-config.json \
    -profile=client \
    client-csr.json | cfssljson -bare client

  # Clean up CSR and config files
  rm -f *.csr *.json

  echo "Certificates generated for ${cluster}:"
  echo "  CA: ${cluster_dir}/ca.pem"
  echo "  Server: ${cluster_dir}/server.pem"
  echo "  Client: ${cluster_dir}/client.pem"
done

echo ""
echo "mTLS certificate generation complete"
echo "Certificates stored in: ${CERTS_DIR}"

