# Calculator CLI E2E Test - Operator Precedence Upgrade

This end-to-end test validates statcode-ai's ability to **understand and refactor existing code**. The AI is given a working Go CLI calculator that evaluates expressions strictly left-to-right (no operator precedence, no parentheses). The test prompt asks the AI to upgrade the implementation to support proper precedence and parentheses, prove the change with tests, and rebuild the binary.

## Overview

- **Starting point**: `main.go` already exists and parses expressions naïvely.
- **Task**: Improve the evaluator so `*` and `/` run before `+` and `-`, and parentheses work.
- **Expectation**: AI inspects the baseline, updates the parser, runs `go test`, and rebuilds `calculator`.
- **Validation**: An external shell script evaluates expressions that require correct precedence.

This scenario mirrors a realistic maintenance task: augmenting an existing feature without rewriting from scratch.

## Prerequisites

- Docker and Docker Compose
- API key for an LLM provider (Anthropic, OpenAI, or Gemini)

## Quick Start

```bash
export OPENAI_API_KEY="your-api-key"
cd e2e-tests/calculator-precedence
./run.sh
```

### Options

- Clean Docker state before running:
  ```bash
  ./run.sh --clean
  ```
- Use a `.env` file (see `.env.example`) to store API keys.

## What Happens During the Test

1. `run.sh` resets `workspace/` to the baseline implementation from `workspace-template/`.
2. Docker builds the statcode-ai image and runs `scripts/build_calculator.sh`.
3. The script prompts statcode-ai to:
   - Inspect the existing calculator.
   - Implement operator precedence and parentheses.
   - Add/adjust Go tests as needed.
   - Run `go test ./...`.
   - Build the CLI binary with `go build -o calculator .`.
4. A separate container executes `scripts/test_calculator.sh`, which validates:
   - `8 * 10 + 2 = 82`
   - `10 + 5 * 2 = 20`
   - `(2 + 3) * 4 = 20`
   - `2 * 3 + 5 = 11`
   - `100 / 4 = 25`
   - `50 - 10 * 2 = 30`
   - `1 + 2 + 3 + 4 = 10`
   - `100 / 10 / 2 = 5`

## Repository Layout

```
e2e-tests/calculator-precedence/
├── run.sh                   # Main orchestrator
├── docker-compose.yml       # statcode-ai + test-runner services
├── Dockerfile               # Builds statcode-ai binary
├── scripts/
│   ├── build_calculator.sh  # Prompts statcode-ai to upgrade calculator
│   └── test_calculator.sh   # Validates final binary
├── workspace-template/      # Baseline implementation without precedence
│   ├── go.mod
│   ├── main.go
│   └── main_test.go
└── workspace/               # Populated at runtime
```

## Expected Output

```
==========================================
Building Calculator CLI with statcode-ai
==========================================
Baseline implementation detected (no operator precedence)
Prompting statcode-ai to upgrade the calculator...
...
go test ./...
ok      calculator      0.XXXs
go build -o calculator .

==========================================
Testing Calculator CLI
==========================================
Testing: 10 + 5 * 2 = 20 ... ✓ PASS
Testing: (2 + 3) * 4 = 20 ... ✓ PASS
...
All tests passed!
```

## Troubleshooting

- **AI did not modify the code**: Ensure the baseline files exist in `workspace/`. Run `./run.sh --clean` to reset.
- **Tests still failing**: Inspect `workspace/main.go` to confirm operator precedence is implemented.
- **Need a fresh run**: Clean Docker state (`./run.sh --clean`) or remove `workspace/` manually.

## Cleanup

```bash
docker-compose down -v
rm -rf workspace/
```
