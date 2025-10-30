#!/bin/bash

set -e

cd "$(dirname "$0")"

# Source the E2E test library
source ../lib.sh

# Print test header
print_header "StatCode AI - Calculator CLI E2E Test"

# Initialize environment (API keys, workspace, Docker cleanup)
init_e2e_test "$@"

# Describe what the test does
echo "Starting E2E test..."
echo "This will:"
echo "  1. Build statcode-ai Docker container"
echo "  2. Use statcode-ai to generate a calculator CLI"
echo "  3. Test the calculator with mathematical expressions"
echo ""
echo "----------------------------------------"
echo ""

# Run the test and handle success/failure
run_e2e_test_flow "./workspace/calculator" "./workspace/calculator \"8 * 10 + 2\""
