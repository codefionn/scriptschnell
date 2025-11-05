package orchestrator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/llm"
	"github.com/statcode-ai/statcode-ai/internal/logger"
)

func (o *Orchestrator) filterToolSpecs(specs []toolSpec) ([]toolSpec, error) {
	if o.summarizeClient == nil {
		return specs, nil
	}

	type mcpSummary struct {
		displayName string
		entries     []string
	}

	optional := make([]toolSpec, 0)
	mcpSummaries := make(map[string]*mcpSummary)
	for _, spec := range specs {
		if !spec.critical {
			optional = append(optional, spec)
			if spec.isMCP {
				serverName := o.lookupServerBySanitizedKey(spec.mcpKey)
				if serverName == "" {
					serverName = spec.mcpKey
				}
				summary := mcpSummaries[spec.mcpKey]
				if summary == nil {
					summary = &mcpSummary{
						displayName: serverName,
					}
					mcpSummaries[spec.mcpKey] = summary
				}
				desc := spec.template.Description()
				if desc == "" {
					desc = fmt.Sprintf("Tool %s", spec.template.Name())
				} else {
					desc = fmt.Sprintf("%s — %s", spec.template.Name(), desc)
				}
				summary.entries = append(summary.entries, desc)
			}
		}
	}

	if len(optional) == 0 {
		return specs, nil
	}

	contextPath, contextBody := o.getContextFileContents()
	dirListing, err := o.listWorkingDirEntries()
	if err != nil {
		dirListing = fmt.Sprintf("Failed to list directory: %v", err)
	}

	selectedMap, selectionErr := o.askSummaryForTools(optional, contextPath, contextBody, dirListing)
	if selectionErr != nil {
		return specs, selectionErr
	}

	includeAll := selectedMap["*"]
	result := make([]toolSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.critical {
			result = append(result, spec)
			continue
		}

		// For MCP tools, check if their server is selected
		if spec.isMCP {
			serverName := o.lookupServerBySanitizedKey(spec.mcpKey)
			if serverName == "" {
				serverName = spec.mcpKey
			}
			serverNameLower := strings.ToLower(serverName)
			mcpKeyLower := strings.ToLower(spec.mcpKey)

			if includeAll || selectedMap[serverNameLower] || selectedMap[mcpKeyLower] {
				result = append(result, spec)
			} else {
				logger.Debug("MCP tool %s disabled (server %s not selected)", spec.template.Name(), serverName)
			}
			continue
		}

		// For built-in tools, check individual tool name
		name := strings.ToLower(spec.template.Name())
		if includeAll || selectedMap[name] {
			result = append(result, spec)
		} else {
			logger.Debug("Tool %s disabled by summarization model", spec.template.Name())
		}
	}

	return result, nil
}

func (o *Orchestrator) askSummaryForTools(optional []toolSpec, contextPath, contextBody, dirListing string) (map[string]bool, error) {
	if o.summarizeClient == nil || len(optional) == 0 {
		return map[string]bool{"*": true}, nil
	}

	type mcpSummary struct {
		displayName string
		entries     []string
	}

	mcpSummaries := make(map[string]*mcpSummary)

	const descriptionLimit = 600

	var builtinBuilder strings.Builder
	var mcpBuilder strings.Builder

	// First pass: collect built-in tools and populate MCP summaries
	for _, spec := range optional {
		if spec.isMCP {
			serverName := o.lookupServerBySanitizedKey(spec.mcpKey)
			if serverName == "" {
				serverName = spec.mcpKey
			}
			summary := mcpSummaries[spec.mcpKey]
			if summary == nil {
				summary = &mcpSummary{displayName: serverName}
				mcpSummaries[spec.mcpKey] = summary
			}
			desc := spec.template.Description()
			if desc == "" {
				desc = spec.template.Name()
			} else {
				desc = fmt.Sprintf("%s — %s", spec.template.Name(), desc)
			}
			summary.entries = append(summary.entries, desc)
			continue
		}
		desc := truncateForPrompt(spec.template.Description(), descriptionLimit)
		builtinBuilder.WriteString(fmt.Sprintf("- %s: %s\n", spec.template.Name(), desc))
	}

	// Second pass: format MCP summaries
	if len(mcpSummaries) > 0 {
		mcpBuilder.WriteString("\nYou should consider the following MCP servers:\n")
		keys := make([]string, 0, len(mcpSummaries))
		for key := range mcpSummaries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			summary := mcpSummaries[key]
			mcpBuilder.WriteString(fmt.Sprintf("- %s provides:\n", summary.displayName))
			sort.Strings(summary.entries)
			for _, entry := range summary.entries {
				mcpBuilder.WriteString(fmt.Sprintf("  • %s\n", truncateForPrompt(entry, descriptionLimit)))
			}
		}
	}

	systemPrompt := `You help choose which optional tools and MCP servers should remain available for an AI coding assistant.
- For built-in tools, select individual tool names (e.g., "web_search").
- For MCP servers, select the server name to enable ALL tools from that server (e.g., "mcp-server-name").
- Respond with a JSON array of tool/server names to enable (e.g., ["web_search", "mcp-rust-docs"]).
- Return ["*"] only if every optional tool and server is clearly required for the user's current context; do not use ["*"] as a default answer.
- Return [] when no optional tools or servers are relevant.
- Output must be valid JSON only—no explanations, no surrounding text.`

	var promptBuilder strings.Builder
	promptBuilder.WriteString("\n<codebase context file>\n")
	promptBuilder.WriteString(contextPath)
	promptBuilder.WriteString("\n</codebase context file>\n")
	promptBuilder.WriteString(contextBody)
	if !strings.HasSuffix(contextBody, "\n") {
		promptBuilder.WriteString("\n")
	}
	promptBuilder.WriteString("\n<current directory entries>\n")
	promptBuilder.WriteString(dirListing)
	if !strings.HasSuffix(dirListing, "\n") {
		promptBuilder.WriteString("\n")
	}
	promptBuilder.WriteString("\n</current directory entries>\n")
	promptBuilder.WriteString("\nOptional built-in tools to consider:\n")
	promptBuilder.WriteString(builtinBuilder.String())
	promptBuilder.WriteString(mcpBuilder.String())
	promptBuilder.WriteString("\nReturn a JSON array with:\n")
	promptBuilder.WriteString("- Individual built-in tool names that should be enabled\n")
	promptBuilder.WriteString("- MCP server names (to enable all tools from that server)")

	req := &llm.CompletionRequest{
		Messages: []*llm.Message{
			{
				Role:    "user",
				Content: promptBuilder.String(),
			},
		},
		Temperature:  0,
		MaxTokens:    16384,
		SystemPrompt: systemPrompt,
	}

	resp, err := o.summarizeClient.CompleteWithRequest(o.ctx, req)
	if err != nil {
		return nil, fmt.Errorf("summarize client request failed: %w", err)
	}

	logger.Debug("Tool selection llm response: %s, %s, %s", req.Messages[0].Content, resp.Content, resp.StopReason)

	names, err := parseToolSelectionResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse summarization response: %w", err)
	}

	result := make(map[string]bool, len(names))
	for _, name := range names {
		normalized := strings.TrimSpace(strings.ToLower(name))
		if normalized != "" {
			result[normalized] = true
		}
	}

	return result, nil
}

func (o *Orchestrator) getContextFileContents() (string, string) {
	contextFile := o.GetExtendedContextFile()
	if contextFile == "" {
		return "(none)", "No context file available."
	}

	data, err := o.fs.ReadFile(o.ctx, contextFile)
	if err != nil {
		return contextFile, fmt.Sprintf("Failed to read %s: %v", contextFile, err)
	}

	return contextFile, string(data)
}

func (o *Orchestrator) listWorkingDirEntries() (string, error) {
	entries, err := o.fs.ListDir(o.ctx, ".")
	if err != nil {
		return "", err
	}

	if len(entries) == 0 {
		return "(empty directory)", nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	const maxEntries = 200
	var builder strings.Builder
	for i, entry := range entries {
		if i >= maxEntries {
			builder.WriteString(fmt.Sprintf("- ... (%d more entries)\n", len(entries)-maxEntries))
			break
		}

		name := entry.Path
		name = strings.TrimPrefix(name, "./")
		if entry.IsDir {
			name += "/"
		}
		builder.WriteString(fmt.Sprintf("- %s\n", name))
	}

	return builder.String(), nil
}

func truncateForPrompt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func parseToolSelectionResponse(text string) ([]string, error) {
	trimmed := strings.TrimSpace(text)
	var names []string
	if err := json.Unmarshal([]byte(trimmed), &names); err == nil {
		return names, nil
	}

	start := strings.Index(trimmed, "[")
	end := strings.LastIndex(trimmed, "]")
	if start >= 0 && end > start {
		snippet := trimmed[start : end+1]
		if err := json.Unmarshal([]byte(snippet), &names); err == nil {
			return names, nil
		}
	}

	return nil, fmt.Errorf("unexpected response format: %s", truncateForPrompt(trimmed, 200))
}
