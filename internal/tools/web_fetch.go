package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/htmlconv"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
)

const (
	webFetchDefaultTimeout  = 30 * time.Second
	webFetchMaxBodyBytes    = 1_000_000 // 1MB cap to avoid overwhelming the UI/LLM
	webFetchMaxSummaryBytes = 200_000   // limit summary input size
)

// WebFetchToolSpec defines the schema for the web_fetch tool.
type WebFetchToolSpec struct{}

func (s *WebFetchToolSpec) Name() string {
	return ToolNameWebFetch
}

func (s *WebFetchToolSpec) Description() string {
	return "Fetch HTML content from a URL using an HTTP GET request. Optionally provide summarize_prompt to summarize the fetched content with the summarize model. Leave summarize_prompt empty to skip summarization."
}

func (s *WebFetchToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to fetch (http or https). Domain must be authorized.",
			},
			"summarize_prompt": map[string]interface{}{
				"type":        "string",
				"description": "Optional prompt to summarize the fetched content using the summarize model. Leave empty to skip summarization.",
			},
		},
		"required": []string{"url"},
	}
}

// WebFetchTool performs GET requests with optional summarization.
type WebFetchTool struct {
	client          *http.Client
	summarizeClient llm.Client
	authorizer      Authorizer
}

// NewWebFetchTool constructs a WebFetchTool.
func NewWebFetchTool(client *http.Client, summarizeClient llm.Client, authorizer Authorizer) *WebFetchTool {
	if client == nil {
		client = &http.Client{Timeout: webFetchDefaultTimeout}
	}
	return &WebFetchTool{
		client:          client,
		summarizeClient: summarizeClient,
		authorizer:      authorizer,
	}
}

// NewWebFetchToolFactory creates a factory for WebFetchTool.
func NewWebFetchToolFactory(client *http.Client, summarizeClient llm.Client, authorizer Authorizer) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewWebFetchTool(client, summarizeClient, authorizer)
	}
}

// Legacy compatibility helpers.
func (t *WebFetchTool) Name() string        { return ToolNameWebFetch }
func (t *WebFetchTool) Description() string { return (&WebFetchToolSpec{}).Description() }
func (t *WebFetchTool) Parameters() map[string]interface{} {
	return (&WebFetchToolSpec{}).Parameters()
}

func (t *WebFetchTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	rawURL := strings.TrimSpace(GetStringParam(params, "url", ""))
	if rawURL == "" {
		return &ToolResult{Error: "url is required"}
	}

	reqURL, err := normalizeFetchURL(rawURL)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("invalid url: %v", err)}
	}

	// Authorization check (domain-level)
	if t.authorizer != nil {
		decision, authErr := t.authorizer.Authorize(ctx, ToolNameWebFetch, map[string]interface{}{
			"url": rawURL,
		})
		if authErr != nil {
			return &ToolResult{Error: fmt.Sprintf("authorization error: %v", authErr)}
		}
		if decision != nil && !decision.Allowed {
			if decision.RequiresUserInput {
				return &ToolResult{
					RequiresUserInput: true,
					AuthReason:        decision.Reason,
				}
			}
			return &ToolResult{Error: decision.Reason}
		}
	}

	summaryPrompt := strings.TrimSpace(GetStringParam(params, "summarize_prompt", ""))
	if summaryPrompt != "" && t.summarizeClient == nil {
		return &ToolResult{Error: "summarization model not configured; clear summarize_prompt or configure a summarize model"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to build request: %v", err)}
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()

	bodyReader := io.LimitReader(resp.Body, webFetchMaxBodyBytes+1)
	bodyBytes, err := io.ReadAll(bodyReader)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to read response: %v", err)}
	}

	truncated := len(bodyBytes) > webFetchMaxBodyBytes
	if truncated {
		bodyBytes = bodyBytes[:webFetchMaxBodyBytes]
	}

	body := string(bodyBytes)
	contentType := resp.Header.Get("Content-Type")
	finalURL := reqURL.String()
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	var summary string
	if summaryPrompt != "" {
		summary, err = t.summarizeContent(ctx, summaryPrompt, body, finalURL)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
	}

	result := map[string]interface{}{
		"url":          finalURL,
		"status_code":  resp.StatusCode,
		"content_type": contentType,
		"body":         body,
		"truncated":    truncated,
	}

	if summaryPrompt != "" {
		result["summary_prompt"] = summaryPrompt
		result["summary"] = summary
	}

	uiResult := fmt.Sprintf("GET %s → %d (%d bytes", finalURL, resp.StatusCode, len(bodyBytes))
	if truncated {
		uiResult += ", truncated"
	}
	uiResult += ")"
	if summary != "" {
		uiResult += "\n\nSummary:\n" + summary
	}

	return &ToolResult{
		Result:   result,
		UIResult: uiResult,
	}
}

func (t *WebFetchTool) summarizeContent(ctx context.Context, prompt, body, urlStr string) (string, error) {
	content := body
	if converted, ok := htmlconv.ConvertIfHTML(body); ok {
		content = converted
	}

	content, wasTruncated := truncateStringToBytes(content, webFetchMaxSummaryBytes)
	if wasTruncated {
		content += fmt.Sprintf("\n\n[Content truncated to %d bytes for summarization]", webFetchMaxSummaryBytes)
	}

	fullPrompt := fmt.Sprintf(`Summarize the fetched page content for the user's goal.

URL: %s
User request: %s

Content:
%s`, urlStr, prompt, content)

	logger.Debug("web_fetch: summarizing content for %s (len=%d)", urlStr, len(content))

	summary, err := t.summarizeClient.Complete(ctx, fullPrompt)
	if err != nil {
		return "", fmt.Errorf("summarization failed: %v", err)
	}

	return summary, nil
}

// normalizeFetchURL ensures the URL has a scheme and host and only allows HTTP/S.
func normalizeFetchURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty url")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		parsed, err = url.Parse("https://" + trimmed)
		if err != nil {
			return nil, err
		}
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("missing host")
	}

	return parsed, nil
}

// truncateStringToBytes trims a string to the specified byte limit without breaking characters.
func truncateStringToBytes(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	var (
		builder strings.Builder
		used    int
	)

	for _, r := range s {
		rb := []byte(string(r))
		if used+len(rb) > limit {
			break
		}
		builder.Write(rb)
		used += len(rb)
	}

	return builder.String(), true
}
