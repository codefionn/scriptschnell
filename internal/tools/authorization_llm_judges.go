package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// judgeDomainWithLLM delegates domain authorization decisions to the summarize client.
func (a *AuthorizationActor) judgeDomainWithLLM(ctx context.Context, displayDomain string) (*AuthorizationDecision, error) {
	if a.summarizeClient == nil {
		return &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Domain %s requires authorization for network access", displayDomain),
			RequiresUserInput: true,
		}, nil
	}

	prompt := fmt.Sprintf(`You are a security analyzer for network domain access. Analyze the following domain and determine if it is safe or should require user approval.

Domain: %s

Consider the following as safe (no approval needed):
- Well-known public APIs (github.com, api.github.com, etc.)
- Popular public services (googleapis.com, aws.amazon.com, etc.)
- Common package registries (npmjs.org, pypi.org, pkg.go.dev, crates.io)
- Standard CDNs (cloudflare.com, jsdelivr.com, unpkg.com)
- Documentation sites (docs.python.org, developer.mozilla.org)
- Public data APIs (openweathermap.org, api.open-meteo.com)

Consider as potentially unsafe (requiring approval):
- Unknown or uncommon domains
- Domains that could be internal networks (localhost, *.local, etc.)
- Domains used for data exfiltration or tracking
- Suspicious or randomly-generated domain names
- Domains associated with malicious activity

Respond with ONLY a JSON object in this exact format (no markdown, no code blocks):
{"safe": true/false, "reason": "brief explanation", "prefix": "domain pattern for permanent authorization or empty string"}

The "prefix" should be a domain pattern that can be used for permanent authorization in this project.
For example:
- If domain is "api.github.com", prefix could be "*.github.com" (to allow all GitHub subdomains)
- If domain is "example.com", prefix could be "example.com" (exact match only)
- If domain is well-known and widely used, provide a wildcard pattern (e.g., "*.googleapis.com")
- If uncertain or potentially unsafe, set prefix to empty string`, displayDomain)

	response, err := a.summarizeClient.Complete(ctx, prompt)
	if err != nil {
		return &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Domain %s requires authorization (LLM analysis failed)", displayDomain),
			RequiresUserInput: true,
		}, nil
	}

	var result struct {
		Safe   bool   `json:"safe"`
		Reason string `json:"reason"`
		Prefix string `json:"prefix"`
	}

	if err := json.Unmarshal([]byte(cleanLLMJSONResponse(response)), &result); err != nil {
		return &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Domain %s requires authorization (failed to parse LLM response)", displayDomain),
			RequiresUserInput: true,
		}, nil
	}

	if result.Safe {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	reason := result.Reason
	if reason == "" {
		reason = fmt.Sprintf("Domain %s requires authorization for network access", displayDomain)
	}

	return &AuthorizationDecision{
		Allowed:           false,
		Reason:            reason,
		RequiresUserInput: true,
	}, nil
}

// judgeShellCommandWithLLM delegates shell command authorization to the summarize client.
func (a *AuthorizationActor) judgeShellCommandWithLLM(ctx context.Context, command string) (*AuthorizationDecision, error) {
	if a.summarizeClient == nil {
		return &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Command requires authorization: %s", command),
			RequiresUserInput: true,
		}, nil
	}

	prompt := fmt.Sprintf(`You are a security analyzer for shell commands. Analyze the following command and determine if it is potentially harmful or should require user approval.

Command: %s

Consider the following as potentially harmful (requiring approval):
- Commands that modify version control (git commit, git push, git rebase, git reset, git merge, etc.)
- Commands that delete files or directories (rm, rmdir, etc.)
- Commands that modify system state (sudo, apt, yum, systemctl, etc.)
- Commands that send network requests (curl, wget, ssh, scp, etc.)
- Commands that compile or install software (make install, npm install -g, pip install, go install, etc.)
- Commands that modify permissions (chmod, chown, etc.)
- Commands that create permanent changes (e.g. kubectl apply, helm install, terraform apply)

Consider as harmless (no approval needed):
- Read-only commands (ls, cat, find, grep, rg, head, tail, etc.)
- Information commands (pwd, echo, env, printenv, which, whereis, etc.)
- Build/test commands in local directory (go build, go test, npm test, make, cargo build, etc.)
- Git read commands (git status, git log, git diff, git show, etc.)
- Package manager read commands (npm list, pip list, apt search, etc.)

Respond with ONLY a JSON object in this exact format (no markdown, no code blocks):
{"harmless": true/false, "reason": "brief explanation", "prefix": "command prefix for permanent authorization or empty string"}

The "prefix" should be a command prefix that can be used for permanent authorization in this project.
For example, if the command is "git status", the prefix could be "git status".
If the command is "git commit -m 'foo'", the prefix could be "git commit" (to allow all git commits).
If the command has arguments specific to this invocation, provide a more general prefix.
Only provide a prefix if the command is harmless or commonly used in development.
If uncertain or potentially harmful, set prefix to empty string.`, command)

	response, err := a.summarizeClient.Complete(ctx, prompt)
	if err != nil {
		return &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Command requires authorization (LLM analysis failed): %s", command),
			RequiresUserInput: true,
		}, nil
	}

	var result struct {
		Harmless bool   `json:"harmless"`
		Reason   string `json:"reason"`
		Prefix   string `json:"prefix"`
	}

	cleaned := cleanLLMJSONResponse(response)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return &AuthorizationDecision{
			Allowed:                false,
			Reason:                 fmt.Sprintf("Command requires authorization (failed to parse LLM response): %s", command),
			RequiresUserInput:      true,
			SuggestedCommandPrefix: "",
		}, nil
	}

	suggestedPrefix := strings.TrimSpace(result.Prefix)
	if result.Harmless {
		return &AuthorizationDecision{Allowed: true, SuggestedCommandPrefix: suggestedPrefix}, nil
	}

	reason := result.Reason
	if reason == "" {
		reason = fmt.Sprintf("Command requires authorization: %s", command)
	}

	if suggestedPrefix != "" {
		reason = fmt.Sprintf("%s\nApproving will also allow future commands starting with %q to run without additional prompts in this project.", reason, suggestedPrefix)
	}

	return &AuthorizationDecision{
		Allowed:                false,
		Reason:                 reason,
		RequiresUserInput:      true,
		SuggestedCommandPrefix: suggestedPrefix,
	}, nil
}

func cleanLLMJSONResponse(response string) string {
	cleaned := strings.TrimSpace(response)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	return strings.TrimSpace(cleaned)
}
