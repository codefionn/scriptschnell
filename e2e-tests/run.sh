#!/bin/bash
#
# Master E2E Test Runner
# Runs all E2E tests in sequence
#
# Usage:
#   ./run.sh <provider> <api-key> [options]
#   ./run.sh <provider> <api-key> --clean
#
# Options:
#   --clean          Clean Docker containers before running
#   --model <name>   Override default model for the provider
#   --timeout <sec>  Set timeout for each test (default: 600)
#   --parallel       Run tests in parallel (not yet implemented)
#
# Environment Variables (optional):
#   STATCODE_MODEL       Override model selection
#   STATCODE_TEMPERATURE Temperature setting (0.0-1.0)
#   STATCODE_TIMEOUT     Timeout in seconds
#   STATCODE_LOG_LEVEL   Log level (debug, info, warn, error)
#
# Examples:
#   ./run.sh anthropic sk-ant-xxxxx
#   ./run.sh openai sk-xxxxx
#   ./run.sh gemini xxxxx
#   ./run.sh mistral xxxxx
#   ./run.sh groq xxxxx
#   ./run.sh openrouter xxxxx
#   ./run.sh cerebras xxxxx
#   ./run.sh ollama xxxxx
#   ./run.sh perplexity xxxxx
#   ./run.sh anthropic sk-ant-xxxxx --clean
#   ./run.sh openai sk-xxxxx --model gpt-4o
#   STATCODE_TEMPERATURE=0.7 ./run.sh anthropic sk-ant-xxxxx
#

set -e

cd "$(dirname "$0")"

# Source the E2E test library
source ./lib.sh

# Check arguments
if [ $# -lt 2 ]; then
    print_error "Missing required arguments!"
    echo ""
    echo "Usage: $0 <provider> <api-key> [options]"
    echo ""
    echo "Provider options:"
    echo "  anthropic   - Anthropic Claude models"
    echo "  openai      - OpenAI GPT models"
    echo "  gemini      - Google Gemini models"
    echo "  mistral     - Mistral AI models"
    echo "  groq        - Groq models"
    echo "  openrouter  - OpenRouter gateway"
    echo "  cerebras    - Cerebras models"
    echo "  ollama      - Ollama local models"
    echo "  perplexity  - Perplexity models"
    echo ""
    echo "Options:"
    echo "  --clean           Clean Docker containers before running"
    echo "  --model <name>    Override default model"
    echo "  --timeout <sec>   Set timeout for each test"
    echo ""
    echo "Examples:"
    echo "  $0 anthropic sk-ant-xxxxx"
    echo "  $0 openai sk-xxxxx --clean"
    echo "  $0 groq xxxxx --model llama-3.1-70b"
    echo ""
    exit 1
fi

PROVIDER=$1
API_KEY=$2
shift 2

# Parse optional arguments
CLEAN_FLAG=""
OVERRIDE_MODEL=""
OVERRIDE_TIMEOUT=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --clean|-c)
            CLEAN_FLAG="--clean"
            shift
            ;;
        --model)
            OVERRIDE_MODEL="$2"
            shift 2
            ;;
        --timeout)
            OVERRIDE_TIMEOUT="$2"
            shift 2
            ;;
        *)
            print_warning "Unknown option: $1"
            shift
            ;;
    esac
done

# Unset all provider API keys to avoid conflicts
unset ANTHROPIC_API_KEY
unset OPENAI_API_KEY
unset GEMINI_API_KEY
unset MISTRAL_API_KEY
unset GROQ_API_KEY
unset OPENROUTER_API_KEY
unset CEREBRAS_API_KEY
unset OLLAMA_HOST
unset PERPLEXITY_API_KEY

# Validate and set provider API key
case "$PROVIDER" in
    anthropic)
        export ANTHROPIC_API_KEY="$API_KEY"
        print_success "Using Anthropic (Claude) provider"
        ;;
    openai)
        export OPENAI_API_KEY="$API_KEY"
        print_success "Using OpenAI (GPT) provider"
        ;;
    gemini|google)
        export GEMINI_API_KEY="$API_KEY"
        print_success "Using Google Gemini provider"
        ;;
    mistral)
        export MISTRAL_API_KEY="$API_KEY"
        print_success "Using Mistral AI provider"
        ;;
    groq)
        export GROQ_API_KEY="$API_KEY"
        print_success "Using Groq provider"
        ;;
    openrouter)
        export OPENROUTER_API_KEY="$API_KEY"
        print_success "Using OpenRouter provider"
        ;;
    cerebras)
        export CEREBRAS_API_KEY="$API_KEY"
        print_success "Using Cerebras provider"
        ;;
    ollama)
        export OLLAMA_HOST="${API_KEY:-http://localhost:11434}"
        print_success "Using Ollama provider (host: $OLLAMA_HOST)"
        ;;
    perplexity)
        export PERPLEXITY_API_KEY="$API_KEY"
        print_success "Using Perplexity provider"
        ;;
    *)
        print_error "Invalid provider: $PROVIDER"
        echo ""
        echo "Valid providers: anthropic, openai, gemini, mistral, groq, openrouter, cerebras, ollama, perplexity"
        exit 1
        ;;
esac

# Export optional configuration environment variables
if [ -n "$OVERRIDE_MODEL" ]; then
    export STATCODE_MODEL="$OVERRIDE_MODEL"
    print_info "Model override: $OVERRIDE_MODEL"
fi

if [ -n "$OVERRIDE_TIMEOUT" ]; then
    export STATCODE_TIMEOUT="$OVERRIDE_TIMEOUT"
    print_info "Timeout override: ${OVERRIDE_TIMEOUT}s"
fi

# Export any user-set environment variables
if [ -n "$STATCODE_TEMPERATURE" ]; then
    print_info "Temperature: $STATCODE_TEMPERATURE"
fi

if [ -n "$STATCODE_LOG_LEVEL" ]; then
    print_info "Log level: $STATCODE_LOG_LEVEL"
fi

echo ""

# Print master header
print_header "scriptschnell - E2E Test Suite" "Running all tests with $PROVIDER provider"

# Track test results
PASSED_TESTS=()
FAILED_TESTS=()

# List of all test directories
TEST_DIRS=(
    "calculator-agentic"
    "calculator-precedence"
)

# Total number of tests
TOTAL_TESTS=${#TEST_DIRS[@]}
CURRENT_TEST=0

echo "Found $TOTAL_TESTS test suites to run"
echo ""

# Run each test
for test_dir in "${TEST_DIRS[@]}"; do
    CURRENT_TEST=$((CURRENT_TEST + 1))

    echo ""
    echo "========================================"
    print_color "$BLUE" "Test $CURRENT_TEST/$TOTAL_TESTS: $test_dir"
    echo "========================================"
    echo ""

    if [ ! -d "$test_dir" ]; then
        print_warning "Skipping $test_dir - directory not found"
        continue
    fi

    if [ ! -f "$test_dir/run.sh" ]; then
        print_warning "Skipping $test_dir - run.sh not found"
        continue
    fi

    # Run the test
    cd "$test_dir"

    if [ -n "$CLEAN_FLAG" ]; then
        if bash ./run.sh "$CLEAN_FLAG"; then
            PASSED_TESTS+=("$test_dir")
            print_success "$test_dir PASSED"
        else
            FAILED_TESTS+=("$test_dir")
            print_error "$test_dir FAILED"
        fi
    else
        if bash ./run.sh; then
            PASSED_TESTS+=("$test_dir")
            print_success "$test_dir PASSED"
        else
            FAILED_TESTS+=("$test_dir")
            print_error "$test_dir FAILED"
        fi
    fi

    cd ..

    # Add separator between tests
    if [ $CURRENT_TEST -lt $TOTAL_TESTS ]; then
        echo ""
        echo "----------------------------------------"
    fi
done

# Print final summary
echo ""
echo ""
print_header "E2E Test Suite - Final Results"

echo "Configuration:"
echo "  Provider: $PROVIDER"
if [ -n "$OVERRIDE_MODEL" ]; then
    echo "  Model: $OVERRIDE_MODEL"
fi
if [ -n "$STATCODE_TEMPERATURE" ]; then
    echo "  Temperature: $STATCODE_TEMPERATURE"
fi
if [ -n "$OVERRIDE_TIMEOUT" ]; then
    echo "  Timeout: ${OVERRIDE_TIMEOUT}s"
fi
echo ""
echo "Results:"
echo "  Total tests: $TOTAL_TESTS"
echo "  Passed: ${#PASSED_TESTS[@]}"
echo "  Failed: ${#FAILED_TESTS[@]}"
echo ""

if [ ${#PASSED_TESTS[@]} -gt 0 ]; then
    echo "Passed tests:"
    for test in "${PASSED_TESTS[@]}"; do
        print_success "  $test"
    done
    echo ""
fi

if [ ${#FAILED_TESTS[@]} -gt 0 ]; then
    echo "Failed tests:"
    for test in "${FAILED_TESTS[@]}"; do
        print_error "  $test"
    done
    echo ""
fi

# Exit with appropriate code
if [ ${#FAILED_TESTS[@]} -eq 0 ]; then
    echo ""
    print_color "$GREEN" "=========================================="
    print_color "$GREEN" "✓ ALL TESTS PASSED!"
    print_color "$GREEN" "=========================================="
    echo ""
    exit 0
else
    echo ""
    print_color "$RED" "=========================================="
    print_color "$RED" "✗ SOME TESTS FAILED!"
    print_color "$RED" "=========================================="
    echo ""
    echo "To clean up and retry:"
    echo "  $0 $PROVIDER <api-key> --clean"
    echo ""
    exit 1
fi
