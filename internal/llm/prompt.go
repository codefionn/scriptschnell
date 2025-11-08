package llm

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/config"
	"github.com/statcode-ai/statcode-ai/internal/fs"
)

const AgentsFileName = "AGENTS.md"
const AgentsLocalFileName = "AGENTS.local.md"

var defaultToolDescriptions = []string{
	"read_file: Read files with optional line range support.",
	"create_file: Create new files with provided content.",
	"write_file_diff: Update existing files by applying unified diffs.",
	"shell: Execute shell commands with optional background execution.",
	"go_sandbox: Run Go code safely in a sandboxed environment.",
	"todo: Manage todo items for planning and progress tracking.",
	"read_file_summarized: Summarize large files before deep dives.",
	"status: Inspect background job progress.",
}

type systemPromptData struct {
	WorkingDir     string
	Files          []string
	ProjectContext string
	ModelSpecific  string
	Tools          []string
	IsCLIMode      bool
	OS             string
	CurrentDate    string
	HasWebSearch   bool
}

// PromptBuilder builds system prompts for the LLM
type PromptBuilder struct {
	fs         fs.FileSystem
	workingDir string
	config     *config.Config
}

func NewPromptBuilder(filesystem fs.FileSystem, workingDir string, cfg *config.Config) *PromptBuilder {
	return &PromptBuilder{
		fs:         filesystem,
		workingDir: workingDir,
		config:     cfg,
	}
}

// BuildSystemPrompt builds the system prompt including AGENTS.md and model-specific guidance
func (pb *PromptBuilder) BuildSystemPrompt(ctx context.Context, modelName string, cliMode bool) (string, error) {
	files, err := pb.listWorkingDirFiles(ctx)
	if err != nil || len(files) == 0 {
		files = nil
	}

	// Check if web search is configured
	hasWebSearch := pb.config != nil && pb.config.Search.Provider != ""

	// Build tools list
	tools := make([]string, len(defaultToolDescriptions))
	copy(tools, defaultToolDescriptions)
	if hasWebSearch {
		tools = append(tools, "web_search: Search the web for up-to-date information using configured search provider.")
	}

	data := systemPromptData{
		WorkingDir:     pb.workingDir,
		Files:          files,
		ProjectContext: pb.projectSpecificContext(ctx),
		ModelSpecific:  pb.modelSpecificPrompt(modelName),
		Tools:          tools,
		IsCLIMode:      cliMode,
		OS:             runtime.GOOS,
		CurrentDate:    time.Now().Format("2006-01-02"),
		HasWebSearch:   hasWebSearch,
	}

	var buf bytes.Buffer
	if err := systemPrompt.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (pb *PromptBuilder) projectSpecificContext(ctx context.Context) string {
	agentsPath := filepath.Join(pb.workingDir, AgentsLocalFileName)
	exists, err := pb.fs.Exists(ctx, agentsPath)
	if err != nil || !exists {
		agentsPath = filepath.Join(pb.workingDir, AgentsFileName)
		exists, err = pb.fs.Exists(ctx, agentsPath)
		if err != nil || !exists {
			return ""
		}
	}

	data, err := pb.fs.ReadFile(ctx, agentsPath)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func (pb *PromptBuilder) modelSpecificPrompt(modelName string) string {
	baseName := strings.ToLower(strings.TrimSpace(modelName))
	if idx := strings.LastIndex(baseName, "/"); idx >= 0 {
		baseName = baseName[idx+1:]
	}

	modelFamily := DetectModelFamily(modelName)

	switch {
	case modelFamily == FamilyZaiGLM:
		return `Always use the todo tool calls to track progress and plan ahead.
`
	default:
		return ""
	}
}

// listWorkingDirFiles lists files in the working directory (non-recursive, limited)
func (pb *PromptBuilder) listWorkingDirFiles(ctx context.Context) ([]string, error) {
	entries, err := pb.fs.ListDir(ctx, ".")
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		// Skip hidden files and common directories
		name := filepath.Base(entry.Path)
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir {
			if name == "node_modules" || name == "vendor" || name == ".git" {
				continue
			}
			files = append(files, name+"/")
		} else {
			files = append(files, name)
		}

		// Limit to 50 entries
		if len(files) >= 50 {
			files = append(files, "... (more files)")
			break
		}
	}

	return files, nil
}

// InitPrompt returns the prompt for the /init command that asks the LLM to analyze the codebase
func InitPrompt() string {
	return `Your ONLY task is to analyze this codebase and CREATE an AGENTS.md file using the create_file tool.

NOTE: If you want to create a local version that won't be checked into git, you can create AGENTS.local.md instead.
The system will prefer AGENTS.local.md if it exists, otherwise it will use AGENTS.md.

## NON-NEGOTIABLE REQUIREMENTS

**YOU MUST END THIS CONVERSATION BY CALLING create_file WITH path="AGENTS.md"**

DO NOT just describe the file. DO NOT just tell me what should be in it.
YOU MUST ACTUALLY CALL THE create_file TOOL to create the file.
If AGENTS.md already exists, you must read it first and then call write_file_diff with a diff to update it. GPT models may omit @@ hunk markers as long as headers and +/- lines are present.

Steps (DO ALL OF THESE):
1. Read 8-12 key files from the codebase (README.md, main.go, key packages)
2. Understand the project structure
3. **CALL create_file tool with path="AGENTS.md" and the complete content (or use write_file_diff if the file already exists)**

This is NOT complete until you have CALLED create_file (or write_file_diff, if updating).

## What to Include in AGENTS.md

1. Project Overview - what the project does
2. Architecture - key patterns and design decisions
3. Project Structure - directory layout
4. Key Components - main packages/modules
5. Development Workflow - build, test, run commands
6. Coding Conventions - patterns you discover
7. Important Notes - gotchas and key insights

## How to Complete This Task

1. Use read_file to read README.md, CLAUDE.md, go.mod, and main entry point
2. Use read_file to read 5-8 key files from major packages (internal/*, cmd/*)
3. Use shell commands if needed to explore structure
4. **CALL create_file with path="AGENTS.md"** containing all sections above (or **write_file_diff** with an appropriate diff if the file already exists)

## Template for AGENTS.md

When you call create_file (or write_file_diff), use this structure (fill in with ACTUAL information from the files you read):

` + "```markdown" + `
# Agent Context

## Project Overview
[What this project does - be specific based on README/code you read]

## Architecture
[Key patterns - actor model, dual LLM, etc. - based on code]

## Project Structure
` + "```" + `
[paste actual directory tree]
` + "```" + `

## Key Components
### [Component Name]
- Location: [path]
- Purpose: [what it does]

[repeat for each major component]

## Development Workflow
` + "```bash" + `
# Build: [actual command]
# Test: [actual command]
# Run: [actual command]
` + "```" + `

## Coding Conventions
- [pattern 1]
- [pattern 2]
[etc - at least 5]

## Important Notes
- [gotcha 1]
- [gotcha 2]
[etc - at least 3]
` + "```" + `

---

**REMEMBER: After reading files, you MUST call create_file (or write_file_diff for updates). That is the ONLY way to complete this task.**`
}
