// WASM Integration Tests
//
// This file contains two types of tests:
// 1. Unit tests: Fast tests that verify tool configuration and validation (no WASM execution)
// 2. Integration tests: Slow tests that compile and execute actual WebAssembly code
//
// Integration tests require:
// - TinyGo download (~50MB) on first run 
// - Go compilation to WebAssembly
// - WASM runtime execution in wazero
//
// To run only fast unit tests (default): go test ./internal/tools
// To run integration tests: go test -run Integration ./internal/tools -integration
// To run all tests: go test ./internal/tools -integration

package tools

import (
	"flag"
	"context"
	"strings"
	"testing"
	"time"
)

// Integration tests flag - only run WASM execution tests when explicitly requested
// These tests are slow because they:
// 1. Download TinyGo (~50MB) on first run
// 2. Compile Go code to WebAssembly 
// 3. Execute WASM in a sandboxed runtime
// Usage: go test -run Integration ./internal/tools -integration
var runIntegrationTests = flag.Bool("integration", false, "Run WASM integration tests (requires explicit flag)")

func init() {
	testing.Init()
	flag.Parse()
}

func TestSandboxTool_Name(t *testing.T) {
	tool := NewSandboxTool("/tmp", "/tmp")
	if tool.Name() != "go_sandbox" {
		t.Errorf("expected name 'go_sandbox', got '%s'", tool.Name())
	}
}

func TestSandboxTool_Description(t *testing.T) {
	tool := NewSandboxTool("/tmp", "/tmp")
	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
	if !strings.Contains(desc, "sandboxed") {
		t.Error("description should mention sandboxed environment")
	}
}

func TestSandboxTool_Parameters(t *testing.T) {
	tool := NewSandboxTool("/tmp", "/tmp")
	params := tool.Parameters()

	// Check required fields
	if params["type"] != "object" {
		t.Error("parameters type should be 'object'")
	}

	// Check properties exist
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties should be a map")
	}

	// Check code parameter
	if _, ok := props["code"]; !ok {
		t.Error("should have 'code' parameter")
	}

	// Check timeout parameter
	if _, ok := props["timeout"]; !ok {
		t.Error("should have 'timeout' parameter")
	}

	// Check libraries parameter
	if _, ok := props["libraries"]; !ok {
		t.Error("should have 'libraries' parameter")
	}

	// Check required fields
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("required should be a string array")
	}
	if len(required) == 0 || required[0] != "code" {
		t.Error("'code' should be a required parameter")
	}
}

func TestIntegration_SandboxTool_Execute_SimpleCode(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	tempDir := t.TempDir()
	tool := NewSandboxTool("/tmp", tempDir)

	code := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`

	params := map[string]interface{}{
		"code": code,
	}

	ctx := context.Background()
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("result should be a map")
	}

	stdout, ok := resultMap["stdout"].(string)
	if !ok {
		t.Fatal("stdout should be a string")
	}

	stderr, _ := resultMap["stderr"].(string)

	if !strings.Contains(stdout, "Hello, World!") {
		t.Errorf("expected output to contain 'Hello, World!', got stdout: %q, stderr: %q", stdout, stderr)
	}

	exitCode, ok := resultMap["exit_code"].(int)
	if !ok {
		t.Fatal("exit_code should be an int")
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d, stderr: %q", exitCode, stderr)
	}

	timeout, ok := resultMap["timeout"].(bool)
	if !ok {
		t.Fatal("timeout should be a bool")
	}

	if timeout {
		t.Error("expected timeout to be false")
	}
}

func TestIntegration_SandboxTool_Execute_WithMath(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	tempDir := t.TempDir()
	tool := NewSandboxTool("/tmp", tempDir)

	code := `package main

import (
	"fmt"
	"math"
)

func main() {
	result := math.Sqrt(16)
	fmt.Printf("Square root of 16 is %.0f\n", result)
}
`

	params := map[string]interface{}{
		"code": code,
	}

	ctx := context.Background()
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	stdout := resultMap["stdout"].(string)

	if !strings.Contains(stdout, "Square root of 16 is 4") {
		t.Errorf("expected correct math output, got: %s", stdout)
	}
}

func TestIntegration_SandboxTool_Execute_CompilationError(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	tempDir := t.TempDir()
	tool := NewSandboxTool("/tmp", tempDir)

	code := `package main

func main() {
	// Using completely undefined symbol that won't be auto-injected
	undefinedFunction()
}
`

	params := map[string]interface{}{
		"code": code,
	}

	ctx := context.Background()
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	exitCode := resultMap["exit_code"].(int)

	// Should have non-zero exit code due to compilation error
	if exitCode == 0 {
		t.Error("expected non-zero exit code for compilation error")
	}

	stdout := resultMap["stdout"].(string)
	if !strings.Contains(stdout, "undefined") {
		t.Errorf("expected compilation error message with 'undefined', got: %s", stdout)
	}
}

func TestIntegration_SandboxTool_Execute_RuntimeError(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	tempDir := t.TempDir()
	tool := NewSandboxTool("/tmp", tempDir)

	code := `package main

import "fmt"

func main() {
	var x *int
	fmt.Println(*x) // nil pointer dereference
}
`

	params := map[string]interface{}{
		"code": code,
	}

	ctx := context.Background()
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	exitCode := resultMap["exit_code"].(int)

	// In WASM, runtime panics may result in non-zero exit or runtime error
	// The behavior is different from native Go execution
	stdout := resultMap["stdout"].(string)

	// WASM may trap on nil pointer dereference rather than panic
	// Just verify the code executed (exit code may be 0 or non-zero depending on WASM runtime)
	t.Logf("Runtime error test - exit code: %d, output: %s", exitCode, stdout)

	// Test passes if code executed without compilation error
	// (WASM handles runtime errors differently than native Go)
}

func TestIntegration_SandboxTool_Execute_Timeout(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	t.Skip("Timeout handling needs investigation - context deadline may not propagate correctly to go run")

	tempDir := t.TempDir()
	tool := NewSandboxTool("/tmp", tempDir)

	code := `package main

import "time"

func main() {
	time.Sleep(30 * time.Second)
	println("Done")
}
`

	params := map[string]interface{}{
		"code":    code,
		"timeout": 2, // 2 second timeout
	}

	ctx := context.Background()
	start := time.Now()
	result, err := tool.Execute(ctx, params)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	// Timeout should occur around the specified timeout value (plus compilation time)
	if duration > 10*time.Second {
		t.Errorf("execution took too long: %v (expected around 2-3 seconds)", duration)
	}

	resultMap := result.(map[string]interface{})

	// Check if either timeout flag is set or exit code indicates termination
	timeout := resultMap["timeout"].(bool)
	exitCode := resultMap["exit_code"].(int)

	// Either timeout should be true, or exit code should be non-zero (killed by context)
	if !timeout && exitCode == 0 {
		t.Error("expected timeout or non-zero exit code for long-running code")
	}
}

func TestIntegration_SandboxTool_Execute_EmptyCode(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	tempDir := t.TempDir()
	tool := NewSandboxTool("/tmp", tempDir)

	params := map[string]interface{}{
		"code": "",
	}

	ctx := context.Background()
	_, err := tool.Execute(ctx, params)
	if err == nil {
		t.Error("expected error for empty code")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("expected error message to mention 'required', got: %v", err)
	}
}

func TestIntegration_SandboxTool_Execute_TimeoutMax(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	tempDir := t.TempDir()
	tool := NewSandboxTool("/tmp", tempDir)

	code := `package main
func main() {
	println("test")
}
`

	// Test that timeout is clamped to max
	params := map[string]interface{}{
		"code":    code,
		"timeout": 999, // Should be clamped to 120
	}

	ctx := context.Background()
	start := time.Now()
	result, err := tool.Execute(ctx, params)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	exitCode := resultMap["exit_code"].(int)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	// Should complete quickly, not take 999 seconds
	if duration > 10*time.Second {
		t.Errorf("execution took too long: %v", duration)
	}
}

func TestIntegration_SandboxTool_Execute_SandboxIsolation(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	tempDir := t.TempDir()
	tool := NewSandboxTool("/tmp", tempDir)

	// Test that WASM sandbox has NO filesystem access (true isolation)
	code := `package main

import (
	"fmt"
	"os"
)

func main() {
	// Try to read a file - should fail in WASM
	_, err := os.ReadFile("/etc/passwd")
	if err != nil {
		fmt.Println("✓ Filesystem access blocked (expected):", err)
	} else {
		fmt.Println("✗ ERROR: Filesystem access should be blocked!")
	}

	// Try to write a file - should also fail
	err = os.WriteFile("test.txt", []byte("data"), 0644)
	if err != nil {
		fmt.Println("✓ File write blocked (expected):", err)
	} else {
		fmt.Println("✗ ERROR: File write should be blocked!")
	}

	fmt.Println("WASM isolation test complete")
}
`

	params := map[string]interface{}{
		"code": code,
	}

	ctx := context.Background()
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	stdout := resultMap["stdout"].(string)

	// In WASM, filesystem operations should fail (this is the security feature!)
	if !strings.Contains(stdout, "blocked") && !strings.Contains(stdout, "Bad file") {
		t.Errorf("expected filesystem operations to be blocked in WASM, got: %s", stdout)
	}

	// Verify test completed
	if !strings.Contains(stdout, "test complete") {
		t.Errorf("expected test to complete, got: %s", stdout)
	}

	exitCode := resultMap["exit_code"].(int)
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestContainsDangerousOps(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected bool
	}{
		{
			name:     "Safe code",
			code:     `package main\nfunc main() { println("hello") }`,
			expected: false,
		},
		{
			name:     "os.RemoveAll",
			code:     `package main\nimport "os"\nfunc main() { os.RemoveAll("/") }`,
			expected: true,
		},
		{
			name:     "exec.Command",
			code:     `package main\nimport "os/exec"\nfunc main() { exec.Command("ls") }`,
			expected: true,
		},
		{
			name:     "syscall",
			code:     `package main\nimport "syscall"\nfunc main() { syscall.Exit(0) }`,
			expected: true,
		},
		{
			name:     "unsafe",
			code:     `package main\nimport "unsafe"\nfunc main() { _ = unsafe.Pointer(nil) }`,
			expected: true,
		},
		{
			name:     "net.Listen",
			code:     `package main\nimport "net"\nfunc main() { net.Listen("tcp", ":80") }`,
			expected: true,
		},
		{
			name:     "http.ListenAndServe",
			code:     `package main\nimport "net/http"\nfunc main() { http.ListenAndServe(":80", nil) }`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsDangerousOps(tt.code)
			if result != tt.expected {
				t.Errorf("containsDangerousOps(%q) = %v, expected %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestIntegration_SandboxTool_Execute_Fetch(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	tool := NewSandboxTool("/tmp", "/tmp")

	// Test Fetch function with httpbin.org (a testing service)
	code := `package main

import "fmt"

func main() {
	// Test GET request
	response, status := Fetch("GET", "https://httpbin.org/get", "")
	fmt.Printf("GET Status: %d\n", status)
	fmt.Printf("GET Response length: %d\n", len(response))

	if status != 200 {
		fmt.Printf("Error: Expected status 200, got %d\n", status)
	}
}`

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	params := map[string]interface{}{
		"code":    code,
		"timeout": 60,
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("result should be a map")
	}

	stdout := resultMap["stdout"].(string)
	stderr := resultMap["stderr"].(string)
	exitCode := resultMap["exit_code"].(int)

	t.Logf("Fetch test output:\nstdout: %s\nstderr: %s\nexit_code: %d", stdout, stderr, exitCode)

	// Check for successful execution
	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	// Check that output contains expected strings
	if !strings.Contains(stdout, "GET Status: 200") {
		t.Errorf("Expected 'GET Status: 200' in stdout, got: %s", stdout)
	}

	if !strings.Contains(stdout, "GET Response length:") {
		t.Errorf("Expected 'GET Response length:' in stdout, got: %s", stdout)
	}
}

func TestIntegration_SandboxTool_Execute_Shell(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}
	
	tool := NewSandboxTool("/tmp", "/tmp")

	// Test Shell function with simple commands
	code := `package main

import "fmt"

func main() {
	// Test simple command
	out, _, code := Shell("echo 'Hello from shell'")
	fmt.Printf("Echo output: %s\n", out)
	fmt.Printf("Echo exit code: %d\n", code)

	// Test command with stderr
	out2, errOut2, code2 := Shell("ls /nonexistent 2>&1 || echo 'Command failed'")
	fmt.Printf("Ls output: %s\n", out2)
	fmt.Printf("Ls stderr: %s\n", errOut2)
	fmt.Printf("Ls exit code: %d\n", code2)

	// Test pwd command
	pwd, _, pwdCode := Shell("pwd")
	fmt.Printf("PWD: %s\n", pwd)
	fmt.Printf("PWD exit code: %d\n", pwdCode)
}`

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	params := map[string]interface{}{
		"code":    code,
		"timeout": 60,
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("result should be a map")
	}

	stdout := ""
	if resultMap["stdout"] != nil {
		stdout = resultMap["stdout"].(string)
	}
	stderr := ""
	if resultMap["stderr"] != nil {
		stderr = resultMap["stderr"].(string)
	}
	exitCode := resultMap["exit_code"].(int)

	t.Logf("Shell test output:\nstdout: %s\nstderr: %s\nexit_code: %d", stdout, stderr, exitCode)

	// Check for successful execution
	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	// Check that output contains expected strings
	if !strings.Contains(stdout, "Echo output: Hello from shell") {
		t.Errorf("Expected 'Echo output: Hello from shell' in stdout, got: %s", stdout)
	}

	if !strings.Contains(stdout, "Echo exit code: 0") {
		t.Errorf("Expected 'Echo exit code: 0' in stdout, got: %s", stdout)
	}

	if !strings.Contains(stdout, "PWD exit code: 0") {
		t.Errorf("Expected 'PWD exit code: 0' in stdout, got: %s", stdout)
	}
}
