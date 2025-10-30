# Calculator CLI E2E Test

This end-to-end test demonstrates statcode-ai's ability to autonomously build a functional Go CLI calculator from a natural language prompt and validate it works correctly.

## Overview

The test:
1. Uses statcode-ai to generate a calculator CLI that can evaluate mathematical expressions
2. Builds the generated calculator binary
3. Tests it with multiple expressions including operator precedence and parentheses

## Prerequisites

- Docker and Docker Compose
- API key for an LLM provider (Anthropic or OpenAI)

## Quick Start

1. Set your API key:
   ```bash
   export ANTHROPIC_API_KEY="your-api-key-here"
   # OR
   export OPENAI_API_KEY="your-api-key-here"
   ```

2. Run the test:
   ```bash
   cd e2e-tests/calculator-cli
   ./run.sh
   ```

That's it! The script will:
- Validate your API key is set
- Build the Docker containers
- Generate the calculator using statcode-ai
- Run all test cases
- Report results

### Alternative: Manual Docker Compose

You can also run docker-compose directly:
```bash
docker-compose up --build
```

### Options

- Clean up previous runs before testing:
  ```bash
  ./run.sh --clean
  ```

- Use a .env file (see .env.example):
  ```bash
  cp .env.example .env
  # Edit .env with your API key
  ./run.sh
  ```

## Test Cases

The test validates the calculator with these expressions:

- `8 * 10 + 2` = 82 (multiplication before addition)
- `2 * 3 + 5` = 11 (multiplication before addition)
- `10 + 5 * 2` = 20 (multiplication before addition)
- `100 / 4` = 25 (division)
- `(2 + 3) * 4` = 20 (parentheses)
- `50 - 10 * 2` = 30 (multiplication before subtraction)
- `1 + 2 + 3 + 4` = 10 (left-to-right addition)
- `100 / 10 / 2` = 5 (left-to-right division)

## Architecture

```
e2e-tests/calculator-cli/
├── run.sh                   # Main test runner script
├── docker-compose.yml       # Orchestrates the test
├── Dockerfile               # Builds statcode-ai container
├── .env.example             # API key template
├── .gitignore               # Ignores workspace and .env
├── scripts/
│   ├── build_calculator.sh  # Uses statcode-ai to generate the calculator
│   └── test_calculator.sh   # Tests the generated calculator
└── workspace/               # Working directory (created during test)
    ├── main.go              # Generated calculator code
    ├── calculator           # Built binary
    └── go.mod               # Go module file
```

## How It Works

1. **statcode-ai service**:
   - Builds the statcode-ai binary
   - Runs `build_calculator.sh` which prompts statcode-ai to create a calculator CLI
   - statcode-ai generates `main.go` and builds the `calculator` binary

2. **test-runner service**:
   - Waits for statcode-ai to complete successfully
   - Runs `test_calculator.sh` to validate the calculator with test expressions
   - Reports pass/fail results

## Expected Output

```
==========================================
Building Calculator CLI with statcode-ai
==========================================
...
==========================================
Calculator CLI built successfully!
==========================================

==========================================
Testing Calculator CLI
==========================================

Running test cases...

Testing: 8 * 10 + 2 = 82 ... ✓ PASS
Testing: 2 * 3 + 5 = 11 ... ✓ PASS
Testing: 10 + 5 * 2 = 20 ... ✓ PASS
Testing: 100 / 4 = 25 ... ✓ PASS
Testing: (2 + 3) * 4 = 20 ... ✓ PASS
Testing: 50 - 10 * 2 = 30 ... ✓ PASS
Testing: 1 + 2 + 3 + 4 = 10 ... ✓ PASS
Testing: 100 / 10 / 2 = 5 ... ✓ PASS

==========================================
Test Results
==========================================
Passed: 8
Failed: 0
Total:  8
==========================================
All tests passed!
```

## Cleanup

Using run.sh with --clean flag:
```bash
./run.sh --clean
```

Or manually:
```bash
docker-compose down -v
rm -rf workspace/
```

## Troubleshooting

- **API key not set**: The run.sh script will check and inform you if no API key is found
- **Build failures**: Check that statcode-ai dependencies are correctly specified in go.mod
- **Test failures**: Review the generated calculator code in `workspace/main.go`
- **Clean start**: Use `./run.sh --clean` to remove all previous artifacts and start fresh
