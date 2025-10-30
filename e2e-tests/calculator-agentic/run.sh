#!/bin/bash

set -e

cd "$(dirname "$0")"

# Source the E2E test library
source ../lib.sh

# Print test header
print_header "StatCode AI - Calculator CLI E2E Test" "AGENTIC VERSION - Full Autonomy Mode"

# Initialize environment (API keys, workspace, Docker cleanup)
init_e2e_test "$@"

# Describe what the test does
echo "Starting AGENTIC E2E test..."
echo "This will:"
echo "  1. Build statcode-ai Docker container"
echo "  2. Give AI full autonomy to design and build calculator"
echo "  3. AI writes and runs its own tests"
echo "  4. AI iterates until all tests pass"
echo "  5. Validate final calculator with test suite"
echo ""
echo "----------------------------------------"
echo ""

# Run the test and handle success/failure
run_e2e_test_flow "./workspace/calculator" "./workspace/calculator \"8 * 10 + 2\""
