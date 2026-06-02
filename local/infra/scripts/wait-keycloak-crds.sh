#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

context="${1:?usage: wait-keycloak-crds.sh <kube-context>}"

kubectl wait \
  --for=condition=Established \
  crd/keycloaks.k8s.keycloak.org \
  crd/keycloakrealmimports.k8s.keycloak.org \
  --timeout=60s \
  --context "${context}"

for resource in keycloaks.k8s.keycloak.org keycloakrealmimports.k8s.keycloak.org; do
  ready=false
  for i in {1..60}; do
    if kubectl get "${resource}" -n keycloak --context "${context}" >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 1
  done

  if [ "${ready}" != true ]; then
    echo "ERROR: Keycloak CRD storage is not ready for ${resource} in ${context}" >&2
    exit 1
  fi
done
