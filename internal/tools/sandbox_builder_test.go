package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestSandboxBuilder_BasicUsage(t *testing.T) {
	code := `package main
import "fmt"
func main() {
	message := "Hello from builder!"
	fmt.Println(message)
}`

	builder := NewSandboxBuilder().
		SetCode(code).
		SetTimeout(10)

	// Validate builder
	if err := builder.Validate(); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// Build sandbox
	tool, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}
}

func TestSandboxBuilder_SetCode(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		expectErr bool
	}{
		{
			name:      "valid code",
			code:      "package main\nfunc main() {}",
			expectErr: false,
		},
		{
			name:      "empty code",
			code:      "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewSandboxBuilder().SetCode(tt.code)
			err := builder.Validate()

			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestSandboxBuilder_AddLibrary(t *testing.T) {
	builder := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AddLibrary("github.com/foo/bar@v1.0.0").
		AddLibrary("github.com/baz/qux@v2.1.0")

	if len(builder.libraries) != 2 {
		t.Errorf("Expected 2 libraries, got %d", len(builder.libraries))
	}

	// Test empty library is rejected
	builder2 := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AddLibrary("")

	if builder2.err == nil {
		t.Error("Expected error for empty library")
	}
}

func TestSandboxBuilder_AddLibraries(t *testing.T) {
	builder := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AddLibraries(
			"github.com/foo/bar@v1.0.0",
			"github.com/baz/qux@v2.1.0",
			"github.com/test/pkg@v3.0.0",
		)

	if len(builder.libraries) != 3 {
		t.Errorf("Expected 3 libraries, got %d", len(builder.libraries))
	}

	// Verify empty strings are filtered out
	builder2 := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AddLibraries("lib1", "", "lib2")

	if len(builder2.libraries) != 2 {
		t.Errorf("Expected 2 libraries (empty filtered), got %d", len(builder2.libraries))
	}
}

func TestSandboxBuilder_SetTimeout(t *testing.T) {
	tests := []struct {
		name      string
		timeout   int
		expectErr bool
	}{
		{"valid timeout", 30, false},
		{"min timeout", 1, false},
		{"max timeout", 600, false},
		{"zero timeout", 0, true},
		{"negative timeout", -1, true},
		{"exceeds max", 601, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewSandboxBuilder().
				SetCode("package main\nfunc main() {}").
				SetTimeout(tt.timeout)

			err := builder.Validate()
			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.expectErr && builder.timeout != tt.timeout {
				t.Errorf("Expected timeout %d, got %d", tt.timeout, builder.timeout)
			}
		})
	}
}

func TestSandboxBuilder_SetFilesystem(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", "/tmp")

	builder := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		SetFilesystem(mockFS).
		SetSession(sess)

	tool, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}
}

func TestSandboxBuilder_SetAuthorization(t *testing.T) {
	// Create a mock authorizer
	mockAuth := &mockAuthorizer{}

	builder := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		SetAuthorization(mockAuth)

	tool, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}
}

func TestSandboxBuilder_AllowDomain(t *testing.T) {
	builder := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AllowDomain("github.com").
		AllowDomain("googleapis.com")

	if len(builder.allowDomains) != 2 {
		t.Errorf("Expected 2 allowed domains, got %d", len(builder.allowDomains))
	}

	// Test empty domain is rejected
	builder2 := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AllowDomain("")

	if builder2.err == nil {
		t.Error("Expected error for empty domain")
	}
}

func TestSandboxBuilder_AllowDomains(t *testing.T) {
	builder := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AllowDomains("github.com", "googleapis.com", "npmjs.org")

	if len(builder.allowDomains) != 3 {
		t.Errorf("Expected 3 allowed domains, got %d", len(builder.allowDomains))
	}

	// Test empty strings are filtered
	builder2 := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AllowDomains("domain1", "", "domain2")

	if len(builder2.allowDomains) != 2 {
		t.Errorf("Expected 2 domains (empty filtered), got %d", len(builder2.allowDomains))
	}
}

func TestSandboxBuilder_AllowAllDomains(t *testing.T) {
	builder := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AllowAllDomains()

	if !builder.allowAll {
		t.Error("Expected allowAll to be true")
	}
}

func TestSandboxBuilder_Validate(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() *SandboxBuilder
		expectErr bool
	}{
		{
			name: "valid minimal config",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode("package main\nfunc main() {}")
			},
			expectErr: false,
		},
		{
			name: "missing code",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder()
			},
			expectErr: true,
		},
		{
			name: "invalid timeout propagates",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().
					SetCode("package main\nfunc main() {}").
					SetTimeout(-1)
			},
			expectErr: true,
		},
		{
			name: "exec.Command usage rejected",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode(`package main
import "os/exec"
func main() {
	exec.Command("ls")
}`)
			},
			expectErr: true,
		},
		{
			name: "trivial code - only imports and comments",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode(`package main

// This is a comment
import "fmt"
import "strings"

// Another comment
/* Multi-line comment */
`)
			},
			expectErr: true,
		},
		{
			name: "trivial code - only fmt prints",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode(`package main

import "fmt"

func main() {
	fmt.Println("Hello World")
	fmt.Print("Testing")
	fmt.Printf("Format %s", "test")
}
`)
			},
			expectErr: true,
		},
		{
			name: "valid code - fmt print with function call",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode(`package main

// This is a test program
import "fmt"
import "time" // for time stuff

/* Main function with just prints */
func main() {
	fmt.Println("Starting...") // Start message
	fmt.Printf("Time: %v\n", time.Now())
	fmt.Print("Done.")
}
`)
			},
			expectErr: false,
		},
		{
			name: "trivial code - mixed imports, comments and only fmt literals",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode(`package main

// This is a test program
import "fmt"
import "strings" // for strings stuff

/* Main function with just string literals */
func main() {
	fmt.Println("Starting...") // Start message
	fmt.Printf("Format: %s\n", "literal")
	fmt.Print("Done.")
}
`)
			},
			expectErr: true,
		},
		{
			name: "valid code - with variable declaration",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode(`package main

import "fmt"

func main() {
	name := "test"
	fmt.Println(name)
}
`)
			},
			expectErr: false,
		},
		{
			name: "valid code - with if statement",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode(`package main

import "fmt"

func main() {
	if true {
		fmt.Println("Hello")
	}
}
`)
			},
			expectErr: false,
		},
		{
			name: "valid code - with function call",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode(`package main

import "fmt"

func helper() string {
	return "hello"
}

func main() {
	result := helper()
	fmt.Println(result)
}
`)
			},
			expectErr: false,
		},
		{
			name: "valid code - with loop",
			setup: func() *SandboxBuilder {
				return NewSandboxBuilder().SetCode(`package main

import "fmt"

func main() {
	for i := 0; i < 3; i++ {
		fmt.Println(i)
	}
}
`)
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := tt.setup()
			err := builder.Validate()

			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestSandboxBuilder_Build(t *testing.T) {
	t.Run("minimal build", func(t *testing.T) {
		builder := NewSandboxBuilder().
			SetCode("package main\nfunc main() {}")

		tool, err := builder.Build()
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		if tool == nil {
			t.Fatal("Expected non-nil tool")
		}
	})

	t.Run("build with filesystem and session", func(t *testing.T) {
		mockFS := fs.NewMockFS()
		sess := session.NewSession("test", "/tmp")

		builder := NewSandboxBuilder().
			SetCode("package main\nfunc main() {}").
			SetFilesystem(mockFS).
			SetSession(sess)

		tool, err := builder.Build()
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		if tool == nil {
			t.Fatal("Expected non-nil tool")
		}
	})

	t.Run("build fails with validation error", func(t *testing.T) {
		builder := NewSandboxBuilder().
			SetTimeout(-1) // Invalid

		_, err := builder.Build()
		if err == nil {
			t.Error("Expected build to fail with validation error")
		}
	})
}

func TestSandboxBuilder_Reset(t *testing.T) {
	builder := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AddLibrary("github.com/foo/bar@v1.0.0").
		SetTimeout(60).
		AllowDomain("github.com")

	// Reset should clear code, libraries, timeout (to default), and domains
	builder.Reset()

	if builder.code != "" {
		t.Error("Expected code to be cleared")
	}
	if len(builder.libraries) != 0 {
		t.Error("Expected libraries to be cleared")
	}
	if builder.timeout != 30 {
		t.Errorf("Expected timeout to be reset to 30, got %d", builder.timeout)
	}
	if builder.allowDomains != nil {
		t.Error("Expected allowDomains to be nil")
	}
	if builder.allowAll {
		t.Error("Expected allowAll to be false")
	}
}

func TestSandboxBuilder_Clone(t *testing.T) {
	original := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AddLibrary("github.com/foo/bar@v1.0.0").
		SetTimeout(60).
		AllowDomain("github.com")

	clone := original.Clone()

	// Verify clone has same values
	if clone.code != original.code {
		t.Error("Clone should have same code")
	}
	if clone.timeout != original.timeout {
		t.Error("Clone should have same timeout")
	}
	if len(clone.libraries) != len(original.libraries) {
		t.Error("Clone should have same number of libraries")
	}
	if len(clone.allowDomains) != len(original.allowDomains) {
		t.Error("Clone should have same number of allowed domains")
	}

	// Verify modifying clone doesn't affect original
	clone.SetCode("different code")
	if original.code == clone.code {
		t.Error("Modifying clone should not affect original")
	}

	clone.AddLibrary("new library")
	if len(original.libraries) == len(clone.libraries) {
		t.Error("Adding library to clone should not affect original")
	}
}

func TestSandboxBuilder_FluentInterface(t *testing.T) {
	// Test that all methods return *SandboxBuilder for chaining
	builder := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AddLibrary("github.com/foo/bar@v1.0.0").
		AddLibraries("lib1", "lib2").
		SetTimeout(45).
		SetWorkingDir("/tmp").
		SetTempDir("/tmp/sandbox").
		AllowDomain("github.com").
		AllowDomains("googleapis.com", "npmjs.org")

	if builder == nil {
		t.Fatal("Expected fluent interface to return builder")
	}

	// Verify all values were set
	if builder.code == "" {
		t.Error("Code should be set")
	}
	if len(builder.libraries) != 3 {
		t.Errorf("Expected 3 libraries, got %d", len(builder.libraries))
	}
	if builder.timeout != 45 {
		t.Errorf("Expected timeout 45, got %d", builder.timeout)
	}
	if len(builder.allowDomains) != 3 {
		t.Errorf("Expected 3 allowed domains, got %d", len(builder.allowDomains))
	}
}

func TestSandboxBuilder_ErrorPropagation(t *testing.T) {
	// Once an error occurs, subsequent calls should not overwrite it
	builder := NewSandboxBuilder().
		SetTimeout(-1). // First error
		SetCode("")     // Second error (should not overwrite)

	if builder.err == nil {
		t.Fatal("Expected error to be set")
	}

	// Error message should be about timeout, not code
	if !strings.Contains(builder.err.Error(), "timeout") {
		t.Errorf("Expected first error (timeout) to be preserved, got: %v", builder.err)
	}

	// Build should fail with the first error
	_, err := builder.Build()
	if err == nil {
		t.Fatal("Expected build to fail")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Expected build error to contain first error, got: %v", err)
	}
}

func TestSandboxBuilder_Defaults(t *testing.T) {
	builder := NewSandboxBuilder()

	if builder.timeout != 30 {
		t.Errorf("Expected default timeout 30, got %d", builder.timeout)
	}
	if builder.workingDir != "." {
		t.Errorf("Expected default workingDir '.', got %s", builder.workingDir)
	}
	if builder.tempDir != "/tmp" {
		t.Errorf("Expected default tempDir '/tmp', got %s", builder.tempDir)
	}
	if len(builder.libraries) != 0 {
		t.Error("Expected empty libraries slice")
	}
	if builder.allowAll {
		t.Error("Expected allowAll to be false by default")
	}
}

// Mock authorizer for testing
type mockAuthorizer struct{}

func (m *mockAuthorizer) Authorize(ctx context.Context, toolName string, params map[string]interface{}) (*AuthorizationDecision, error) {
	return &AuthorizationDecision{
		Allowed: true,
		Reason:  "mock authorization",
	}, nil
}

// Benchmark tests
func BenchmarkSandboxBuilder_Build(b *testing.B) {
	code := "package main\nfunc main() {}"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builder := NewSandboxBuilder().SetCode(code)
		_, _ = builder.Build()
	}
}

func BenchmarkSandboxBuilder_Clone(b *testing.B) {
	original := NewSandboxBuilder().
		SetCode("package main\nfunc main() {}").
		AddLibrary("github.com/foo/bar@v1.0.0").
		SetTimeout(60)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = original.Clone()
	}
}
