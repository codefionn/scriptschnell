#!/bin/bash
# Don't exit on error so we can see all test results
set +e

echo "=========================================="
echo "Testing Calculator CLI"
echo "=========================================="

WORKSPACE_DIR="${WORKSPACE_DIR:-/workspace}"
CALCULATOR_BIN="${WORKSPACE_DIR}/calculator"
MODULE_NAME="${MODULE_NAME:-calculator-cli}"

if [ ! -d "$WORKSPACE_DIR" ]; then
    echo "ERROR: workspace directory not found at $WORKSPACE_DIR"
    exit 1
fi

# Check if calculator binary exists – build it if missing
if [ ! -f "$CALCULATOR_BIN" ]; then
    echo "Calculator binary missing – attempting to build it now..."

    if [ ! -f "${WORKSPACE_DIR}/go.mod" ]; then
        echo "go.mod missing – initializing module ${MODULE_NAME}..."
        if ! (cd "$WORKSPACE_DIR" && go mod init "${MODULE_NAME}"); then
            echo "ERROR: failed to initialize go.mod in ${WORKSPACE_DIR}"
            exit 1
        fi
    fi

    if ! (cd "$WORKSPACE_DIR" && go build -o calculator .); then
        echo "ERROR: failed to build calculator binary"
        exit 1
    fi

    if [ ! -f "$CALCULATOR_BIN" ]; then
        echo "ERROR: calculator binary still not found after build attempt"
        exit 1
    fi
fi

# Make calculator executable
chmod +x "$CALCULATOR_BIN"

# Test cases
declare -a tests=(
    "8 * 10 + 2:82"
    "2 * 3 + 5:11"
    "10 + 5 * 2:20"
    "100 / 4:25"
    "(2 + 3) * 4:20"
    "50 - 10 * 2:30"
    "1 + 2 + 3 + 4:10"
    "100 / 10 / 2:5"
)

failed=0
passed=0

echo ""
echo "Running test cases..."
echo ""

for test in "${tests[@]}"; do
    IFS=':' read -r expression expected <<< "$test"

    echo -n "Testing: $expression = $expected ... "

    # Run calculator and capture output
    result=$("$CALCULATOR_BIN" "$expression" 2>&1 || echo "ERROR")

    # Trim whitespace
    result=$(echo "$result" | tr -d '[:space:]')
    expected=$(echo "$expected" | tr -d '[:space:]')

    if [ "$result" = "$expected" ]; then
        echo "✓ PASS"
        ((passed++))
    else
        echo "✗ FAIL (got: $result)"
        ((failed++))
    fi
done

echo ""
echo "=========================================="
echo "Test Results"
echo "=========================================="
echo "Passed: $passed"
echo "Failed: $failed"
echo "Total:  $((passed + failed))"
echo "=========================================="

if [ $failed -gt 0 ]; then
    echo "Some tests failed!"
    exit 1
fi

echo "All tests passed!"
exit 0
