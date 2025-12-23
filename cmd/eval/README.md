# Model Evaluation Tool for Scriptschnell

A comprehensive model evaluation system that supports both simple prompt-response testing and complex agentic task evaluation.

## Features

### 🔍 Simple Evaluation
- Test models on individual prompts with expected responses
- Good for basic capability testing and benchmarking
- Support for multiple test categories (calculator, reasoning, coding)
- Detailed cost analysis and performance metrics

### 🎯 Task-Based Evaluation
- Test models on complex multi-step agentic tasks
- AI autonomously designs, implements, and validates solutions
- Supports task validation scripts and success criteria
- Tracks generated files, iterations, and detailed execution metrics

### 📊 Analytics & Reporting
- Model rankings based on performance and cost efficiency
- Category-based performance analysis  
- Detailed cost breakdowns by model and category
- Export to JSON, CSV formats
- Real-time progress tracking

### 🌐 Provider Support
- OpenRouter integration with multiple model providers
- Configurable API keys and model selection
- Automatic model discovery and validation

## Installation

```bash
cd cmd/eval
go build -o eval-cli .
```

## Quick Start

### 1. Configuration

Create a configuration file or use environment variables:

```bash
export OPENROUTER_API_KEY="your-key-here"
```

### 2. Simple Evaluation

Test models on basic prompts:

```bash
# Run with default configuration
./eval-cli simple

# Evaluate specific models
./eval-cli simple --models mistralai/devstral-2512,openai/gpt-4

# Custom settings
./eval-cli simple --temperature 0.3 --output results.json --verbose
```

### 3. Task-Based Evaluation

Test models on complex agentic tasks:

```bash
# Run a specific task
./eval-cli task --task tasks/calculator-agentic.json

# Run all tasks in directory
./eval-cli task --tasks-dir tasks/

# Evaluate specific models on tasks
./eval-cli task --models openai/gpt-4 --tasks-dir tasks/

# Custom settings
./eval-cli task --temperature 0.7 --timeout 30m --verbose
```

## Task Development

### Creating Tasks

Tasks are JSON files defining the agentic problem to solve:

```json
{
  "id": "my-task",
  "name": "My Task",
  "description": "Task description",
  
  "prompt": "Detailed task instructions for the AI...",
  
  "success_criteria": {
    "file_exists": "main.go",
    "contains_code": "package main",
    "compiles": true,
    "tests_pass": true
  },
  
  "validation_script": "#!/bin/bash\necho 'Running validation...'\n# Custom validation logic",
  
  "category": "coding",
  "difficulty": "medium",
  "timeout": "10m"
}
```

### Success Criteria

Available criteria types:
- `file_exists`: Check if a file exists
- `contains_code`: Search for code pattern in files
- `compiles`: Attempt to compile the project
- `tests_pass`: Run test suite

### Validation Scripts

Optional bash scripts for custom validation logic:

```bash
#!/bin/bash
set -e

echo "Running validation checks..."

# Check required files
if [ ! -f "main.go" ]; then
  echo "ERROR: main.go not found"
  exit 1
fi

# Run tests
go test -v .

# Run specific functionality tests
./my-program test-input
```

## Example Tasks

### Calculator Agentic Task

```bash
./eval-cli task --task tasks/calculator-agentic.json
```

This task challenges models to:
1. Design a calculator CLI from scratch
2. Implement proper operator precedence
3. Support parentheses
4. Write comprehensive tests
5. Ensure all tests pass

### Simple Calculator Task

```bash  
./eval-cli task --task tasks/simple-calculator.json
```

Basic task to implement Add/Subtract functions in Go.

## Configuration

### Command Line Options

#### General Options
- `--config`: Configuration file path
- `--models`: Comma-separated model IDs to evaluate
- `--temperature`: LLM temperature (0.0-1.0)
- `--max-tokens`: Maximum tokens per request
- `--verbose`: Enable verbose output

#### Task Evaluation Options
- `--task`: Single task file to evaluate
- `--tasks-dir`: Directory containing task files
- `--timeout`: Overall evaluation timeout
- `--clean`: Clean temporary files after evaluation

### Configuration File

JSON configuration file format:

```json
{
  "openrouter_api_key": "your-key-here",
  "models": [
    {
      "id": "openai/gpt-4",
      "display_name": "GPT-4",
      "provider": "openrouter", 
      "enabled": true
    }
  ],
  "test_cases": [
    {
      "id": "basic_math",
      "name": "Basic Math",
      "prompt": "What is 15 + 27?",
      "expected": "42",
      "category": "calculator"
    }
  ],
  "temperature": 0.7,
  "max_tokens": 4096,
  "timeout": "60s",
  "verbose": true
}
```

## Results & Analytics

### Output Files

- `task_results_*.json`: Detailed task results
- `task_summary_*.json`: Aggregated summary and rankings
- `eval_results.json`: Simple evaluation results  
- `eval_summary.json`: Simple evaluation summary

### Model Rankings
Models are ranked using a scoring algorithm that balances:
- Success rate (primary factor)
- Cost efficiency (lower cost is better)
- Average completion time

### Cost Analysis
Detailed cost breakdowns by:
- Model (total cost, tokens, cost/token)
- Category (cost distribution across types)
- Individual test/task performance

## Examples

### Compare Model Performance

```bash
# Simple evaluation comparison
./eval-cli simple --models "mistralai/devstral-2512,openai/gpt-4,google/gemini-3-pro-preview" --verbose

# Task-based comparison
./eval-cli task --tasks-dir tasks/ --models "openai/gpt-4,anthropic/claude-3-opus" --timeout 60m
```

### Custom Evaluation

```bash
# High-precision evaluation
./eval-cli simple --temperature 0.1 --max-tokens 8000 --models "openai/gpt-4"

# Quick cost-effective testing  
./eval-cli simple --temperature 0.8 --max-tokens 1000 --models "mistralai/devstral-2512"
```

### Integration with CI/CD

```bash
#!/bin/bash
# CI evaluation script
set -e

# Run core test suite
./eval-cli simple --models "openai/gpt-4" --output core_results.json

# Check if success rate meets threshold (e.g., 95%)
success_rate=$(jq '.success_rate' core_results.json)
if (( $(echo "$success_rate < 0.95" | bc -l) )); then
  echo "Model performance below threshold: ${success_rate}"
  exit 1
fi

# Run agentic task validation
./eval-cli task --task tasks/critical-task.json --models "openai/gpt-4"
```

## Architecture

### Core Components

1. **EvalRunner**: Coordinates simple evaluation across models
2. **TaskEvalRunner**: Coordinates task-based evaluation
3. **OpenRouterEvalClient**: Handles API interactions and cost tracking
4. **TaskEvalClient**: Manages task execution and validation
5. **ResultsReporter**: Generates reports and analytics

### Task Execution Flow

1. **Setup**: Create isolated workspace for each task
2. **Execution**: AI receives prompt and works autonomously
3. **Validation**: Run validation scripts and check success criteria
4. **Analysis**: Collect metrics, costs, generated files
5. **Reporting**: Generate rankings and detailed reports

## Troubleshooting

### Common Issues

**Build Errors**: Ensure Go 1.21+ and all dependencies are installed

```bash
go mod tidy
go build -o eval-cli .
```

**API Key Issues**: Verify OpenRouter API key is valid

```bash
export OPENROUTER_API_KEY="your-key-here"
./eval-cli simple --models openai/gpt-4 --verbose
```

**Task Timeouts**: Increase timeout for complex tasks

```bash
./eval-cli task --timeout 60m --tasks-dir tasks/
```

**Validation Failures**: Check task setup and validation scripts

```bash
./eval-cli task --task tasks/my-task.json --verbose
```

### Debug Mode

Enable verbose output to see detailed execution:

```bash
./eval-cli task --task tasks/debug.json --verbose --temperature 0.0
```

## Contributing

### Adding New Tasks

1. Create task JSON in `tasks/` directory
2. Define clear success criteria
3. Add validation script if needed
4. Test with multiple models
5. Update documentation

### Extending Evaluation Types

1. Add new criteria types in `task_eval.go`
2. Implement validation logic
3. Update result structures
4. Add reporting metrics

## License

Part of the scriptschnell project. See main LICENSE for details.