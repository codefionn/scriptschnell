# Task Evaluation System Demo

## Overview

The task evaluation system extends the existing scriptschnell eval infrastructure to support agentic task evaluation. This allows testing models on complex multi-step problems that require autonomous design, implementation, and validation.

## Key Features Implemented

### 1. Task-Based Evaluation Architecture
- **TaskTestCase**: JSON-defined tasks with prompts, success criteria, and validation scripts
- **TaskEvalClient**: Handles task execution in isolated workspaces  
- **TaskEvalRunner**: Coordinates evaluation across multiple models
- **ValidationSystem**: Checks generated code against success criteria

### 2. Success Criteria Framework
```go
success_criteria: {
    "file_exists": "main.go",           // Check file existence
    "contains_code": "package main",     // Search for patterns
    "compiles": true,                   // Attempt compilation
    "tests_pass": true                  // Run test suite
}
```

### 3. Comprehensive Analytics & Reporting
- Model rankings based on success rate and cost efficiency
- Category-based performance analysis
- Detailed cost breakdowns by model and task
- Export to JSON/CSV formats

### 4. CLI Interface
- Backward compatible with existing simple evaluation
- New `task` subcommand for agentic evaluation
- Rich configuration options and progress tracking

## Usage Examples

### Run Simple Evaluation (Backward Compatible)
```bash
./eval-cli simple --models mistralai/devstral-2512,openai/gpt-4
```

### Run Task Evaluation  
```bash
./eval-cli task --task tasks/calculator-agentic.json
./eval-cli task --tasks-dir tasks/ --models openai/gpt-4
```

### Advanced Configuration
```bash
./eval-cli task \
  --tasks-dir tasks/ \
  --models "openai/gpt-4,anthropic/claude-3-opus" \
  --temperature 0.7 \
  --timeout 60m \
  --verbose \
  --output results.json
```

## Example Task: Calculator Agentic

The system includes a sophisticated example task that challenges models to:

1. **Autonomous Design**: Read requirements and design a calculator architecture
2. **Implementation**: Write Go code with proper error handling
3. **Testing**: Create comprehensive test suites  
4. **Validation**: Ensure operator precedence and parentheses work correctly
5. **Iteration**: Fix any issues until all tests pass

### Task Definition
```json
{
  "id": "calculator-agentic",
  "name": "Calculator CLI - Agentic Implementation", 
  "description": "AI autonomously designs, implements, tests, and validates a Go CLI calculator",
  
  "prompt": "You need to create a command-line calculator in Go...",
  
  "success_criteria": {
    "file_exists": "calculator",
    "contains_code": "package main", 
    "compiles": true,
    "tests_pass": true
  },
  
  "validation_script": "#!/bin/bash\n# Run comprehensive validation...\n",
  
  "category": "coding",
  "difficulty": "medium",
  "timeout": "10m"
}
```

### Expected Output
```bash
=== Model Performance ===
Model                           Tests     Success%     Cost ($)    Tokens
openai/gpt-4                      8       100.0%     $0.000012      850
mistralai/devstral-2512           8        75.0%     $0.000008      420

=== Category Performance ===
coding:              8/8 (100.0%)

=== Top Ranked Models ===
1. openai/gpt-4 (Score: 87.5)
2. mistralai/devstral-2512 (Score: 68.2)
```

## Architecture Details

### Task Execution Flow
1. **Workspace Setup**: Create isolated temporary directory
2. **Task Dispatch**: Send prompt + context to LLM with tools
3. **Autonomous Work**: AI uses tools to design, code, and test
4. **Validation**: Run validation scripts and check criteria
5. **Results Collection**: Track files, metrics, costs, timing
6. **Cleanup**: Remove temporary files (unless --clean=false)

### Tool Integration
The system integrates with scriptschnell's existing tool ecosystem:
- `read_file`: Examine existing code/templates
- `create_file`: Generate new source files
- `write_file_diff`: Update existing implementations  
- `go_sandbox`: Test compilation and run code
- `todo`: Plan and track implementation steps

### Cost Tracking
- **Granular**: Track costs per model, per task, per operation
- **Transparent**: Show prompt/completion tokens and currency
- **Optimization**: Help identify cost-effective model choices

## Integration with Existing E2E Tests

The task system is designed to complement the existing e2e-tests directory:

- **E2E Tests**: Full integration testing with Docker and real environments
- **Task Eval**: Rapid iteration testing with simulated environments  
- **Both**: Share similar validation approaches and success criteria

You can convert an e2e-test to a task eval by:
1. Extract the core prompt/requirements
2. Define success criteria
3. Create validation script (optional)
4. Add to tasks/ directory

## Future Enhancements

### Planned Features
1. **Dynamic Task Discovery**: Auto-discover tasks from e2e-tests
2. **Multi-Language Support**: Beyond Go (Python, JavaScript, etc.)
3. **Advanced Validation**: Semantic similarity, performance benchmarks
4. **Team Collaboration**: Shared task libraries and results
5. **CI/CD Integration**: Automated model validation pipelines

### Extension Points
- Add new success criteria types in `task_eval.go`
- Implement customized validation logic for specific domains
- Extend reporting with domain-specific metrics
- Add new model providers beyond OpenRouter

## Quality Assurance

### Testing
- Core component tests validate structures and metrics
- Mock evaluation system tests without API dependencies
- Integration tests with real models (requires API key)

### Reliability
- Graceful error handling for network issues
- Timeout management for long-running tasks
- Progress tracking and interrupt handling
- Comprehensive logging and debugging support

This system successfully bridges the gap between simple prompt-response testing and real-world agentic capabilities, providing a comprehensive framework for evaluating LLM models on complex, practical tasks.