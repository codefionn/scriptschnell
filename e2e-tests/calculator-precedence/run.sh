#!/bin/bash

set -e

cd "$(dirname "$0")"

# Source the E2E test library
source ../lib.sh

# Print test header
print_header "StatCode AI - Calculator CLI E2E Test" "OPERATOR PRECEDENCE UPGRADE SCENARIO"

# Initialize environment (API keys, workspace, Docker cleanup)
init_e2e_test "$@"

# Describe what the test does
echo "Starting operator precedence upgrade test..."
echo "This will:"
echo "  1. Build statcode-ai Docker container"
echo "  2. Provide statcode-ai with an existing calculator implementation"
echo "  3. Ask the AI to add proper operator precedence and parentheses support"
echo "  4. Require the AI to run tests and build the binary"
echo "  5. Validate the final calculator with an external test suite"
echo ""
echo "----------------------------------------"
echo ""

# Run the test and handle success/failure
run_e2e_test_flow "./workspace/calculator" "./workspace/calculator \"10 + 5 * 2\""
