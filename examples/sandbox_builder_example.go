package main

import (
	"context"
	"fmt"
	"log"

	"github.com/statcode-ai/statcode-ai/internal/tools"
)

func main() {
	// Example 1: Simple Hello World
	simpleExample()

	// Example 2: With timeout
	timeoutExample()

	// Example 3: Using Clone for batch execution
	batchExample()
}

func simpleExample() {
	fmt.Println("=== Example 1: Simple Hello World ===")

	code := `package main
import "fmt"
func main() {
	fmt.Println("Hello from SandboxBuilder!")
}`

	result, err := tools.NewSandboxBuilder().
		SetCode(code).
		SetTimeout(10).
		Execute(context.Background())

	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	// Parse result
	resultMap := result.(map[string]interface{})
	stdout := resultMap["stdout"].(string)
	exitCode := resultMap["exit_code"].(int)

	fmt.Printf("Exit Code: %d\n", exitCode)
	fmt.Printf("Output:\n%s\n", stdout)
}

func timeoutExample() {
	fmt.Println("\n=== Example 2: With Custom Timeout ===")

	code := `package main
import (
	"fmt"
	"time"
)
func main() {
	fmt.Println("Starting computation...")
	time.Sleep(2 * time.Second)
	fmt.Println("Computation complete!")
}`

	result, err := tools.NewSandboxBuilder().
		SetCode(code).
		SetTimeout(5). // 5 second timeout
		Execute(context.Background())

	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	resultMap := result.(map[string]interface{})
	stdout := resultMap["stdout"].(string)
	timedOut := resultMap["timeout"].(bool)

	if timedOut {
		fmt.Println("Execution timed out!")
	} else {
		fmt.Printf("Output:\n%s\n", stdout)
	}
}

func batchExample() {
	fmt.Println("\n=== Example 3: Batch Execution with Clone ===")

	// Create a base builder with common configuration
	baseBuilder := tools.NewSandboxBuilder().
		SetTimeout(10).
		SetWorkingDir(".")

	// Test cases
	testCases := []struct {
		name string
		code string
	}{
		{
			name: "Addition",
			code: `package main
import "fmt"
func main() {
	result := 2 + 2
	fmt.Printf("2 + 2 = %d\n", result)
}`,
		},
		{
			name: "String manipulation",
			code: `package main
import (
	"fmt"
	"strings"
)
func main() {
	s := "hello world"
	fmt.Println(strings.ToUpper(s))
}`,
		},
		{
			name: "Loop",
			code: `package main
import "fmt"
func main() {
	for i := 1; i <= 5; i++ {
		fmt.Printf("Count: %d\n", i)
	}
}`,
		},
	}

	// Execute each test case
	for _, tc := range testCases {
		fmt.Printf("\nTest: %s\n", tc.name)

		// Clone the base builder and set the test code
		result, err := baseBuilder.Clone().
			SetCode(tc.code).
			Execute(context.Background())

		if err != nil {
			fmt.Printf("  Error: %v\n", err)
			continue
		}

		resultMap := result.(map[string]interface{})
		stdout := resultMap["stdout"].(string)
		exitCode := resultMap["exit_code"].(int)

		if exitCode == 0 {
			fmt.Printf("  ✓ Success\n")
			fmt.Printf("  Output: %s", stdout)
		} else {
			stderr := resultMap["stderr"].(string)
			fmt.Printf("  ✗ Failed (exit code %d)\n", exitCode)
			fmt.Printf("  Error: %s\n", stderr)
		}
	}
}
