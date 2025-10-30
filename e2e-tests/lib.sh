#!/bin/bash
#
# E2E Test Library - Common Functions
#
# This library provides shared functionality for all E2E test run.sh scripts.
# Source this file at the beginning of your run.sh:
#   source ../lib.sh  # or appropriate path to lib.sh
#

# Color codes for output formatting
export RED='\033[0;31m'
export GREEN='\033[0;32m'
export YELLOW='\033[1;33m'
export BLUE='\033[0;34m'
export NC='\033[0m' # No Color

# Print a colored message
# Usage: print_color COLOR "message"
print_color() {
    local color=$1
    shift
    echo -e "${color}$@${NC}"
}

# Print section header
# Usage: print_header "Title" ["Subtitle"]
print_header() {
    echo "=========================================="
    echo "$1"
    if [ -n "$2" ]; then
        echo "$2"
    fi
    echo "=========================================="
    echo ""
}

# Print info message
print_info() {
    echo "$@"
}

# Print success message
print_success() {
    print_color "$GREEN" "✓ $@"
}

# Print error message
print_error() {
    print_color "$RED" "✗ $@"
}

# Print warning message
print_warning() {
    print_color "$YELLOW" "$@"
}

# Check if required API keys are set
# Returns 0 if at least one API key is present, 1 otherwise
check_api_keys() {
    if [ -z "$ANTHROPIC_API_KEY" ] && \
       [ -z "$OPENAI_API_KEY" ] && \
       [ -z "$GEMINI_API_KEY" ] && \
       [ -z "$MISTRAL_API_KEY" ] && \
       [ -z "$GROQ_API_KEY" ] && \
       [ -z "$OPENROUTER_API_KEY" ] && \
       [ -z "$CEREBRAS_API_KEY" ] && \
       [ -z "$OLLAMA_HOST" ] && \
       [ -z "$PERPLEXITY_API_KEY" ]; then
        print_error "No API key found!"
        echo ""
        echo "Please set at least one of the following environment variables:"
        echo "  - ANTHROPIC_API_KEY (for Claude models)"
        echo "  - OPENAI_API_KEY (for GPT models)"
        echo "  - GEMINI_API_KEY (for Gemini models)"
        echo "  - MISTRAL_API_KEY (for Mistral AI models)"
        echo "  - GROQ_API_KEY (for Groq models)"
        echo "  - OPENROUTER_API_KEY (for OpenRouter)"
        echo "  - CEREBRAS_API_KEY (for Cerebras models)"
        echo "  - OLLAMA_HOST (for Ollama, e.g., http://localhost:11434)"
        echo "  - PERPLEXITY_API_KEY (for Perplexity models)"
        echo ""
        echo "Example:"
        echo "  export ANTHROPIC_API_KEY='your-key-here'"
        echo "  ./run.sh"
        echo ""
        echo "Or create a .env file (see .env.example)"
        echo ""
        echo "Or use the master script:"
        echo "  cd .. && ./run.sh cerebras your-key-here"
        return 1
    fi
    return 0
}

# Load environment variables from .env file if it exists
load_env_file() {
    local env_file="${1:-.env}"
    if [ -f "$env_file" ]; then
        print_success "Loading environment from $env_file file..."
        # Use safer approach to load env file
        set -a
        source "$env_file"
        set +a
        echo ""
    fi
}

# Display which API keys are available
display_api_keys() {
    echo "Available API keys:"
    if [ -n "$ANTHROPIC_API_KEY" ]; then
        print_success "  ANTHROPIC_API_KEY"
    fi
    if [ -n "$OPENAI_API_KEY" ]; then
        print_success "  OPENAI_API_KEY"
    fi
    if [ -n "$GEMINI_API_KEY" ]; then
        print_success "  GEMINI_API_KEY"
    fi
    if [ -n "$MISTRAL_API_KEY" ]; then
        print_success "  MISTRAL_API_KEY"
    fi
    if [ -n "$GROQ_API_KEY" ]; then
        print_success "  GROQ_API_KEY"
    fi
    if [ -n "$OPENROUTER_API_KEY" ]; then
        print_success "  OPENROUTER_API_KEY"
    fi
    if [ -n "$CEREBRAS_API_KEY" ]; then
        print_success "  CEREBRAS_API_KEY"
    fi
    if [ -n "$OLLAMA_HOST" ]; then
        print_success "  OLLAMA_HOST ($OLLAMA_HOST)"
    fi
    if [ -n "$PERPLEXITY_API_KEY" ]; then
        print_success "  PERPLEXITY_API_KEY"
    fi
    echo ""
}

# Clean workspace directory
# Usage: clean_workspace [workspace_dir]
clean_workspace() {
    local workspace_dir="${1:-workspace}"
    print_warning "Cleaning workspace for fresh start..."
    if [ -d "$workspace_dir" ]; then
        # Remove contents but keep directory so Docker bind mounts stay valid
        find "$workspace_dir" -mindepth 1 -exec rm -rf {} +
    else
        mkdir -p "$workspace_dir"
    fi
    mkdir -p "$workspace_dir"
    echo ""
}

# Copy baseline/template workspace if it exists
# Usage: copy_workspace_template [source_dir] [dest_dir]
copy_workspace_template() {
    local source_dir="${1:-workspace-template}"
    local dest_dir="${2:-workspace}"

    if [ -d "$source_dir" ]; then
        print_info "Copying baseline from $source_dir..."
        cp -R "$source_dir/." "$dest_dir/"
        echo ""
    fi
}

# Clean Docker containers
# Usage: clean_docker [compose_file]
clean_docker() {
    local compose_file="${1:-docker-compose.yml}"
    print_warning "Cleaning Docker containers..."
    if [ -f "$compose_file" ]; then
        docker-compose -f "$compose_file" down -v 2>/dev/null || true
    else
        docker-compose down -v 2>/dev/null || true
    fi
    echo ""
}

# Check for --clean or -c flag in arguments
should_clean() {
    for arg in "$@"; do
        if [ "$arg" == "--clean" ] || [ "$arg" == "-c" ]; then
            return 0
        fi
    done
    return 1
}

# Run Docker Compose with standard options
# Usage: run_docker_compose [compose_file] [service_name]
run_docker_compose() {
    local compose_file="${1:-docker-compose.yml}"
    local service_name="${2:-test-runner}"

    if [ -f "$compose_file" ]; then
        docker-compose -f "$compose_file" up --build --abort-on-container-exit --exit-code-from "$service_name"
    else
        docker-compose up --build --abort-on-container-exit --exit-code-from "$service_name"
    fi
}

# Run Docker Compose with debug logging enabled and capture logs on failure
# Usage: run_docker_compose_with_debug [compose_file] [service_name] [log_dir]
run_docker_compose_with_debug() {
    local compose_file="${1:-docker-compose.yml}"
    local service_name="${2:-test-runner}"
    local log_dir="${3:-.logs}"

    # Create log directory
    mkdir -p "$log_dir"

    # Set debug log level
    export STATCODE_LOG_LEVEL="debug"
    export STATCODE_LOG_PATH="/workspace/.logs/statcode-ai.log"

    # Run docker compose and capture exit code
    local exit_code=0
    if [ -f "$compose_file" ]; then
        docker-compose -f "$compose_file" up --build --abort-on-container-exit --exit-code-from "$service_name" || exit_code=$?
    else
        docker-compose up --build --abort-on-container-exit --exit-code-from "$service_name" || exit_code=$?
    fi

    # If test failed, display the debug log
    if [ $exit_code -ne 0 ]; then
        if [ -f "$log_dir/statcode-ai.log" ]; then
            echo ""
            print_header "$(print_color "$RED" "Debug Log Output (Test Failed)")"
            echo ""
            cat "$log_dir/statcode-ai.log"
            echo ""
        else
            print_warning "No debug log file found at $log_dir/statcode-ai.log"
        fi
    fi

    return $exit_code
}

# Print success summary for E2E test
# Usage: print_success_summary [output_path] [example_command]
print_success_summary() {
    local output_path="${1:-./workspace/calculator}"
    local example_command="${2:-./workspace/calculator \"8 * 10 + 2\"}"

    echo ""
    print_header "$(print_color "$GREEN" "✓ E2E Test PASSED!")"
    echo ""

    if [ -n "$output_path" ]; then
        echo "The generated output is available in:"
        echo "  $output_path"
        echo ""
    fi

    if [ -n "$example_command" ]; then
        echo "You can test it manually:"
        echo "  $example_command"
        echo ""
    fi
}

# Print failure summary for E2E test
# Usage: print_failure_summary
print_failure_summary() {
    echo ""
    print_header "$(print_color "$RED" "✗ E2E Test FAILED!")"
    echo ""
    echo "Check the logs above for details."
    echo "To clean up and retry:"
    echo "  ./run.sh --clean"
    echo ""
}

# Initialize E2E test environment
# This combines common initialization steps:
# 1. Check for --clean flag
# 2. Check API keys
# 3. Load .env file
# 4. Display available API keys
# 5. Clean workspace
# 6. Optionally copy template
# 7. Clean Docker if --clean flag is set
#
# Usage: init_e2e_test "$@"
init_e2e_test() {
    # Store original args for later use
    local args="$@"

    # Check and load environment
    if ! check_api_keys; then
        exit 1
    fi

    load_env_file
    display_api_keys

    # Clean workspace
    clean_workspace

    # Copy template if it exists (for upgrade scenarios)
    copy_workspace_template

    # Clean Docker if requested
    if should_clean "$@"; then
        clean_docker
    fi
}

# Run the complete E2E test flow
# This is a convenience function that runs the full test lifecycle
# Usage: run_e2e_test [output_path] [example_command] [compose_file] [service_name]
run_e2e_test_flow() {
    local output_path="${1:-./workspace/calculator}"
    local example_command="${2:-./workspace/calculator \"8 * 10 + 2\"}"
    local compose_file="${3:-docker-compose.yml}"
    local service_name="${4:-test-runner}"

    if run_docker_compose_with_debug "$compose_file" "$service_name" ".logs"; then
        print_success_summary "$output_path" "$example_command"
        exit 0
    else
        print_failure_summary
        exit 1
    fi
}
