#!/bin/bash
# Helper script to get NATS keys for devspace variables
# Generates keys on first access if devspace.env doesn't exist

set -e

KEY_NAME="$1"
ENV_FILE="devspace.env"

# Generate keys if file doesn't exist or is empty
if [ ! -f "$ENV_FILE" ] || [ ! -s "$ENV_FILE" ]; then
  # Check if nsc is available
  if ! command -v nsc &> /dev/null; then
    echo "ERROR: nsc is required but not installed. Install with: go install github.com/nats-io/nsc/v2@latest" >&2
    exit 1
  fi

  # Generate auth user nkey
  AUTH_USER_OUTPUT=$(nsc generate nkey --user 2>&1)
  AUTH_USER_NKEY_SEED=$(echo "$AUTH_USER_OUTPUT" | grep -E "^SU" | head -1)
  AUTH_USER_NKEY=$(echo "$AUTH_USER_OUTPUT" | grep -E "^U" | head -1)

  # Generate signing key
  SIGNING_OUTPUT=$(nsc generate nkey --account 2>&1)
  AUTH_SIGNING_KEY_SEED=$(echo "$SIGNING_OUTPUT" | grep -E "^SA" | head -1)
  AUTH_SIGNING_KEY=$(echo "$SIGNING_OUTPUT" | grep -E "^A" | head -1)

  # Generate xkey
  XKEY_OUTPUT=$(nsc generate nkey --curve 2>&1)
  XKEY_SEED=$(echo "$XKEY_OUTPUT" | grep -E "^SX" | head -1)
  XKEY_PUBKEY=$(echo "$XKEY_OUTPUT" | grep -E "^X" | head -1)

  # Write to devspace.env (gitignored)
  cat > "$ENV_FILE" << EOF
# Auto-generated NATS keys - DO NOT COMMIT
# Generated at $(date)
AUTH_USER_NKEY_SEED=${AUTH_USER_NKEY_SEED}
AUTH_USER_NKEY=${AUTH_USER_NKEY}
AUTH_SIGNING_KEY_SEED=${AUTH_SIGNING_KEY_SEED}
AUTH_SIGNING_KEY=${AUTH_SIGNING_KEY}
XKEY_SEED=${XKEY_SEED}
XKEY_PUBKEY=${XKEY_PUBKEY}
EOF

  echo "Generated NATS keys in $ENV_FILE" >&2
fi

# Return the requested key value
grep -E "^${KEY_NAME}=" "$ENV_FILE" | cut -d= -f2
