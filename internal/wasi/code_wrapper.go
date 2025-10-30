package wasi

import (
	"net/url"
	"strings"
)

// WrapGoCodeWithAuthorization wraps user Go code with HTTP authorization layer
// This injects the authorization check before the code runs
func WrapGoCodeWithAuthorization(userCode string) string {
	// Extract user imports and code body separately
	userImports, userCodeBody := extractImportsAndCode(userCode)

	// Merge our required imports with user imports
	mergedImports := mergeImports([]string{
		`"bytes"`,
		`"fmt"`,
		`"io"`,
		`"net/http"`,
		`"strings"`,
	}, userImports)

	// Build the complete wrapped code
	var wrapped strings.Builder
	wrapped.WriteString("package main\n\n")
	wrapped.WriteString("import (\n")
	for _, imp := range mergedImports {
		wrapped.WriteString("\t" + imp + "\n")
	}
	wrapped.WriteString(")\n\n")
	wrapped.WriteString(`// STATCODE_AI_INTERNAL: Authorization system
// This is injected by StatCode AI to enforce network authorization

//go:wasmimport env authorize_domain
func authorizeDomainHost(domainPtr *byte, domainLen int32) int32

func authorizeDomain(domain string) bool {
	if len(domain) == 0 {
		return false
	}
	domainBytes := []byte(domain)
	result := authorizeDomainHost(&domainBytes[0], int32(len(domainBytes)))
	return result == 1
}

type authorizedTransport struct {
	base http.RoundTripper
}

func (t *authorizedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	domain := req.URL.Hostname()
	if domain == "" {
		domain = req.URL.Host
		if idx := strings.IndexByte(domain, ':'); idx >= 0 {
			domain = domain[:idx]
		}
	}

	if !authorizeDomain(domain) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(bytes.NewBufferString(fmt.Sprintf("Domain %s not authorized", domain))),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}

	if t.base == nil {
		t.base = http.DefaultTransport
	}
	return t.base.RoundTrip(req)
}

func init() {
	// Replace default HTTP client with authorized version
	http.DefaultClient = &http.Client{
		Transport: &authorizedTransport{},
	}
}

//go:wasmimport env fetch
func fetchHost(methodPtr *byte, methodLen int32, urlPtr *byte, urlLen int32, bodyPtr *byte, bodyLen int32, responsePtr *byte, responseCap int32) int32

// Fetch performs an HTTP request using the host's fetch function
// Returns the response body as a string and the HTTP status code
func Fetch(method, url, body string) (string, int) {
	// Prepare method
	methodBytes := []byte(method)
	var methodPtr *byte
	if len(methodBytes) > 0 {
		methodPtr = &methodBytes[0]
	}

	// Prepare URL
	urlBytes := []byte(url)
	var urlPtr *byte
	if len(urlBytes) > 0 {
		urlPtr = &urlBytes[0]
	}

	// Prepare body
	bodyBytes := []byte(body)
	var bodyPtr *byte
	if len(bodyBytes) > 0 {
		bodyPtr = &bodyBytes[0]
	}

	// Prepare response buffer (max 1MB)
	responseBuffer := make([]byte, 1024*1024)
	var respPtr *byte
	if len(responseBuffer) > 0 {
		respPtr = &responseBuffer[0]
	}

	// Call host fetch function
	statusCode := fetchHost(
		methodPtr, int32(len(methodBytes)),
		urlPtr, int32(len(urlBytes)),
		bodyPtr, int32(len(bodyBytes)),
		respPtr, int32(len(responseBuffer)),
	)

	// Find the actual length of the response
	responseLen := 0
	for i, b := range responseBuffer {
		if b == 0 {
			responseLen = i
			break
		}
	}
	if responseLen == 0 {
		responseLen = len(responseBuffer)
	}

	return string(responseBuffer[:responseLen]), int(statusCode)
}

//go:wasmimport env shell
func shellHost(cmdPtr *byte, cmdLen int32, stdoutPtr *byte, stdoutCap int32, stderrPtr *byte, stderrCap int32) int32

// Shell executes a shell command using the host's shell function
// Returns stdout, stderr, and exit code
func Shell(command string) (stdout string, stderr string, exitCode int) {
	// Prepare command
	cmdBytes := []byte(command)
	var cmdPtr *byte
	if len(cmdBytes) > 0 {
		cmdPtr = &cmdBytes[0]
	}

	// Prepare stdout buffer (max 1MB)
	stdoutBuffer := make([]byte, 1024*1024)
	var stdoutPtr *byte
	if len(stdoutBuffer) > 0 {
		stdoutPtr = &stdoutBuffer[0]
	}

	// Prepare stderr buffer (max 1MB)
	stderrBuffer := make([]byte, 1024*1024)
	var stderrPtr *byte
	if len(stderrBuffer) > 0 {
		stderrPtr = &stderrBuffer[0]
	}

	// Call host shell function
	exitCodeRaw := shellHost(
		cmdPtr, int32(len(cmdBytes)),
		stdoutPtr, int32(len(stdoutBuffer)),
		stderrPtr, int32(len(stderrBuffer)),
	)

	// Find the actual length of stdout
	stdoutLen := 0
	for i, b := range stdoutBuffer {
		if b == 0 {
			stdoutLen = i
			break
		}
	}
	if stdoutLen == 0 && len(stdoutBuffer) > 0 && stdoutBuffer[0] != 0 {
		stdoutLen = len(stdoutBuffer)
	}

	// Find the actual length of stderr
	stderrLen := 0
	for i, b := range stderrBuffer {
		if b == 0 {
			stderrLen = i
			break
		}
	}
	if stderrLen == 0 && len(stderrBuffer) > 0 && stderrBuffer[0] != 0 {
		stderrLen = len(stderrBuffer)
	}

	return string(stdoutBuffer[:stdoutLen]), string(stderrBuffer[:stderrLen]), int(exitCodeRaw)
}

//go:wasmimport env summarize
func summarizeHost(promptPtr *byte, promptLen int32, textPtr *byte, textLen int32, resultPtr *byte, resultCap int32) int32

// Summarize uses the host's summarization LLM to summarize text based on a prompt
// Returns the summary result. Returns error message if summarization fails.
func Summarize(prompt, text string) string {
	// Prepare prompt
	promptBytes := []byte(prompt)
	var promptPtr *byte
	if len(promptBytes) > 0 {
		promptPtr = &promptBytes[0]
	}

	// Prepare text
	textBytes := []byte(text)
	var textPtr *byte
	if len(textBytes) > 0 {
		textPtr = &textBytes[0]
	}

	// Prepare result buffer (max 1MB)
	resultBuffer := make([]byte, 1024*1024)
	var resultPtr *byte
	if len(resultBuffer) > 0 {
		resultPtr = &resultBuffer[0]
	}

	// Call host summarize function
	statusCode := summarizeHost(
		promptPtr, int32(len(promptBytes)),
		textPtr, int32(len(textBytes)),
		resultPtr, int32(len(resultBuffer)),
	)

	// Find the actual length of the result
	resultLen := 0
	for i, b := range resultBuffer {
		if b == 0 {
			resultLen = i
			break
		}
	}
	if resultLen == 0 && len(resultBuffer) > 0 && resultBuffer[0] != 0 {
		resultLen = len(resultBuffer)
	}

	result := string(resultBuffer[:resultLen])

	// Check status code
	if statusCode != 0 {
		// If there's an error, result contains the error message
		if result == "" {
			return fmt.Sprintf("Error: Summarization failed with status code %d", statusCode)
		}
		return result // Return error message from host
	}

	return result
}

// END STATCODE_AI_INTERNAL

// User code begins here:
`)
	wrapped.WriteString(userCodeBody)

	return wrapped.String()
}

// mergeImports combines required imports with user imports, removing duplicates
func mergeImports(required, user []string) []string {
	importSet := make(map[string]bool)
	var result []string

	// Add required imports first
	for _, imp := range required {
		if !importSet[imp] {
			importSet[imp] = true
			result = append(result, imp)
		}
	}

	// Add user imports
	for _, imp := range user {
		if !importSet[imp] {
			importSet[imp] = true
			result = append(result, imp)
		}
	}

	return result
}

// extractImportsAndCode separates imports from the rest of the code
func extractImportsAndCode(code string) ([]string, string) {
	lines := strings.Split(code, "\n")
	var imports []string
	var codeLines []string
	inImportBlock := false
	inMultiLineImport := false
	pastImports := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip package declaration
		if strings.HasPrefix(trimmed, "package ") {
			continue
		}

		// Handle import block
		if strings.HasPrefix(trimmed, "import (") {
			inImportBlock = true
			inMultiLineImport = true
			continue
		}

		if inImportBlock {
			if trimmed == ")" {
				inImportBlock = false
				continue
			}
			if trimmed != "" {
				imports = append(imports, trimmed)
			}
			continue
		}

		// Handle single-line import
		if strings.HasPrefix(trimmed, "import ") {
			// Extract the import path
			importPath := strings.TrimPrefix(trimmed, "import ")
			importPath = strings.TrimSpace(importPath)
			imports = append(imports, importPath)
			continue
		}

		// If we've seen imports and now see non-import code, we're past imports
		if len(imports) > 0 && trimmed != "" && !inMultiLineImport {
			pastImports = true
		}

		// Everything else is user code
		if pastImports || (!inImportBlock && !strings.HasPrefix(trimmed, "package ") && !strings.HasPrefix(trimmed, "import")) {
			codeLines = append(codeLines, line)
		}
	}

	// Join code lines and clean up leading/trailing whitespace
	userCode := strings.Join(codeLines, "\n")
	userCode = strings.TrimSpace(userCode)

	return imports, userCode
}

// ExtractDomainsFromGoCode attempts to extract domains from Go HTTP code
// This is a best-effort static analysis
func ExtractDomainsFromGoCode(code string) []string {
	domains := make(map[string]bool)

	// Look for common HTTP patterns
	patterns := []string{
		`http.Get("`,
		`http.Post("`,
		`http.NewRequest(`,
		`url.Parse("`,
		`"https://`,
		`"http://`,
	}

	for _, pattern := range patterns {
		idx := 0
		for {
			pos := strings.Index(code[idx:], pattern)
			if pos == -1 {
				break
			}
			idx += pos + len(pattern)

			// Extract the URL
			endPos := strings.IndexByte(code[idx:], '"')
			if endPos == -1 {
				continue
			}

			urlStr := code[idx : idx+endPos]
			if parsed, err := url.Parse(urlStr); err == nil && parsed.Host != "" {
				domains[parsed.Hostname()] = true
			}
		}
	}

	result := make([]string, 0, len(domains))
	for domain := range domains {
		result = append(result, domain)
	}
	return result
}

// GenerateWASMStub generates a Go file that can be compiled to WASM with authorization
func GenerateWASMStub(userCode string) (string, []string) {
	wrappedCode := WrapGoCodeWithAuthorization(userCode)
	detectedDomains := ExtractDomainsFromGoCode(userCode)
	return wrappedCode, detectedDomains
}
