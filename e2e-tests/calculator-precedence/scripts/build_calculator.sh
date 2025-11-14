#!/bin/bash
set -e

echo "=========================================="
echo "Building Calculator CLI with statcode-ai"
echo "=========================================="
echo "Scenario: Upgrade existing CLI to support operator precedence"
echo "=========================================="

# Check which API key is available (for logging purposes)
if [ -n "$ANTHROPIC_API_KEY" ]; then
    echo "Detected: ANTHROPIC_API_KEY"
elif [ -n "$OPENAI_API_KEY" ]; then
    echo "Detected: OPENAI_API_KEY"
elif [ -n "$GEMINI_API_KEY" ]; then
    echo "Detected: GEMINI_API_KEY"
elif [ -n "$MISTRAL_API_KEY" ]; then
    echo "Detected: MISTRAL_API_KEY"
elif [ -n "$GROQ_API_KEY" ]; then
    echo "Detected: GROQ_API_KEY"
elif [ -n "$OPENROUTER_API_KEY" ]; then
    echo "Detected: OPENROUTER_API_KEY"
elif [ -n "$CEREBRAS_API_KEY" ]; then
    echo "Detected: CEREBRAS_API_KEY"
elif [ -n "$OLLAMA_HOST" ]; then
    echo "Detected: OLLAMA_HOST ($OLLAMA_HOST)"
elif [ -n "$PERPLEXITY_API_KEY" ]; then
    echo "Detected: PERPLEXITY_API_KEY"
else
    echo "ERROR: No API key found!"
    echo "Please set one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY,"
    echo "                   MISTRAL_API_KEY, GROQ_API_KEY, OPENROUTER_API_KEY,"
    echo "                   CEREBRAS_API_KEY, OLLAMA_HOST, PERPLEXITY_API_KEY"
    exit 1
fi

echo ""
echo "Baseline project detected:"
echo "- main.go → left-to-right evaluation, no parentheses"
echo "- main_test.go → expectations for operator precedence and parentheses"
echo ""

if [ ! -f "go.mod" ]; then
    echo "ERROR: go.mod missing. The workspace should contain the baseline project."
    exit 1
fi

# Use statcode-ai to upgrade the calculator implementation
# The --dangerous-allow-all flag bypasses authorization prompts for the e2e test
echo "Prompting statcode-ai to implement operator precedence..."
echo ""
statcode-ai --dangerous-allow-all "You are working inside an existing Go project for a CLI calculator. The current evaluate function in main.go walks the expression left-to-right and fails to respect operator precedence or parentheses.

YOUR OBJECTIVE:
  * Upgrade main.go so the CLI supports +, -, *, / with correct precedence and parentheses (standard arithmetic rules).
  * Preserve the command-line interface: \`./calculator \"expression\"\` prints only the numeric result and exits 0 on success, 1 on error.
  * Use the provided Go tests in main_test.go as the acceptance criteria. You may add more tests if helpful.
  * Run \`go test ./...\` to prove the upgrade works.
  * Build the binary with \`go build -o calculator .\`.

AVAILABLE CONTEXT:
  * main.go contains the current implementation without precedence.
  * main_test.go encodes the desired behavior and currently fails.

WORKFLOW GUIDANCE:
  1. Read main.go and main_test.go.
  2. Implement a parsing strategy that respects precedence and parentheses (recursive descent, shunting yard, or another correct approach).
  3. Update or extend tests if needed.
  4. Run gofmt, go test ./..., and go build -o calculator .
  5. Share the test results and any implementation notes.

You have full tool access and do not need to ask for confirmation.

EXPECTED OUTPUT:
- main.go (working implementation)
- calculator (compiled binary, this must be done for our cicd test suite)
- Test results showing all tests pass"

# Verify tests and build succeed after AI modifications
echo ""
echo "Running go test to confirm..."
go test ./...

echo "Building calculator binary..."
go build -o calculator .

if [ ! -f "calculator" ]; then
    echo "ERROR: calculator binary was not created"
    exit 1
fi

echo "=========================================="
echo "Calculator CLI upgraded successfully!"
echo "=========================================="
