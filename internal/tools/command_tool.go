package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CommandTool executes a local command and returns its output.
type CommandTool struct {
	name        string
	description string
	command     []string
	workingDir  string
	env         map[string]string
	timeout     time.Duration
}

// CommandToolConfig captures options required to build a CommandTool.
type CommandToolConfig struct {
	Name        string
	Description string
	Command     []string
	WorkingDir  string
	Env         map[string]string
	Timeout     time.Duration
}

// NewCommandTool constructs a CommandTool from the provided configuration.
func NewCommandTool(cfg *CommandToolConfig) *CommandTool {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	envCopy := make(map[string]string, len(cfg.Env))
	for k, v := range cfg.Env {
		envCopy[k] = v
	}

	cmdCopy := append([]string(nil), cfg.Command...)

	return &CommandTool{
		name:        cfg.Name,
		description: cfg.Description,
		command:     cmdCopy,
		workingDir:  cfg.WorkingDir,
		env:         envCopy,
		timeout:     timeout,
	}
}

// Name implements Tool.
func (c *CommandTool) Name() string {
	return c.name
}

// Description implements Tool.
func (c *CommandTool) Description() string {
	if c.description != "" {
		return c.description
	}
	return fmt.Sprintf("Execute command: %s", strings.Join(c.command, " "))
}

// Parameters implements Tool.
func (c *CommandTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input": map[string]interface{}{
				"type":        "string",
				"description": "Optional string written to the command's STDIN.",
			},
			"args": map[string]interface{}{
				"type":        "array",
				"description": "Additional arguments appended when executing the command.",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
	}
}

// Execute runs the configured command with optional STDIN and extra arguments.
func (c *CommandTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if len(c.command) == 0 {
		return nil, fmt.Errorf("command not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	baseArgs := c.command[1:]
	extraArgs := extractStringSlice(params["args"])
	allArgs := append(baseArgs, extraArgs...)

	cmd := exec.CommandContext(ctx, c.command[0], allArgs...)
	if c.workingDir != "" {
		cmd.Dir = c.workingDir
	}

	cmd.Env = os.Environ()
	if len(c.env) > 0 {
		for key, val := range c.env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, val))
		}
	}

	input := ""
	if s, ok := params["input"].(string); ok {
		input = s
	}
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	} else {
		exitCode = 0
	}

	result := map[string]interface{}{
		"command":    strings.Join(c.command, " "),
		"args":       allArgs,
		"stdout":     stdout.String(),
		"stderr":     stderr.String(),
		"exit_code":  exitCode,
		"success":    err == nil,
		"workingDir": cmd.Dir,
		"env":        c.env,
	}

	if err != nil {
		result["error"] = err.Error()
	}

	return result, nil
}

func extractStringSlice(value interface{}) []string {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return []string{fmt.Sprint(v)}
	}
}
