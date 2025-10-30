package tools

import (
	"testing"
)

func TestExtractDomainsFromCode(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected []string
	}{
		{
			name: "HTTP URL",
			code: `
package main
import "net/http"
func main() {
	http.Get("http://api.example.com/data")
}`,
			expected: []string{"api.example.com"},
		},
		{
			name: "HTTPS URL",
			code: `
package main
import "net/http"
func main() {
	http.Get("https://jsonplaceholder.typicode.com/posts")
}`,
			expected: []string{"jsonplaceholder.typicode.com"},
		},
		{
			name: "Multiple URLs",
			code: `
package main
import "net/http"
func main() {
	http.Get("https://api1.example.com/data")
	http.Get("https://api2.example.com/data")
}`,
			expected: []string{"api1.example.com", "api2.example.com"},
		},
		{
			name: "Domain without protocol",
			code: `
package main
func main() {
	domain := "api.example.com"
}`,
			expected: []string{"api.example.com"},
		},
		{
			name: "No domains",
			code: `
package main
func main() {
	println("Hello, world!")
}`,
			expected: []string{},
		},
		{
			name: "URL with port",
			code: `
package main
import "net/http"
func main() {
	http.Get("https://localhost:8080/data")
}`,
			expected: []string{"localhost"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domains := extractDomainsFromCode(tt.code)

			// Check length
			if len(domains) != len(tt.expected) {
				t.Errorf("Expected %d domains, got %d: %v", len(tt.expected), len(domains), domains)
				return
			}

			// Convert to map for easier checking
			domainMap := make(map[string]bool)
			for _, d := range domains {
				domainMap[d] = true
			}

			// Check each expected domain exists
			for _, expected := range tt.expected {
				if !domainMap[expected] {
					t.Errorf("Expected domain %s not found in %v", expected, domains)
				}
			}
		})
	}
}

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected string
	}{
		{
			name:     "Simple domain",
			domain:   "example.com",
			expected: "example.com",
		},
		{
			name:     "Domain with port",
			domain:   "example.com:8080",
			expected: "example.com",
		},
		{
			name:     "HTTP URL",
			domain:   "http://example.com",
			expected: "example.com",
		},
		{
			name:     "HTTPS URL",
			domain:   "https://example.com",
			expected: "example.com",
		},
		{
			name:     "HTTPS URL with port",
			domain:   "https://example.com:8443",
			expected: "example.com",
		},
		{
			name:     "Subdomain",
			domain:   "api.example.com",
			expected: "api.example.com",
		},
		{
			name:     "Uppercase domain",
			domain:   "API.EXAMPLE.COM",
			expected: "api.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeDomain(tt.domain)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestContainsDangerousOpsWithNetworkAccess(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		dangerous bool
	}{
		{
			name: "Safe HTTP code - network allowed",
			code: `
package main
import "net/http"
func main() {
	http.Get("https://api.example.com")
}`,
			dangerous: false,
		},
		{
			name: "Safe network listener code",
			code: `
package main
import "net"
func main() {
	ln, _ := net.Listen("tcp", ":8080")
	defer ln.Close()
}`,
			dangerous: true, // net.Listen should still be blocked
		},
		{
			name: "Safe HTTP server code",
			code: `
package main
import "net/http"
func main() {
	http.ListenAndServe(":8080", nil)
}`,
			dangerous: true, // ListenAndServe should still be blocked
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsDangerousOps(tt.code)
			if result != tt.dangerous {
				t.Errorf("Expected dangerous=%v, got %v", tt.dangerous, result)
			}
		})
	}
}
