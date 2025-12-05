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
		`"encoding/json"`,
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
// This is injected by scriptschnell to enforce network authorization

//go:wasmimport env get_last_exit_code
func getLastExitCodeHost() int32

//go:wasmimport env get_last_stdout
func getLastStdoutHost(bufferPtr *byte, bufferCap int32) int32

//go:wasmimport env get_last_stderr
func getLastStderrHost(bufferPtr *byte, bufferCap int32) int32

// Global variables accessible in user code
var (
	last_exit_code int
	last_stdout    string
	last_stderr    string
)

func init() {
	// Initialize last_* variables from previous sandbox execution
	last_exit_code = int(getLastExitCodeHost())

	// Get last stdout
	stdoutBuffer := make([]byte, 1024*1024) // 1MB buffer
	var stdoutPtr *byte
	if len(stdoutBuffer) > 0 {
		stdoutPtr = &stdoutBuffer[0]
	}
	stdoutLen := getLastStdoutHost(stdoutPtr, int32(len(stdoutBuffer)))
	if stdoutLen > 0 {
		last_stdout = string(stdoutBuffer[:stdoutLen])
	}

	// Get last stderr
	stderrBuffer := make([]byte, 1024*1024) // 1MB buffer
	var stderrPtr *byte
	if len(stderrBuffer) > 0 {
		stderrPtr = &stderrBuffer[0]
	}
	stderrLen := getLastStderrHost(stderrPtr, int32(len(stderrBuffer)))
	if stderrLen > 0 {
		last_stderr = string(stderrBuffer[:stderrLen])
	}
}

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

//go:wasmimport env command
func commandHost(cmdPtr *byte, cmdLen int32, stdinPtr *byte, stdinLen int32, stdoutPtr *byte, stdoutCap int32, stderrPtr *byte, stderrCap int32) int32

// Command executes a shell command with stdin input using the host's shell function.
// The command must be provided as a slice where the first element is the binary
// and the remaining elements are arguments. Returns stdout, stderr, and exit code.
func ExecuteCommand(command []string, stdin string) (stdout string, stderr string, exitCode int) {
	if len(command) == 0 {
		return "", "Error: command must include at least one argument", -1
	}

	// Serialize the command slice so the host can execute the exact argv form
	cmdBytes, err := json.Marshal(command)
	if err != nil {
		return "", fmt.Sprintf("Error: failed to marshal command: %v", err), -1
	}

	var cmdPtr *byte
	if len(cmdBytes) > 0 {
		cmdPtr = &cmdBytes[0]
	}

	// Prepare stdin
	stdinBytes := []byte(stdin)
	var stdinPtr *byte
	if len(stdinBytes) > 0 {
		stdinPtr = &stdinBytes[0]
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

	// Call host command function
	exitCodeRaw := commandHost(
		cmdPtr, int32(len(cmdBytes)),
		stdinPtr, int32(len(stdinBytes)),
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

//go:wasmimport env read_file
func readFileHost(pathPtr *byte, pathLen int32, fromLine int32, toLine int32, contentPtr *byte, contentCap int32) int32

// ReadFile reads a file from the filesystem
// Parameters:
//   - path: file path relative to working directory
//   - fromLine: starting line number (1-indexed, 0 means read all)
//   - toLine: ending line number (1-indexed, 0 means read all)
// Returns file content as string. Returns error message if operation fails.
func ReadFile(path string, fromLine, toLine int) string {
	pathBytes := []byte(path)
	var pathPtr *byte
	if len(pathBytes) > 0 {
		pathPtr = &pathBytes[0]
	}

	// Prepare content buffer (max 10MB for large files)
	contentBuffer := make([]byte, 10*1024*1024)
	var contentPtr *byte
	if len(contentBuffer) > 0 {
		contentPtr = &contentBuffer[0]
	}

	// Call host read_file function
	statusCode := readFileHost(
		pathPtr, int32(len(pathBytes)),
		int32(fromLine), int32(toLine),
		contentPtr, int32(len(contentBuffer)),
	)

	// Find actual length
	contentLen := 0
	for i, b := range contentBuffer {
		if b == 0 {
			contentLen = i
			break
		}
	}
	if contentLen == 0 && len(contentBuffer) > 0 && contentBuffer[0] != 0 {
		contentLen = len(contentBuffer)
	}

	result := string(contentBuffer[:contentLen])

	// Check status code (0 = success, negative = error)
	if statusCode < 0 {
		if result == "" {
			return fmt.Sprintf("Error: Failed to read file (status %d)", statusCode)
		}
		return result // Error message from host
	}

	return result
}

//go:wasmimport env create_file
func createFileHost(pathPtr *byte, pathLen int32, contentPtr *byte, contentLen int32) int32

// CreateFile creates a new file with the given content
// Returns empty string on success, error message on failure
func CreateFile(path, content string) string {
	pathBytes := []byte(path)
	var pathPtr *byte
	if len(pathBytes) > 0 {
		pathPtr = &pathBytes[0]
	}

	contentBytes := []byte(content)
	var contentPtr *byte
	if len(contentBytes) > 0 {
		contentPtr = &contentBytes[0]
	}

	// Call host create_file function
	statusCode := createFileHost(
		pathPtr, int32(len(pathBytes)),
		contentPtr, int32(len(contentBytes)),
	)

	// Check status code (0 = success, negative = error)
	if statusCode == 0 {
		return "" // Success
	}

	return fmt.Sprintf("Error: Failed to create file (status %d)", statusCode)
}

//go:wasmimport env write_file
func writeFileHost(pathPtr *byte, pathLen int32, appendMode int32, contentPtr *byte, contentLen int32, resultPtr *byte, resultCap int32) int32

// WriteFile writes or appends content to an existing file
// The file must have been read earlier in the session (read-before-write rule)
// If append is true, content is appended to the file; otherwise the file is overwritten
// Returns empty string on success, error message on failure
//
// Examples:
//   WriteFile("file.txt", false, "new content")       // Overwrite file with new content
//   WriteFile("file.txt", true, "additional content") // Append content to file
func WriteFile(path string, append bool, content string) string {
	pathBytes := []byte(path)
	var pathPtr *byte
	if len(pathBytes) > 0 {
		pathPtr = &pathBytes[0]
	}

	appendMode := int32(0)
	if append {
		appendMode = 1
	}

	contentBytes := []byte(content)
	var contentPtr *byte
	if len(contentBytes) > 0 {
		contentPtr = &contentBytes[0]
	}

	// Prepare result buffer for error messages
	resultBuffer := make([]byte, 1024*1024)
	var resultPtr *byte
	if len(resultBuffer) > 0 {
		resultPtr = &resultBuffer[0]
	}

	// Call host write_file function
	statusCode := writeFileHost(
		pathPtr, int32(len(pathBytes)),
		appendMode,
		contentPtr, int32(len(contentBytes)),
		resultPtr, int32(len(resultBuffer)),
	)

	// Find actual length of result
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

	// Check status code (0 = success, negative = error)
	if statusCode == 0 {
		return "" // Success
	}

	if result == "" {
		return fmt.Sprintf("Error: Failed to write file (status %d)", statusCode)
	}
	return result // Error message from host
}

//go:wasmimport env list_files
func listFilesHost(patternPtr *byte, patternLen int32, resultPtr *byte, resultCap int32) int32

// ListFiles lists files matching a glob pattern
// Respects .gitignore rules automatically
// Returns newline-separated list of file paths
func ListFiles(pattern string) string {
	patternBytes := []byte(pattern)
	var patternPtr *byte
	if len(patternBytes) > 0 {
		patternPtr = &patternBytes[0]
	}

	// Prepare result buffer (max 1MB)
	resultBuffer := make([]byte, 1024*1024)
	var resultPtr *byte
	if len(resultBuffer) > 0 {
		resultPtr = &resultBuffer[0]
	}

	// Call host list_files function
	statusCode := listFilesHost(
		patternPtr, int32(len(patternBytes)),
		resultPtr, int32(len(resultBuffer)),
	)

	// Find actual length
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

	// Check status code (0 = success, negative = error)
	if statusCode < 0 {
		if result == "" {
			return fmt.Sprintf("Error: Failed to list files (status %d)", statusCode)
		}
		return result // Error message from host
	}

	return result
}

//go:wasmimport env mkdir
func mkdirHost(pathPtr *byte, pathLen int32, recursive int32, resultPtr *byte, resultCap int32) int32

// Mkdir creates a directory. When recursive is true, parent directories are created as needed.
// Returns empty string on success, or an error message on failure.
func Mkdir(path string, recursive bool) string {
	pathBytes := []byte(path)
	var pathPtr *byte
	if len(pathBytes) > 0 {
		pathPtr = &pathBytes[0]
	}

	recFlag := int32(0)
	if recursive {
		recFlag = 1
	}

	resultBuffer := make([]byte, 1024)
	var resultPtr *byte
	if len(resultBuffer) > 0 {
		resultPtr = &resultBuffer[0]
	}

	statusCode := mkdirHost(
		pathPtr, int32(len(pathBytes)),
		recFlag,
		resultPtr, int32(len(resultBuffer)),
	)

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

	if statusCode == 0 {
		return ""
	}

	if result == "" {
		return fmt.Sprintf("Error: Failed to create directory (status %d)", statusCode)
	}
	return result
}

//go:wasmimport env move
func moveHost(srcPtr *byte, srcLen int32, dstPtr *byte, dstLen int32, resultPtr *byte, resultCap int32) int32

// Move renames or moves a file or directory to a new path.
// Returns empty string on success, or an error message on failure.
func Move(src, dst string) string {
	srcBytes := []byte(src)
	var srcPtr *byte
	if len(srcBytes) > 0 {
		srcPtr = &srcBytes[0]
	}

	dstBytes := []byte(dst)
	var dstPtr *byte
	if len(dstBytes) > 0 {
		dstPtr = &dstBytes[0]
	}

	resultBuffer := make([]byte, 1024)
	var resultPtr *byte
	if len(resultBuffer) > 0 {
		resultPtr = &resultBuffer[0]
	}

	statusCode := moveHost(
		srcPtr, int32(len(srcBytes)),
		dstPtr, int32(len(dstBytes)),
		resultPtr, int32(len(resultBuffer)),
	)

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

	if statusCode == 0 {
		return ""
	}

	if result == "" {
		return fmt.Sprintf("Error: Failed to move (status %d)", statusCode)
	}
	return result
}

//go:wasmimport env remove_file
func removeFileHost(pathPtr *byte, pathLen int32, resultPtr *byte, resultCap int32) int32

// RemoveFile removes a file from the filesystem
// The file must have been read earlier in the session (read-before-write rule)
// Returns empty string on success, error message on failure
func RemoveFile(path string) string {
	pathBytes := []byte(path)
	var pathPtr *byte
	if len(pathBytes) > 0 {
		pathPtr = &pathBytes[0]
	}

	// Prepare result buffer for error messages
	resultBuffer := make([]byte, 1024)
	var resultPtr *byte
	if len(resultBuffer) > 0 {
		resultPtr = &resultBuffer[0]
	}

	// Call host remove_file function
	statusCode := removeFileHost(
		pathPtr, int32(len(pathBytes)),
		resultPtr, int32(len(resultBuffer)),
	)

	// Find actual length of result
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

	// Check status code (0 = success, negative = error)
	if statusCode == 0 {
		return "" // Success
	}

	if result == "" {
		return fmt.Sprintf("Error: Failed to remove file (status %d)", statusCode)
	}
	return result // Error message from host
}

//go:wasmimport env remove_dir
func removeDirHost(pathPtr *byte, pathLen int32, recursive int32, resultPtr *byte, resultCap int32) int32

// RemoveDir removes a directory from the filesystem
// If recursive is true, removes the directory and all its contents
// If recursive is false, only removes empty directories
// Returns empty string on success, error message on failure
func RemoveDir(path string, recursive bool) string {
	pathBytes := []byte(path)
	var pathPtr *byte
	if len(pathBytes) > 0 {
		pathPtr = &pathBytes[0]
	}

	recursiveInt := int32(0)
	if recursive {
		recursiveInt = 1
	}

	// Prepare result buffer for error messages
	resultBuffer := make([]byte, 1024)
	var resultPtr *byte
	if len(resultBuffer) > 0 {
		resultPtr = &resultBuffer[0]
	}

	// Call host remove_dir function
	statusCode := removeDirHost(
		pathPtr, int32(len(pathBytes)),
		recursiveInt,
		resultPtr, int32(len(resultBuffer)),
	)

	// Find actual length of result
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

	// Check status code (0 = success, negative = error)
	if statusCode == 0 {
		return "" // Success
	}

	if result == "" {
		return fmt.Sprintf("Error: Failed to remove directory (status %d)", statusCode)
	}
	return result // Error message from host
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
