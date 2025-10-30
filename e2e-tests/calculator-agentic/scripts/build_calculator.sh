#!/bin/bash
set -e

echo "=========================================="
echo "Building Calculator CLI with statcode-ai"
echo "=========================================="
echo "AGENTIC MODE: Full autonomy enabled"
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
echo "Note: statcode-ai will auto-configure from environment variables"
echo ""

# Initialize go module if not exists
if [ ! -f "go.mod" ]; then
    echo "Initializing Go module..."
    go mod init calculator-cli
fi

# Use statcode-ai to build the calculator CLI with full autonomy
# The --dangerous-allow-all flag bypasses authorization prompts for the e2e test
echo "Running statcode-ai in agentic mode..."
echo ""
statcode-ai --dangerous-allow-all "You are an autonomous software engineer. Build and test a Go command-line calculator.

REQUIREMENTS:
- Accept math expressions as CLI args: ./calculator \"8 * 10 + 2\"
- Support: +, -, *, /, parentheses
- Correct operator precedence (*, / before +, -)
- Print only the number to stdout
- Exit 0 on success, 1 on error

EXAMPLES (must pass):
  ./calculator \"8 * 10 + 2\"     → 82
  ./calculator \"(2 + 3) * 4\"    → 20
  ./calculator \"100 / 10 / 2\"   → 5

YOUR AUTONOMOUS WORKFLOW:
1. Create main.go with calculator implementation
2. Write test cases (as Go tests or shell commands)
3. Run your tests
4. Fix any failures
5. Build the calculator binary: go build -o calculator .

DO NOT ask for permission or clarification. You have full authority to:
- Choose any implementation (recursive descent, Shunting Yard, etc.)
- Write comprehensive tests
- Iterate and debug
- Make design decisions

EXPECTED OUTPUT:
- main.go (working implementation)
- calculator (compiled binary)
- Test results showing all tests pass

START NOW. Build the calculator."

# Verify the calculator was created
echo ""
echo "Verifying calculator was built..."
if [ ! -f "calculator" ]; then
    echo "ERROR: calculator binary was not created"
    echo "The AI should have built the calculator binary."
    exit 1
fi

if [ ! -f "main.go" ]; then
    echo "WARNING: main.go not found, but calculator binary exists"
fi

echo "=========================================="
echo "Calculator CLI built successfully!"
echo "=========================================="
