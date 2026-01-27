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
	"github.com/codefionn/scriptschnell/internal/safety"
	"github.com/codefionn/scriptschnell/internal/secretdetect"
	"github.com/codefionn/scriptschnell/internal/summarizer"
)

const (
	webFetchDefaultTimeout  = 30 * time.Second
	webFetchMaxBodyBytes    = 16_384 // ~16KB cap to avoid overwhelming the UI/LLM
	webFetchMaxSummaryBytes = 16_384 // limit summary input size
)

// FeatureFlagsProvider provides access to feature flags
type FeatureFlagsProvider interface {
	IsToolEnabled(toolName string) bool
}

// WebFetchToolSpec defines the schema for the web_fetch tool.
type WebFetchToolSpec struct{}

func (s *WebFetchToolSpec) Name() string {
	return ToolNameWebFetch
}

func (s *WebFetchToolSpec) Description() string {
	return "Fetch HTML content from a URL using an HTTP GET request. Provide a summarize_prompt to extract key information from the fetched content using the fast summarize model. This is highly recommended for large pages or when you need specific information. Leave summarize_prompt empty to get the raw content. Examples: 'Extract the main points', 'What are the key technical specifications?', 'List all API endpoints', or 'What errors are mentioned?'"
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
				"description": "Optional prompt to summarize the fetched content using the summarize model. Leave empty to skip summarization. Recommended prompts: 'Extract the main points and key information', 'What are the key findings or conclusions?', 'List the most important details', 'Summarize this to key points', or provide specific questions like 'What authentication methods are used?' or 'How many errors occurred and what were they?'",
			},
		},
		"required": []string{"url"},
	}
}

// WebFetchTool performs GET requests with optional summarization.
type WebFetchTool struct {
	client            *http.Client
	summarizeClient   llm.Client
	authorizer        Authorizer
	detector          secretdetect.Detector
	featureFlags      FeatureFlagsProvider // Interface to check feature flags
	chunkedSummarizer *summarizer.ChunkedSummarizer
}

// NewWebFetchTool constructs a WebFetchTool.
func NewWebFetchTool(client *http.Client, summarizeClient llm.Client, authorizer Authorizer, detector secretdetect.Detector, featureFlags FeatureFlagsProvider) *WebFetchTool {
	if client == nil {
		client = &http.Client{Timeout: webFetchDefaultTimeout}
	}
	var chunkedSummarizer *summarizer.ChunkedSummarizer
	if summarizeClient != nil {
		chunkedSummarizer = summarizer.NewChunkedSummarizer(summarizeClient)
	}
	return &WebFetchTool{
		client:            client,
		summarizeClient:   summarizeClient,
		authorizer:        authorizer,
		detector:          detector,
		featureFlags:      featureFlags,
		chunkedSummarizer: chunkedSummarizer,
	}
}

// NewWebFetchToolFactory creates a factory for WebFetchTool.
func NewWebFetchToolFactory(client *http.Client, summarizeClient llm.Client, authorizer Authorizer, detector secretdetect.Detector, featureFlags FeatureFlagsProvider) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewWebFetchTool(client, summarizeClient, authorizer, detector, featureFlags)
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

	// Scan request components for secrets before making the request (if feature flag is enabled)
	headers := make(map[string]string) // Currently no custom headers are supported, but preparing for future use
	var secretMatches []secretdetect.SecretMatch
	var secretWarning string

	if t.featureFlags != nil && t.featureFlags.IsToolEnabled("web_fetch_secret_detect") {
		secretMatches = t.scanWebFetchRequest(reqURL.String(), headers, "")
		if len(secretMatches) > 0 {
			secretWarning = formatSecretMatches(secretMatches)
			logger.Debug("web_fetch: detected %d potential secrets in request", len(secretMatches))
		}
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
		summary, err = t.summarizeContent(ctx, summaryPrompt, body, finalURL, secretWarning)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
	}

	// Safety evaluation of web content
	if t.summarizeClient != nil {
		safetyEvaluator := safety.NewEvaluator(t.summarizeClient)
		safetyResult, err := safetyEvaluator.EvaluateWebContent(ctx, body, finalURL)
		if err != nil {
			logger.Warn("Failed to evaluate web content safety: %v", err)
		} else if !safetyResult.IsSafe {
			return &ToolResult{
				Error: fmt.Sprintf("Web content safety evaluation failed: %s (risk level: %s, category: %s)",
					safetyResult.Reason, safetyResult.RiskLevel, safetyResult.Category),
			}
		}
	}

	result := map[string]interface{}{
		"url":          finalURL,
		"status_code":  resp.StatusCode,
		"content_type": contentType,
		"body":         body,
		"truncated":    truncated,
	}

	// Add secret detection metadata if any secrets were found
	if len(secretMatches) > 0 {
		result["secret_detection"] = map[string]interface{}{
			"secrets_found": len(secretMatches),
			"warning":       "Potential secrets detected in request - see UI output for details",
		}
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
	if secretWarning != "" {
		uiResult += secretWarning
	}
	if summary != "" {
		uiResult += "\n\nSummary:\n" + summary
	}

	return &ToolResult{
		Result:   result,
		UIResult: uiResult,
	}
}

func (t *WebFetchTool) summarizeContent(ctx context.Context, prompt, body, urlStr, secretWarning string) (string, error) {
	if t.chunkedSummarizer == nil {
		return "", fmt.Errorf("summarization model not configured")
	}

	content := body
	if converted, ok := htmlconv.ConvertIfHTML(body); ok {
		content = converted
	}

	// Append secret warning if any
	fullContent := content
	if secretWarning != "" {
		fullContent = content + "\n\n" + secretWarning
	}

	// Use the abstracted chunked summarizer
	result, err := t.chunkedSummarizer.Summarize(ctx, fullContent, summarizer.SummarizeOptions{
		Context:    urlStr,
		BasePrompt: prompt,
		MaxBytes:   webFetchMaxSummaryBytes,
	})

	if err != nil {
		return "", fmt.Errorf("summarization failed: %w", err)
	}

	logger.Debug("web_fetch: summarized content for %s (chunks used: %d, total tokens: %d)", urlStr, result.ChunksUsed, result.TotalTokens)

	return result.Summary, nil
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

// scanWebFetchRequest scans the web fetch request components for secrets
func (t *WebFetchTool) scanWebFetchRequest(url string, headers map[string]string, body string) []secretdetect.SecretMatch {
	var allMatches []secretdetect.SecretMatch

	if t.detector == nil {
		return allMatches
	}

	// Scan URL
	urlMatches := t.detector.Scan(url)
	allMatches = append(allMatches, urlMatches...)

	// Scan headers
	for name, value := range headers {
		headerMatches := t.detector.Scan(fmt.Sprintf("%s: %s", name, value))
		allMatches = append(allMatches, headerMatches...)
	}

	// Scan body if provided
	if body != "" {
		bodyMatches := t.detector.Scan(body)
		allMatches = append(allMatches, bodyMatches...)
	}

	return allMatches
}

// formatSecretMatches formats secret matches into a readable warning message
func formatSecretMatches(matches []secretdetect.SecretMatch) string {
	if len(matches) == 0 {
		return ""
	}

	var warnings []string
	for _, match := range matches {
		location := "url"
		if match.FilePath != "" {
			location = match.FilePath
		}
		warnings = append(warnings, fmt.Sprintf("• %s detected in %s at line %d, col %d: %s",
			match.PatternName, location, match.LineNumber, match.Column, match.MatchedText))
	}

	return fmt.Sprintf("\n\n⚠️ **SECRET DETECTION WARNING**\nThe following potential secrets were detected in the web fetch request:\n%s\n\nConsider whether these should be redacted before proceeding.",
		strings.Join(warnings, "\n"))
}
