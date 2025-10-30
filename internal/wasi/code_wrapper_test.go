package wasi

import (
	"strings"
	"testing"
)

func TestExtractImportsAndCode(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedImports []string
		expectedCode    string
	}{
		{
			name: "full program with imports",
			input: `package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("Hello, World!")
}`,
			expectedImports: []string{`"fmt"`, `"time"`},
			expectedCode: `func main() {
	fmt.Println("Hello, World!")
}`,
		},
		{
			name: "single import",
			input: `package main
import "fmt"

func main() {
	fmt.Println("Hello")
}`,
			expectedImports: []string{`"fmt"`},
			expectedCode: `func main() {
	fmt.Println("Hello")
}`,
		},
		{
			name: "no imports",
			input: `package main

func main() {
	println("Hello")
}`,
			expectedImports: []string{},
			expectedCode: `func main() {
	println("Hello")
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imports, code := extractImportsAndCode(tt.input)
			code = strings.TrimSpace(code)
			expectedCode := strings.TrimSpace(tt.expectedCode)

			if code != expectedCode {
				t.Errorf("extractImportsAndCode() code mismatch:\nGot:\n%q\n\nExpected:\n%q", code, expectedCode)
			}

			if len(imports) != len(tt.expectedImports) {
				t.Errorf("extractImportsAndCode() imports count mismatch: got %d, expected %d", len(imports), len(tt.expectedImports))
			}

			for i, imp := range imports {
				if i >= len(tt.expectedImports) {
					break
				}
				if imp != tt.expectedImports[i] {
					t.Errorf("extractImportsAndCode() import[%d] mismatch: got %q, expected %q", i, imp, tt.expectedImports[i])
				}
			}
		})
	}
}

func TestWrapGoCodeWithAuthorization(t *testing.T) {
	userCode := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`

	wrapped := WrapGoCodeWithAuthorization(userCode)

	// Check that wrapped code has exactly one package declaration
	packageCount := strings.Count(wrapped, "package main")
	if packageCount != 1 {
		t.Errorf("Expected 1 package declaration, got %d", packageCount)
	}

	// Check that wrapped code contains the authorization system
	if !strings.Contains(wrapped, "authorizeDomain") {
		t.Error("Expected wrapped code to contain authorizeDomain function")
	}

	// Check that user code is present
	if !strings.Contains(wrapped, `fmt.Println("Hello, World!")`) {
		t.Error("Expected wrapped code to contain user's main function")
	}

	// Print for manual inspection
	t.Logf("Wrapped code:\n%s", wrapped)
}
