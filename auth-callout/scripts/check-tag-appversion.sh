#!/bin/bash
# Script to validate that a tag matches appVersion in Chart.yaml

set -e

# Set DEBUG=true to enable debug output
DEBUG="${DEBUG:-false}"

# Get the repository root directory
REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$REPO_ROOT" || exit 1

# Get tag from first argument
TAG="$1"

if [ -z "$TAG" ]; then
    echo "✗ Error: Tag argument is required"
    echo "  Usage: $0 <tag>"
    exit 1
fi

[ "$DEBUG" = "true" ] && echo "[DEBUG] Checking tag: '$TAG'"

# Find Chart.yaml in deploy/ directory
CHART_FILE="deploy/Chart.yaml"

if [ ! -f "$CHART_FILE" ]; then
    echo "✗ Error: Chart.yaml not found at $CHART_FILE"
    exit 1
fi

# Extract appVersion from Chart.yaml
APP_VERSION=$(grep '^appVersion:' "$CHART_FILE" | sed -E "s/^appVersion:[[:space:]]*[\"']?([^\"']+)[\"']?/\1/" || true)

[ "$DEBUG" = "true" ] && echo "[DEBUG] Extracted APP_VERSION: '$APP_VERSION'"

if [ -z "$APP_VERSION" ]; then
    echo "✗ Error: Could not extract appVersion from Chart.yaml"
    exit 1
fi

# Compare tag to appVersion
[ "$DEBUG" = "true" ] && echo "[DEBUG] Comparing TAG ('$TAG') == APP_VERSION ('$APP_VERSION')"

if [ "$TAG" != "$APP_VERSION" ]; then
    echo "✗ Error: appVersion '$APP_VERSION' in Chart.yaml does not match tag: '$TAG'"
    echo "  Please update appVersion in Chart.yaml to match the tag, or use a matching tag name"
    exit 1
else
    [ "$DEBUG" = "true" ] && echo "[DEBUG] Validation PASSED: tag matches appVersion"
    echo "✓ appVersion '$APP_VERSION' matches tag: '$TAG'"
fi

exit 0
