package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/codefionn/scriptschnell/internal/features"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// SandboxCodeRewriter detects and rewrites problematic Go code patterns
// before sandbox execution. It handles two cases:
// 1. os/exec usage → rewrite to ExecuteCommand() via LLM
// 2. Print-only code → extract text and skip execution entirely
type SandboxCodeRewriter struct {
	summarizeClient llm.Client
	enabled         bool
}

// RewriteResult describes the outcome of analyzing sandbox code.
type RewriteResult struct {
	RewrittenCode string // The corrected code (for os_exec rewrites)
	ExtractedText string // The static text (for print_only detection)
	RewriteType   string // "os_exec", "print_only", or "none"
}

// NewSandboxCodeRewriter creates a new rewriter instance.
func NewSandboxCodeRewriter(summarizeClient llm.Client) *SandboxCodeRewriter {
	enabled := features.Enabled["sandbox_code_rewrite"]
	return &SandboxCodeRewriter{
		summarizeClient: summarizeClient,
		enabled:         enabled,
	}
}

// os/exec detection patterns
var osExecPatterns = []*regexp.Regexp{
	regexp.MustCompile(`"os/exec"`),
	regexp.MustCompile(`exec\.Command\(`),
	regexp.MustCompile(`exec\.CommandContext\(`),
	regexp.MustCompile(`exec\.LookPath\(`),
	regexp.MustCompile(`os\.StartProcess\(`),
	regexp.MustCompile(`syscall\.Exec\(`),
	regexp.MustCompile(`syscall\.ForkExec\(`),
	regexp.MustCompile(`cmd\.Run\(\)`),
	regexp.MustCompile(`cmd\.Output\(\)`),
	regexp.MustCompile(`cmd\.CombinedOutput\(\)`),
	regexp.MustCompile(`cmd\.Start\(\)`),
}

// DetectOsExecUsage returns true if the code contains os/exec patterns.
func DetectOsExecUsage(code string) bool {
	for _, pat := range osExecPatterns {
		if pat.MatchString(code) {
			return true
		}
	}
	return false
}

// Patterns for print-only detection
var (
	// Match func main() { ... } body
	mainBodyRe = regexp.MustCompile(`(?s)func\s+main\s*\(\s*\)\s*\{(.+)\}`)

	// Match print statements with only string literal arguments
	printStmtRe = regexp.MustCompile(`^\s*(?:fmt\.(?:Println|Printf|Print)|println)\s*\((.+)\)\s*$`)

	// Match a string literal (double-quoted or backtick)
	stringLiteralRe = regexp.MustCompile("^\\s*(?:`[^`]*`|\"(?:[^\"\\\\]|\\\\.)*\")\\s*$")

	// Sandbox API function calls that indicate real computation
	sandboxAPICalls = regexp.MustCompile(`(?:ExecuteCommand|Fetch|ReadFile|WriteFile|CreateFile|ListFiles|GrepFile|RemoveFile|RemoveDir|Mkdir|Move|Summarize|ConvertHTML)\s*\(`)
)

// DetectPrintOnlyCode detects code where main() only contains print
// statements with string literal arguments and no computation.
// Returns (true, extractedText) or (false, "").
func DetectPrintOnlyCode(code string) (bool, string) {
	// Must not contain any sandbox API calls
	if sandboxAPICalls.MatchString(code) {
		return false, ""
	}

	// Extract main body
	match := mainBodyRe.FindStringSubmatch(code)
	if len(match) < 2 {
		return false, ""
	}
	body := match[1]

	// Split body into statements (by newline)
	lines := strings.Split(body, "\n")
	var extractedParts []string
	hasStatements := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "//" {
			continue
		}

		// Skip single-line comments
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		hasStatements = true

		// Must be a print statement
		m := printStmtRe.FindStringSubmatch(line)
		if len(m) < 2 {
			return false, ""
		}

		argStr := strings.TrimSpace(m[1])

		// Handle multiple comma-separated arguments for Printf-like calls
		// But each arg must be a string literal
		args := splitPrintArgs(argStr)
		for _, arg := range args {
			arg = strings.TrimSpace(arg)
			if !stringLiteralRe.MatchString(arg) {
				return false, ""
			}
		}

		// Extract the text content from all string literal args
		for _, arg := range args {
			text := extractStringLiteral(strings.TrimSpace(arg))
			extractedParts = append(extractedParts, text)
		}
	}

	if !hasStatements {
		return false, ""
	}

	return true, strings.Join(extractedParts, "")
}

// splitPrintArgs splits comma-separated print arguments, respecting string
// literals that may contain commas.
func splitPrintArgs(s string) []string {
	var args []string
	depth := 0
	inString := false
	escape := false
	inBacktick := false
	start := 0

	for i, ch := range s {
		if escape {
			escape = false
			continue
		}
		switch {
		case ch == '\\' && inString:
			escape = true
		case ch == '"' && !inBacktick:
			inString = !inString
		case ch == '`' && !inString:
			inBacktick = !inBacktick
		case ch == '(' && !inString && !inBacktick:
			depth++
		case ch == ')' && !inString && !inBacktick:
			depth--
		case ch == ',' && !inString && !inBacktick && depth == 0:
			args = append(args, s[start:i])
			start = i + 1
		}
	}
	args = append(args, s[start:])
	return args
}

// extractStringLiteral removes quotes from a Go string literal and
// unescapes basic sequences.
func extractStringLiteral(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`") {
		return s[1 : len(s)-1]
	}
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		inner := s[1 : len(s)-1]
		inner = strings.ReplaceAll(inner, `\\`, "\x00")
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		inner = strings.ReplaceAll(inner, `\n`, "\n")
		inner = strings.ReplaceAll(inner, `\t`, "\t")
		inner = strings.ReplaceAll(inner, "\x00", `\`)
		return inner
	}
	return s
}

const osExecRewriteSystemPrompt = `You are rewriting Go code for a WebAssembly sandbox. Replace os/exec usage with sandbox APIs.

Available functions (globally available, no imports needed):
- ExecuteCommand(command []string, stdin string) (stdout, stderr string, exitCode int)
- Fetch(method, url, body string) (responseBody string, statusCode int)
- ReadFile(path string, fromLine, toLine int) string
- WriteFile(path string, append bool, content string) string
- CreateFile(path, content string) string
- ListFiles(pattern string) string
- GrepFile(pattern, path, glob string, context int) string
- RemoveFile(path string) string
- RemoveDir(path string, recursive bool) string
- Mkdir(path string, recursive bool) string
- Move(src, dst string) string
- Summarize(prompt, text string) string
- ConvertHTML(html string) string

Global vars: last_exit_code int, last_stdout string, last_stderr string

Rules:
1. exec.Command("ls", "-la") → ExecuteCommand([]string{"ls", "-la"}, "")
2. cmd.Stdin pipe → stdin parameter of ExecuteCommand
3. cmd.Output()/CombinedOutput() → stdout/stderr returns
4. Remove "os/exec" import, keep needed imports
5. Return ONLY the complete rewritten Go code, no markdown fences or explanation`

// RewriteOsExecCode calls the summarization LLM to rewrite os/exec code
// to use sandbox APIs.
func (r *SandboxCodeRewriter) RewriteOsExecCode(ctx context.Context, code string) (string, error) {
	if r.summarizeClient == nil {
		return "", fmt.Errorf("summarization client not available")
	}

	userPrompt := "Rewrite the following Go code to use sandbox APIs instead of os/exec:\n\n" + code

	response, err := r.summarizeClient.CompleteWithRequest(ctx, &llm.CompletionRequest{
		Messages: []*llm.Message{
			{Role: "system", Content: osExecRewriteSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:   8192,
		Temperature: 1.0,
	})
	if err != nil {
		return "", fmt.Errorf("LLM rewrite request failed: %w", err)
	}

	rewritten := strings.TrimSpace(response.Content)

	// Strip markdown code fences if the model wrapped the output
	rewritten = stripCodeFences(rewritten)

	// Sanity check: the rewritten code should still have a main function
	if !strings.Contains(rewritten, "func main()") {
		return "", fmt.Errorf("rewritten code is missing func main()")
	}

	// Should no longer contain os/exec
	if strings.Contains(rewritten, `"os/exec"`) {
		return "", fmt.Errorf("rewritten code still contains os/exec import")
	}

	return rewritten, nil
}

// stripCodeFences removes surrounding ```go ... ``` or ``` ... ``` fences.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```go") {
		s = strings.TrimPrefix(s, "```go")
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

// AnalyzeAndRewrite checks sandbox code for known bad patterns and returns
// a RewriteResult. Returns RewriteType "none" if no rewrite is needed.
func (r *SandboxCodeRewriter) AnalyzeAndRewrite(ctx context.Context, code string) (*RewriteResult, error) {
	if !r.enabled {
		return &RewriteResult{RewriteType: "none"}, nil
	}

	// Check print-only first (cheaper, no LLM call)
	if isPrintOnly, text := DetectPrintOnlyCode(code); isPrintOnly {
		logger.Info("SandboxCodeRewriter: detected print-only code, extracting text")
		return &RewriteResult{
			ExtractedText: text,
			RewriteType:   "print_only",
		}, nil
	}

	// Check os/exec usage (requires LLM call for rewrite)
	if DetectOsExecUsage(code) {
		logger.Info("SandboxCodeRewriter: detected os/exec usage, rewriting")
		rewritten, err := r.RewriteOsExecCode(ctx, code)
		if err != nil {
			return nil, fmt.Errorf("os/exec rewrite failed: %w", err)
		}
		return &RewriteResult{
			RewrittenCode: rewritten,
			RewriteType:   "os_exec",
		}, nil
	}

	return &RewriteResult{RewriteType: "none"}, nil
}
