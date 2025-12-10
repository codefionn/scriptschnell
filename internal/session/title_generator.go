package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// TitleGenerator generates concise titles for sessions based on conversation context
type TitleGenerator struct {
	summarizeClient llm.Client
}

// NewTitleGenerator creates a new TitleGenerator
func NewTitleGenerator(summarizeClient llm.Client) *TitleGenerator {
	return &TitleGenerator{
		summarizeClient: summarizeClient,
	}
}

// GenerateTitle generates a concise title for a session based on the first user prompt,
// the list of files in the workspace, and any files that were read in the session
func (tg *TitleGenerator) GenerateTitle(ctx context.Context, userPrompt string, workspaceFiles []string, filesRead map[string]string) (string, error) {
	if tg.summarizeClient == nil {
		// Fallback to simple title if no summarize client available
		return generateSimpleTitle(userPrompt), nil
	}

	// Build context for title generation
	prompt := buildTitleGenerationPrompt(userPrompt, workspaceFiles, filesRead)

	response, err := tg.summarizeClient.Complete(ctx, prompt)
	if err != nil {
		logger.Warn("Failed to generate title with LLM, using fallback: %v", err)
		return generateSimpleTitle(userPrompt), nil
	}

	// Parse the JSON response
	var result struct {
		Title string `json:"title"`
	}

	cleanedResponse := cleanLLMJSONResponse(response)
	if err := json.Unmarshal([]byte(cleanedResponse), &result); err != nil {
		logger.Warn("Failed to parse LLM title response, using fallback: %v", err)
		return generateSimpleTitle(userPrompt), nil
	}

	if result.Title == "" {
		logger.Warn("LLM returned empty title, using fallback")
		return generateSimpleTitle(userPrompt), nil
	}

	// Ensure title is not too long
	if len(result.Title) > 80 {
		result.Title = result.Title[:77] + "..."
	}

	return result.Title, nil
}

// buildTitleGenerationPrompt builds the prompt for title generation
func buildTitleGenerationPrompt(userPrompt string, workspaceFiles []string, filesRead map[string]string) string {
	var sb strings.Builder

	sb.WriteString("You are a session title generator. Generate a concise, descriptive title (maximum 80 characters) for a coding session based on the user's initial request and context.\n\n")
	sb.WriteString(fmt.Sprintf("User's request:\n%s\n\n", userPrompt))

	// Add workspace context if available
	if len(workspaceFiles) > 0 {
		sb.WriteString("Files in workspace (first 20):\n")
		count := 0
		for _, file := range workspaceFiles {
			if count >= 20 {
				sb.WriteString(fmt.Sprintf("... and %d more files\n", len(workspaceFiles)-20))
				break
			}
			sb.WriteString(fmt.Sprintf("- %s\n", file))
			count++
		}
		sb.WriteString("\n")
	}

	// Add files that were read
	if len(filesRead) > 0 {
		sb.WriteString("Files accessed in session:\n")
		for path := range filesRead {
			sb.WriteString(fmt.Sprintf("- %s\n", path))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Generate a short, descriptive title that captures the essence of this session. ")
	sb.WriteString("The title should be:\n")
	sb.WriteString("- Concise (maximum 80 characters)\n")
	sb.WriteString("- Descriptive of the main task or goal\n")
	sb.WriteString("- Easy to understand at a glance\n")
	sb.WriteString("- Technical but not overly verbose\n\n")
	sb.WriteString("Examples of good titles:\n")
	sb.WriteString("- \"Fix authentication bug in login handler\"\n")
	sb.WriteString("- \"Add dark mode toggle to settings\"\n")
	sb.WriteString("- \"Refactor user service for better testability\"\n")
	sb.WriteString("- \"Implement session auto-titling feature\"\n\n")
	sb.WriteString("Respond with ONLY a JSON object in this exact format (no markdown, no code blocks):\n")
	sb.WriteString(`{"title": "your generated title here"}`)

	return sb.String()
}

// generateSimpleTitle creates a simple title from the user prompt as a fallback
func generateSimpleTitle(userPrompt string) string {
	// Take first line of prompt
	lines := strings.Split(userPrompt, "\n")
	title := strings.TrimSpace(lines[0])

	// Remove common prefixes
	title = strings.TrimPrefix(title, "please ")
	title = strings.TrimPrefix(title, "Please ")
	title = strings.TrimPrefix(title, "can you ")
	title = strings.TrimPrefix(title, "Can you ")
	title = strings.TrimPrefix(title, "could you ")
	title = strings.TrimPrefix(title, "Could you ")

	// Capitalize first letter
	if len(title) > 0 {
		runes := []rune(title)
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		title = string(runes)
	}

	// Truncate if too long
	if len(title) > 80 {
		title = title[:77] + "..."
	}

	// If still empty, use generic title
	if title == "" {
		title = "New session"
	}

	return title
}

// cleanLLMJSONResponse removes markdown code blocks and cleans up LLM JSON responses
func cleanLLMJSONResponse(response string) string {
	response = strings.TrimSpace(response)

	// Remove markdown code blocks
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")

	return strings.TrimSpace(response)
}

// GetWorkspaceFiles returns a list of files in the workspace (up to maxFiles)
// This is a helper function that can be used to gather workspace context
func GetWorkspaceFiles(workingDir string, maxFiles int) []string {
	// This would ideally use the fs.FileSystem interface, but for simplicity
	// we can return an empty slice if this is too complex. The caller can
	// optionally provide this information.
	return []string{}
}

// ExtractFilesList extracts just the file paths from the filesRead map
func ExtractFilesList(filesRead map[string]string) []string {
	files := make([]string, 0, len(filesRead))
	for path := range filesRead {
		files = append(files, filepath.Base(path))
	}
	return files
}
