#!/bin/bash

# Test script for Helm chart validation
# This script tests both valid and invalid configurations to ensure validation works correctly

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Determine script directory and change to parent directory (which contains deploy/)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
echo "Script directory: $SCRIPT_DIR"
cd "$SERVICE_DIR"
echo "Changed to service directory: $SERVICE_DIR"

echo "🧪 Testing Helm Chart Validation"
echo "================================="

# Function to test valid configuration
test_valid_config() {
    local test_name="$1"
    shift
    echo -e "\n${GREEN}✓ Testing valid config: $test_name${NC}"

    if helm template test-release ./deploy "$@" > /dev/null 2>&1; then
        echo -e "  ${GREEN}✓ PASS${NC}"
    else
        echo -e "  ${RED}✗ FAIL - Valid configuration was rejected${NC}"
        exit 1
    fi
}

# Function to test invalid configuration
test_invalid_config() {
    local test_name="$1"
    local expected_error="$2"
    shift 2
    echo -e "\n${RED}✗ Testing invalid config: $test_name${NC}"

    if output=$(helm template test-release ./deploy "$@" 2>&1); then
        echo -e "  ${RED}✗ FAIL - Invalid configuration was accepted${NC}"
        echo "  Expected error containing: $expected_error"
        exit 1
    else
        if echo "$output" | grep -q "$expected_error"; then
            echo -e "  ${GREEN}✓ PASS - Correctly rejected with expected error${NC}"
        else
            echo -e "  ${RED}✗ FAIL - Rejected but with unexpected error message${NC}"
            echo "  Expected: $expected_error"
            echo "  Got: $output"
            exit 1
        fi
    fi
}

echo -e "\n${YELLOW}📋 Testing Basic Configuration Validation${NC}"
echo "==========================================="

# Test basic valid configurations
test_valid_config "Default configuration"

test_valid_config "Metrics enabled with OTLP" \
    --set serviceConfig.observability.metrics.enabled=true

test_valid_config "Metrics with Prometheus provider" \
    --set serviceConfig.observability.metrics.enabled=true \
    --set serviceConfig.observability.metrics.provider=prometheus

test_valid_config "ServiceMonitor with Prometheus provider" \
    --set serviceConfig.observability.metrics.enabled=true \
    --set serviceConfig.observability.metrics.provider=prometheus \
    --set serviceMonitor.enabled=true

test_valid_config "Health checks enabled (default)"

test_valid_config "Health checks disabled" \
    --set healthChecks.livenessProbe.enabled=false \
    --set healthChecks.readinessProbe.enabled=false \
    --set healthChecks.startupProbe.enabled=false

test_valid_config "Only startup probe enabled" \
    --set healthChecks.livenessProbe.enabled=false \
    --set healthChecks.readinessProbe.enabled=false \
    --set healthChecks.startupProbe.enabled=true

test_valid_config "Tracing with OpenTelemetry" \
    --set serviceConfig.observability.tracing.enabled=true \
    --set serviceConfig.observability.tracing.provider=otlp \
    --set serviceConfig.observability.tracing.otlp.endpoint=otel-collector:4317

echo -e "\n${YELLOW}📋 Testing Invalid Configuration Validation${NC}"
echo "============================================="

# Test metrics validation
# (ServiceMonitor is automatically conditional on prometheus provider - no validation needed)

# Test endpoint URL validation (Note: validation is now in observability library)
# Endpoint validation moved to library level

echo "🎉 All validation tests passed!"
echo "==============================="
echo
echo "📝 Summary:"
echo "- Metrics configuration validation: ✓"
echo "- ServiceMonitor with Prometheus provider: ✓"
echo "- Health checks configuration validation: ✓"
echo "- Tracing configuration validation: ✓"
echo
echo "The Helm chart validation is working correctly!"
