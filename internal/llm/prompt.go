package llm

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/project"
)

const AgentsFileName = "AGENTS.md"
const AgentsLocalFileName = "AGENTS.local.md"

var defaultToolDescriptions = []string{
	"read_file: Read files with optional line range support.",
	"create_file: Create new files with provided content.",
	"edit_file: Update existing files by applying unified diffs.",
	"shell: Execute shell commands with optional background execution.",
	"go_sandbox: Run Go code safely in a sandboxed environment.",
	"todo: Manage todo items for planning and progress tracking.",
	"read_file_summarized: Summarize large files before deep dives.",
	"status: Inspect background job progress.",
}

type systemPromptData struct {
	WorkingDir       string
	Files            []string
	ProjectContext   string
	ModelSpecific    string
	Tools            []string
	IsCLIMode        bool
	OS               string
	CurrentDate      string
	HasWebSearch     bool
	ProjectLanguage  string
	ProjectFramework string
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
func (pb *PromptBuilder) BuildSystemPrompt(ctx context.Context, modelName string, cliMode bool, availableTools []map[string]interface{}) (string, error) {
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

	// Detect project language/framework
	projectLanguage, projectFramework, err := pb.projectLanguageContext(ctx)
	if err != nil {
		projectLanguage = "Unknown"
		projectFramework = ""
	}

	data := systemPromptData{
		WorkingDir:       pb.workingDir,
		Files:            files,
		ProjectContext:   pb.projectSpecificContext(ctx),
		ModelSpecific:    pb.modelSpecificPrompt(modelName, availableTools),
		Tools:            tools,
		IsCLIMode:        cliMode,
		OS:               runtime.GOOS,
		CurrentDate:      time.Now().Format("2006-01-02"),
		HasWebSearch:     hasWebSearch,
		ProjectLanguage:  projectLanguage,
		ProjectFramework: projectFramework,
	}

	var buf bytes.Buffer
	if err := systemPrompt.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (pb *PromptBuilder) projectLanguageContext(ctx context.Context) (string, string, error) {
	detector := project.NewDetector(pb.workingDir)
	projectTypes, err := detector.Detect(ctx)
	if err != nil {
		return "", "", err
	}

	if len(projectTypes) == 0 {
		return "Unknown", "", nil
	}

	// Return the highest confidence project type
	bestMatch := projectTypes[0]
	return bestMatch.Name, bestMatch.Description, nil
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

func (pb *PromptBuilder) modelSpecificPrompt(modelName string, availableTools []map[string]interface{}) string {
	modelFamily := DetectModelFamily(modelName)

	switch {
	case modelFamily == FamilyMistralLarge ||
		modelFamily == FamilyMistralMedium ||
		modelFamily == FamilyMistralSmall ||
		modelFamily == FamilyCodestral ||
		modelFamily == FamilyPixtral ||
		modelFamily == FamilyMixtral:
		return "Shell commands can be executed with the golang sandbox tool call.\n"
	case modelFamily == FamilyZaiGLM:
		return `Always use the todo tool calls to track progress and plan ahead.
Create new todos, sub-todos and check them if done successfully.
`
	case modelFamily == FamilyKimi:
		toolCount := len(availableTools)
		toolNames := make([]string, 0, len(availableTools))
		for _, tool := range availableTools {
			if fn, ok := tool["function"].(map[string]interface{}); ok {
				if name, ok := fn["name"].(string); ok {
					toolNames = append(toolNames, name)
				}
			}
		}

		toolsInfo := ""
		if toolCount > 0 {
			toolsInfo = fmt.Sprintf("Available tools: %s. ", strings.Join(toolNames, ", "))
		}

		return fmt.Sprintf(`%sWhen making tool calls, you MUST include a unique tool_call_id field for each tool call.
This is critical for Kimi K2 models to properly track and manage tool executions.
Each tool call should have a unique ID in the format: call_<unique_identifier>
Example format: {"type": "function", "function": {"name": "tool_name", "arguments": {...}}, "tool_call_id": "call_123"}
`, toolsInfo)
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

## How to Complete This Task

1. Read key dependency files (go.mod, package.json, Cargo.toml, requirements.txt, etc.)
2. Read README.md or CLAUDE.md if they exist
3. Identify the language and version
4. Identify the most important frameworks and dependencies with versions
5. Identify basic tooling commands (lint, formatting, build, test, integration test)
6. **CALL create_file with path="AGENTS.md"** with a short, concise file

## Template for AGENTS.md

Keep this SHORT and FOCUSED. Only include:

` + "```markdown" + `# Project Overview

[Provide a very brief 1-2 sentence description of what this project is and its main components]

Examples:
- "A REST API backend built with Go and Echo framework"
- "A full-stack web application with React frontend and Node.js backend"
- "A command-line tool for processing data files"
- "A monorepo containing a Go backend service and TypeScript frontend"

# Project Context

- **Language**: [e.g., Go, Python, TypeScript]
- **Language Version**: [e.g., 1.21, 3.11, 5.0]
- **Framework**: [main framework if any - e.g., Echo, FastAPI, React]
- **Framework Version**: [version if available]

## Dependencies
[List the most important dependencies and their versions]

## Tooling

- **Build**: [command]
- **Test**: [command]
- **Integration Test**: [command if different from test]
- **Lint**: [command]
- **Format**: [command]
` + "```" + `

---

**REMEMBER: Keep it SHORT. After reading dependency files, you MUST call create_file (or write_file_diff for updates). That is the ONLY way to complete this task.**`
}
