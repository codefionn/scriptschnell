# Summarize Function Example

The `go_sandbox` tool now includes a built-in `Summarize` function that provides access to the fast summarization LLM model.

## Function Signature

```go
func Summarize(prompt, text string) string
```

## Parameters

- `prompt` - Instructions for what to extract or how to summarize
- `text` - The text to be summarized

## Returns

- `summary` - The summarized result based on your prompt

## Examples

### Extract Key Points

```go
package main

import "fmt"

func main() {
	longArticle := `
	Artificial Intelligence (AI) has transformed many industries...
	[very long text]
	`

	summary := Summarize("Extract the 3 main points as bullet points", longArticle)
	fmt.Println(summary)
}
```

### Answer Specific Questions

```go
package main

import "fmt"

func main() {
	logData := `
	2024-01-01 10:23:45 INFO: Server started
	2024-01-01 10:24:12 ERROR: Database connection failed
	2024-01-01 10:24:15 WARN: Retrying connection
	2024-01-01 10:24:18 ERROR: Connection timeout
	2024-01-01 10:25:00 INFO: Fallback to cached data
	`

	summary := Summarize("How many errors occurred and what were they?", logData)
	fmt.Println(summary)
}
```

### Extract Structured Data

```go
package main

import "fmt"

func main() {
	readme := `
	# MyProject

	A tool for data processing.

	## Installation
	npm install myproject

	## Usage
	myproject --input file.csv
	`

	summary := Summarize("List only the installation command", readme)
	fmt.Println(summary)
	// Output: npm install myproject
}
```

### Combine with Fetch

```go
package main

import "fmt"

func main() {
	// Fetch some data
	data, status := Fetch("GET", "https://example.com/api/data", "")

	if status == 200 {
		// Summarize the response
		summary := Summarize("Extract the user count and status", data)
		fmt.Printf("Summary: %s\n", summary)
	}
}
```

### Combine with Shell

```go
package main

import "fmt"

func main() {
	// Get system information
	out, _, _ := Shell("df -h")

	// Summarize disk usage
	summary := Summarize("Which partition has the most free space?", out)
	fmt.Printf("Result: %s\n", summary)
}
```

### Process Multiple Items

```go
package main

import "fmt"

func main() {
	reviews := []string{
		"Great product! Works perfectly. 5 stars.",
		"Terrible quality. Broke after one use.",
		"Good value for money. Recommended.",
		"Average. Nothing special but does the job.",
	}

	for i, review := range reviews {
		sentiment := Summarize("Is this review positive or negative? Answer in one word.", review)
		fmt.Printf("Review %d: %s\n", i+1, sentiment)
	}
}
```

### Extract Function Names from Code

```go
package main

import "fmt"

func main() {
	sourceCode := `
	func ProcessData(input string) error {
		return nil
	}

	func ValidateInput(data []byte) bool {
		return true
	}

	func FormatOutput(result interface{}) string {
		return ""
	}
	`

	functions := Summarize("List all function names, one per line", sourceCode)
	fmt.Println("Functions found:")
	fmt.Println(functions)
}
```

### Analyze Configuration

```go
package main

import "fmt"

func main() {
	config := Shell("cat /etc/nginx/nginx.conf")
	out, _, _ := config

	analysis := Summarize("What port is the server listening on?", out)
	fmt.Printf("Port: %s\n", analysis)
}
```

## Use Cases

The Summarize function is perfect for:

1. **Log Analysis**: Extract errors, warnings, or specific events from logs
2. **Data Extraction**: Pull specific information from large documents
3. **Code Analysis**: List functions, classes, or dependencies
4. **Configuration Review**: Extract specific settings from config files
5. **API Response Processing**: Get specific fields from JSON responses
6. **Documentation Parsing**: Extract installation steps, usage examples
7. **Sentiment Analysis**: Determine tone or sentiment from text
8. **Content Filtering**: Extract relevant information based on criteria

## Features

- **Fast Model**: Uses a lightweight, fast summarization model (e.g., Claude Haiku)
- **No Authorization**: No approval needed - runs immediately
- **Flexible**: Works with any text input and custom prompts
- **Large Context**: Can handle up to 1MB of input text
- **Automatic**: No need to import anything - function is always available

## Error Handling

```go
package main

import (
	"fmt"
	"strings"
)

func main() {
	text := "Some text to summarize"
	result := Summarize("Summarize this", text)

	// Check for errors (errors start with "Error:")
	if strings.HasPrefix(result, "Error:") {
		fmt.Printf("Summarization failed: %s\n", result)
		return
	}

	fmt.Printf("Summary: %s\n", result)
}
```

## Limitations

- Maximum text size: 1MB
- Maximum result size: 1MB
- Requires summarization LLM to be configured
- Uses the fast summarization model (not the main orchestration model)

## Note

The `Summarize` function is automatically available in your code - no imports needed!
