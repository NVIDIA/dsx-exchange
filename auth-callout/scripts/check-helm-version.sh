#!/bin/bash
set -e

# Get all staged files under deploy/
DEPLOY_FILES=$(git diff --cached --name-only --diff-filter=ACM | grep '^deploy/' || true)

# If no deploy files are staged, exit successfully
if [ -z "$DEPLOY_FILES" ]; then
    exit 0
fi

echo "✓ Found changes in deploy/ directory"

# Check if Chart.yaml has version changes in staged content
CHART_FILES=$(echo "$DEPLOY_FILES" | grep 'Chart.yaml$' || true)

if [ -z "$CHART_FILES" ]; then
    echo "✗ Error: Changes detected in deploy/ but Chart.yaml not modified"
    echo "  Please bump the 'version' field in Chart.yaml"
    exit 1
fi

# Check if the version field actually changed
VERSION_CHANGED=$(git diff --cached -- $CHART_FILES | grep '^[+-]version:' || true)

if [ -z "$VERSION_CHANGED" ]; then
    echo "✗ Error: Chart.yaml modified but version field not changed"
    echo "  Please bump the 'version' field in Chart.yaml"
    exit 1
fi

echo "✓ Chart version has been updated"
exit 0
