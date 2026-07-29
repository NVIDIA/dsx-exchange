#!/usr/bin/env bash
# Copyright 2026 NVIDIA Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

set -Eeuo pipefail
umask 077

REPO_ROOT="$(git rev-parse --show-toplevel)"
GENERATOR_DIR="$REPO_ROOT/local/agent-gateway/secret-generators/demoidp"
OUTPUT_DIR="$GENERATOR_DIR/generated"
TEMPLATE="$GENERATOR_DIR/credentials.yaml.tmpl"
TEMP_DIR=""

ISSUERS=(
  human-oidc
  service-oidc
  svid-issuer
  svid-wrong-key
  unconfigured-issuer
)

cleanup() {
  [[ -z "$TEMP_DIR" || ! -d "$TEMP_DIR" ]] || rm -rf "$TEMP_DIR"
}

sources_ready() {
  local issuer
  for issuer in "${ISSUERS[@]}"; do
    [[ -s "$OUTPUT_DIR/$issuer/config.json" ]] || return 1
    [[ -s "$OUTPUT_DIR/$issuer/tls.key" ]] || return 1
    [[ "$OUTPUT_DIR/$issuer/config.json" -nt "$TEMPLATE" ]] || return 1
  done
}

generate_sources() {
  local client_secret operator viewer tenant_a tenant_b tenant_c
  local tenant_test tenant_test_b bad_svid
  local human_key service_key svid_key wrong_key unconfigured_key
  local rendered issuer name signing_key

  client_secret="$(openssl rand -hex 32)"
  operator="$(openssl rand -hex 32)"
  viewer="$(openssl rand -hex 32)"
  tenant_a="$(openssl rand -hex 32)"
  tenant_b="$(openssl rand -hex 32)"
  tenant_c="$(openssl rand -hex 32)"
  tenant_test="$(openssl rand -hex 32)"
  tenant_test_b="$(openssl rand -hex 32)"
  bad_svid="$(openssl rand -hex 32)"
  human_key="$(openssl genrsa 4096 2>/dev/null | base64 | tr -d '\n')"
  service_key="$(openssl genrsa 4096 2>/dev/null | base64 | tr -d '\n')"
  svid_key="$(openssl genrsa 4096 2>/dev/null | base64 | tr -d '\n')"
  wrong_key="$(openssl genrsa 4096 2>/dev/null | base64 | tr -d '\n')"
  unconfigured_key="$(openssl genrsa 4096 2>/dev/null | base64 | tr -d '\n')"
  rendered="$TEMP_DIR/credentials.yaml"

  DEMOIDP_CLIENT_SECRET="$client_secret" \
  DEMOIDP_OPERATOR_PASSWORD="$operator" \
  DEMOIDP_VIEWER_PASSWORD="$viewer" \
  DEMOIDP_TENANT_A_PASSWORD="$tenant_a" \
  DEMOIDP_TENANT_B_PASSWORD="$tenant_b" \
  DEMOIDP_TENANT_C_PASSWORD="$tenant_c" \
  DEMOIDP_TENANT_TEST_PASSWORD="$tenant_test" \
  DEMOIDP_TENANT_TEST_B_PASSWORD="$tenant_test_b" \
  DEMOIDP_BAD_SVID_PASSWORD="$bad_svid" \
  DEMOIDP_HUMAN_OIDC_SIGNING_KEY_BASE64="$human_key" \
  DEMOIDP_SERVICE_OIDC_SIGNING_KEY_BASE64="$service_key" \
  DEMOIDP_SVID_SIGNING_KEY_BASE64="$svid_key" \
  DEMOIDP_SVID_WRONG_KEY_SIGNING_KEY_BASE64="$wrong_key" \
  DEMOIDP_UNCONFIGURED_SIGNING_KEY_BASE64="$unconfigured_key" \
    awk '
    { gsub(/<<DEMOIDP_CLIENT_SECRET>>/, ENVIRON["DEMOIDP_CLIENT_SECRET"]);
      gsub(/<<DEMOIDP_OPERATOR_PASSWORD>>/, ENVIRON["DEMOIDP_OPERATOR_PASSWORD"]);
      gsub(/<<DEMOIDP_VIEWER_PASSWORD>>/, ENVIRON["DEMOIDP_VIEWER_PASSWORD"]);
      gsub(/<<DEMOIDP_TENANT_A_PASSWORD>>/, ENVIRON["DEMOIDP_TENANT_A_PASSWORD"]);
      gsub(/<<DEMOIDP_TENANT_B_PASSWORD>>/, ENVIRON["DEMOIDP_TENANT_B_PASSWORD"]);
      gsub(/<<DEMOIDP_TENANT_C_PASSWORD>>/, ENVIRON["DEMOIDP_TENANT_C_PASSWORD"]);
      gsub(/<<DEMOIDP_TENANT_TEST_PASSWORD>>/, ENVIRON["DEMOIDP_TENANT_TEST_PASSWORD"]);
      gsub(/<<DEMOIDP_TENANT_TEST_B_PASSWORD>>/, ENVIRON["DEMOIDP_TENANT_TEST_B_PASSWORD"]);
      gsub(/<<DEMOIDP_BAD_SVID_PASSWORD>>/, ENVIRON["DEMOIDP_BAD_SVID_PASSWORD"]);
      gsub(/<<DEMOIDP_HUMAN_OIDC_SIGNING_KEY_BASE64>>/,
        ENVIRON["DEMOIDP_HUMAN_OIDC_SIGNING_KEY_BASE64"]);
      gsub(/<<DEMOIDP_SERVICE_OIDC_SIGNING_KEY_BASE64>>/,
        ENVIRON["DEMOIDP_SERVICE_OIDC_SIGNING_KEY_BASE64"]);
      gsub(/<<DEMOIDP_SVID_SIGNING_KEY_BASE64>>/,
        ENVIRON["DEMOIDP_SVID_SIGNING_KEY_BASE64"]);
      gsub(/<<DEMOIDP_SVID_WRONG_KEY_SIGNING_KEY_BASE64>>/,
        ENVIRON["DEMOIDP_SVID_WRONG_KEY_SIGNING_KEY_BASE64"]);
      gsub(/<<DEMOIDP_UNCONFIGURED_SIGNING_KEY_BASE64>>/,
        ENVIRON["DEMOIDP_UNCONFIGURED_SIGNING_KEY_BASE64"]);
      print }
  ' "$TEMPLATE" >"$rendered"

  while IFS= read -r issuer; do
    name="$(jq -r '.name' <<<"$issuer")"
    signing_key="$(jq -r '.signingKeyBase64' <<<"$issuer")"
    mkdir -p "$TEMP_DIR/$name"
    jq -c '{
      issuer,
      kid: (.kid // .name),
      ttlSeconds: (.ttlSeconds // 3600),
      clients,
      users: (.users // [])
    }' <<<"$issuer" >"$TEMP_DIR/$name/config.json"
    printf '%s' "$signing_key" |
      openssl base64 -d -A >"$TEMP_DIR/$name/tls.key"
  done < <(yq eval -o=json -I=0 '.issuers[]' "$rendered")

  rm -f "$rendered"
}

main() {
  sources_ready && exit 0

  mkdir -p "$(dirname "$OUTPUT_DIR")"
  TEMP_DIR="$(mktemp -d "$(dirname "$OUTPUT_DIR")/.generated.XXXXXX")"
  generate_sources
  rm -rf "$OUTPUT_DIR"
  mv "$TEMP_DIR" "$OUTPUT_DIR"
  TEMP_DIR=""
}

trap cleanup EXIT
trap 'exit 1' INT TERM
main "$@"
