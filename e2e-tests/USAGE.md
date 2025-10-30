# E2E Test Suite Usage Guide

This guide covers how to use the E2E test infrastructure, including the master test runner and the shared library.

## Quick Start

### Run All Tests

From the `e2e-tests` directory, use the master `run.sh` script:

```bash
./run.sh <provider> <api-key> [options]
```

**Examples:**
```bash
# Run all tests with Anthropic (Claude)
./run.sh anthropic sk-ant-xxxxx

# Run all tests with OpenAI (GPT)
./run.sh openai sk-xxxxx

# Run with Docker cleanup
./run.sh anthropic sk-ant-xxxxx --clean
```

### Run Individual Test

Navigate to a specific test directory:

```bash
cd calculator-cli
export ANTHROPIC_API_KEY="sk-ant-xxxxx"
./run.sh
```

## Supported Providers

The master script supports all StatCode AI providers:

| Provider | Example Command | Notes |
|----------|----------------|-------|
| **Anthropic** | `./run.sh anthropic sk-ant-xxxxx` | Claude models |
| **OpenAI** | `./run.sh openai sk-xxxxx` | GPT models |
| **Gemini** | `./run.sh gemini xxxxx` | Google Gemini models |
| **Mistral** | `./run.sh mistral xxxxx` | Mistral AI models |
| **Groq** | `./run.sh groq xxxxx` | Groq inference |
| **OpenRouter** | `./run.sh openrouter xxxxx` | OpenRouter gateway |
| **Cerebras** | `./run.sh cerebras xxxxx` | Cerebras models |
| **Ollama** | `./run.sh ollama http://localhost:11434` | Local Ollama instance |
| **Perplexity** | `./run.sh perplexity xxxxx` | Perplexity models |

## Configuration Options

### Command-Line Flags

```bash
# Clean Docker containers before running
./run.sh anthropic sk-ant-xxxxx --clean

# Override model selection
./run.sh openai sk-xxxxx --model gpt-4o

# Set custom timeout (in seconds)
./run.sh anthropic sk-ant-xxxxx --timeout 900

# Combine multiple options
./run.sh openai sk-xxxxx --model gpt-4o --clean --timeout 1200
```

### Environment Variables

Configure additional settings via environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `STATCODE_MODEL` | Override model selection | `claude-3-opus-20240229` |
| `STATCODE_TEMPERATURE` | LLM temperature (0.0-1.0) | `0.7` |
| `STATCODE_TIMEOUT` | Timeout in seconds | `900` |
| `STATCODE_LOG_LEVEL` | Log level | `debug`, `info`, `warn`, `error` |

**Usage:**
```bash
# Set temperature
STATCODE_TEMPERATURE=0.7 ./run.sh anthropic sk-ant-xxxxx

# Enable debug logging
STATCODE_LOG_LEVEL=debug ./run.sh openai sk-xxxxx

# Override model
STATCODE_MODEL=claude-3-opus-20240229 ./run.sh anthropic sk-ant-xxxxx

# Combine multiple variables
STATCODE_TEMPERATURE=0.7 STATCODE_LOG_LEVEL=debug ./run.sh openai sk-xxxxx
```

## How the Master Script Works

### Provider Isolation

The master script ensures clean provider isolation:

1. **Unsets all provider API keys** before running
2. **Sets only the specified provider's API key**
3. Runs all tests with that single provider
4. Prevents conflicts between multiple providers

This means if you have multiple API keys in your environment, only the specified one will be used.

### Test Execution Flow

1. Validates provider name and API key
2. Clears all provider environment variables
3. Sets the specified provider's API key
4. Applies configuration overrides (model, timeout, etc.)
5. Displays configuration summary
6. Runs each test suite sequentially:
   - calculator-cli
   - calculator-agentic
   - calculator-precedence
7. Tracks pass/fail status for each test
8. Prints comprehensive results summary
9. Exits with code 0 only if all tests pass

### Output

The script provides detailed output:

```
==========================================
StatCode AI - E2E Test Suite
Running all tests with anthropic provider
==========================================

Configuration:
  Provider: anthropic
  Model: claude-3-opus-20240229
  Temperature: 0.7

========================================
Test 1/3: calculator-cli
========================================
[... test output ...]
✓ calculator-cli PASSED

----------------------------------------

========================================
Test 2/3: calculator-agentic
========================================
[... test output ...]
✓ calculator-agentic PASSED

----------------------------------------

========================================
Test 3/3: calculator-precedence
========================================
[... test output ...]
✓ calculator-precedence PASSED

==========================================
E2E Test Suite - Final Results
==========================================

Configuration:
  Provider: anthropic
  Model: claude-3-opus-20240229
  Temperature: 0.7

Results:
  Total tests: 3
  Passed: 3
  Failed: 0

Passed tests:
  ✓ calculator-cli
  ✓ calculator-agentic
  ✓ calculator-precedence

==========================================
✓ ALL TESTS PASSED!
==========================================
```

## Recent Updates

### Multi-Provider Support (Latest)

The library and master script now support **all 9 StatCode AI providers**:
- All provider API keys are recognized by `check_api_keys()`
- All provider keys are displayed by `display_api_keys()`
- Master script ensures provider isolation (unsets conflicting keys)
- Individual tests can be run with any supported provider

This means you can now use:
```bash
./run.sh cerebras your-key
./run.sh groq your-key
./run.sh mistral your-key
# ... and any other supported provider
```

## Library Functions (lib.sh)

The shared `lib.sh` library provides reusable functions for all test scripts.

### Output Functions

```bash
print_header "Title" ["Subtitle"]    # Section header with borders
print_success "message"              # Green success message with ✓
print_error "message"                # Red error message with ✗
print_warning "message"              # Yellow warning message
print_info "message"                 # Standard info message
print_color "$GREEN" "message"       # Custom color message
```

### Environment Functions

```bash
check_api_keys                       # Verify at least one API key exists
load_env_file [path]                 # Load .env file (default: .env)
display_api_keys                     # Show which API keys are set
```

### Workspace Functions

```bash
clean_workspace [dir]                # Remove and recreate workspace
copy_workspace_template [src] [dst]  # Copy baseline files
```

### Docker Functions

```bash
clean_docker [compose_file]          # Stop and remove containers
should_clean "$@"                    # Check for --clean flag
run_docker_compose [file] [service]  # Run Docker Compose
```

### High-Level Functions

```bash
init_e2e_test "$@"                   # Complete initialization
run_e2e_test_flow [output] [cmd]     # Complete test lifecycle
print_success_summary [path] [cmd]   # Success message
print_failure_summary                # Failure message
```

### Color Variables

The library exports these color codes:

```bash
$RED      # Red color
$GREEN    # Green color
$YELLOW   # Yellow color
$BLUE     # Blue color
$NC       # No color (reset)
```

## Writing Custom Tests

To create a new E2E test using the library:

### 1. Create Test Directory

```bash
mkdir e2e-tests/my-new-test
cd e2e-tests/my-new-test
```

### 2. Create run.sh

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")"

# Source the library
source ../lib.sh

# Print header
print_header "StatCode AI - My Test" "Optional subtitle"

# Initialize (handles API keys, workspace, Docker cleanup)
init_e2e_test "$@"

# Describe the test
echo "Starting my custom test..."
echo "This will:"
echo "  1. Do something"
echo "  2. Do something else"
echo ""

# Run the test
run_e2e_test_flow "./workspace/output" "./workspace/output --help"
```

### 3. Create Docker Compose Setup

Create `docker-compose.yml`, `Dockerfile`, and test scripts following the pattern in existing tests.

### 4. Register in Master Script

Add your test directory to the `TEST_DIRS` array in `e2e-tests/run.sh`:

```bash
TEST_DIRS=(
    "calculator-cli"
    "calculator-agentic"
    "calculator-precedence"
    "my-new-test"        # Add your test here
)
```

## Troubleshooting

### No API Key Error

```bash
export ANTHROPIC_API_KEY="your-key-here"
# or
./run.sh anthropic your-key-here
```

### Docker Issues

```bash
# Clean everything and retry
./run.sh anthropic your-key --clean

# Check Docker logs
cd calculator-cli
docker-compose up --build
```

### View Generated Code

```bash
cd calculator-cli
ls -la workspace/
cat workspace/main.go
```

### Test a Specific Provider

```bash
# Test just one provider
cd calculator-cli
export GROQ_API_KEY="your-key"
./run.sh
```

### Debug Mode

```bash
# Enable debug logging
STATCODE_LOG_LEVEL=debug ./run.sh anthropic your-key
```

## Best Practices

1. **Always use the master script** when running full test suites
2. **Use --clean flag** when encountering Docker issues
3. **Set explicit timeouts** for slow providers
4. **Use environment variables** for sensitive configuration
5. **Check provider API key format** - they differ by provider
6. **For Ollama**, ensure the server is running first

## Examples

### Complete Test Suite Run

```bash
# Full test suite with all options
STATCODE_TEMPERATURE=0.7 \
STATCODE_LOG_LEVEL=info \
./run.sh anthropic sk-ant-xxxxx \
  --model claude-3-opus-20240229 \
  --timeout 1200 \
  --clean
```

### Quick Iteration During Development

```bash
# Test single suite quickly
cd calculator-cli
export ANTHROPIC_API_KEY="sk-ant-xxxxx"
./run.sh
```

### Compare Providers

```bash
# Test with different providers
./run.sh anthropic sk-ant-xxxxx > results-anthropic.log
./run.sh openai sk-xxxxx > results-openai.log
./run.sh groq xxxxx > results-groq.log
diff results-*.log
```

## Additional Resources

- **Individual test READMEs**: Each test directory has detailed documentation
- **lib.sh source**: See inline comments for function details
- **Docker Compose files**: Review for understanding test architecture
- **CLAUDE.md**: Project-level documentation in repository root
