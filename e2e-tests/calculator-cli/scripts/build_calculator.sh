#!/bin/bash
set -e

echo "=========================================="
echo "Building Calculator CLI with statcode-ai"
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

# Use statcode-ai to build the calculator CLI
# The --dangerous-allow-all flag bypasses authorization prompts for the e2e test
echo "Running statcode-ai to create calculator CLI..."
statcode-ai --dangerous-allow-all "Use the create_file tool to create main.go with this Go calculator implementation:

package main

import (
    \"fmt\"
    \"os\"
    \"strconv\"
    \"strings\"
)

func main() {
    if len(os.Args) != 2 {
        fmt.Fprintln(os.Stderr, \"Usage: calculator <expression>\")
        os.Exit(1)
    }

    expr := os.Args[1]
    result, err := evaluate(expr)
    if err != nil {
        fmt.Fprintln(os.Stderr, \"Error:\", err)
        os.Exit(1)
    }

    fmt.Println(result)
}

// Implement a recursive descent parser for math expressions with +, -, *, /, and parentheses
// Handle operator precedence correctly

func evaluate(expr string) (float64, error) {
    // Remove whitespace
    expr = strings.ReplaceAll(expr, \" \", \"\")

    p := &parser{input: expr, pos: 0}
    result, err := p.parseExpression()
    if err != nil {
        return 0, err
    }

    if p.pos < len(p.input) {
        return 0, fmt.Errorf(\"unexpected character at position %d\", p.pos)
    }

    return result, nil
}

type parser struct {
    input string
    pos   int
}

func (p *parser) parseExpression() (float64, error) {
    left, err := p.parseTerm()
    if err != nil {
        return 0, err
    }

    for p.pos < len(p.input) {
        op := p.input[p.pos]
        if op != '+' && op != '-' {
            break
        }
        p.pos++

        right, err := p.parseTerm()
        if err != nil {
            return 0, err
        }

        if op == '+' {
            left += right
        } else {
            left -= right
        }
    }

    return left, nil
}

func (p *parser) parseTerm() (float64, error) {
    left, err := p.parseFactor()
    if err != nil {
        return 0, err
    }

    for p.pos < len(p.input) {
        op := p.input[p.pos]
        if op != '*' && op != '/' {
            break
        }
        p.pos++

        right, err := p.parseFactor()
        if err != nil {
            return 0, err
        }

        if op == '*' {
            left *= right
        } else {
            if right == 0 {
                return 0, fmt.Errorf(\"division by zero\")
            }
            left /= right
        }
    }

    return left, nil
}

func (p *parser) parseFactor() (float64, error) {
    if p.pos >= len(p.input) {
        return 0, fmt.Errorf(\"unexpected end of expression\")
    }

    // Handle parentheses
    if p.input[p.pos] == '(' {
        p.pos++
        result, err := p.parseExpression()
        if err != nil {
            return 0, err
        }

        if p.pos >= len(p.input) || p.input[p.pos] != ')' {
            return 0, fmt.Errorf(\"missing closing parenthesis\")
        }
        p.pos++

        return result, nil
    }

    // Parse number
    return p.parseNumber()
}

func (p *parser) parseNumber() (float64, error) {
    start := p.pos

    // Handle negative numbers
    if p.pos < len(p.input) && p.input[p.pos] == '-' {
        p.pos++
    }

    // Parse digits before decimal
    for p.pos < len(p.input) && (p.input[p.pos] >= '0' && p.input[p.pos] <= '9') {
        p.pos++
    }

    // Parse decimal part
    if p.pos < len(p.input) && p.input[p.pos] == '.' {
        p.pos++
        for p.pos < len(p.input) && (p.input[p.pos] >= '0' && p.input[p.pos] <= '9') {
            p.pos++
        }
    }

    if start == p.pos {
        return 0, fmt.Errorf(\"expected number at position %d\", p.pos)
    }

    numStr := p.input[start:p.pos]
    num, err := strconv.ParseFloat(numStr, 64)
    if err != nil {
        return 0, fmt.Errorf(\"invalid number: %s\", numStr)
    }

    return num, nil
}

After creating main.go, use the shell tool to run: go build -o calculator ."

# Check if main.go was created
if [ ! -f "main.go" ]; then
    echo "ERROR: main.go was not created"
    exit 1
fi

# Build the calculator
echo "Building calculator binary..."
go build -o calculator .

if [ ! -f "calculator" ]; then
    echo "ERROR: calculator binary was not created"
    exit 1
fi

echo "=========================================="
echo "Calculator CLI built successfully!"
echo "=========================================="
