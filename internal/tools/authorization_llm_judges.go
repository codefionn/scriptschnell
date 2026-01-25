package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/llm"
)

type authorizationRecord struct {
	Kind   string
	Target string
	Reason string
}

// judgeDomainWithLLM delegates domain authorization decisions to the summarize client.
func (a *AuthorizationActor) judgeDomainWithLLM(ctx context.Context, displayDomain string) (*AuthorizationDecision, error) {
	if a.summarizeClient == nil {
		decision := &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Domain %s requires authorization for network access", displayDomain),
			RequiresUserInput: true,
		}
		a.recordLLMDecision(false, authorizationRecord{
			Kind:   "domain",
			Target: displayDomain,
			Reason: decision.Reason,
		})
		return decision, nil
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

	if history := a.formatLLMDecisionHistory(); history != "" {
		prompt = fmt.Sprintf("%s\n\nRecent authorization outcomes to use as reference:\n%s", prompt, history)
	}

	response, err := a.summarizeClient.Complete(ctx, prompt)
	if err != nil {
		decision := &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Domain %s requires authorization (LLM analysis failed)", displayDomain),
			RequiresUserInput: true,
		}
		a.recordLLMDecision(false, authorizationRecord{
			Kind:   "domain",
			Target: displayDomain,
			Reason: decision.Reason,
		})
		return decision, nil
	}

	var result struct {
		Safe   bool   `json:"safe"`
		Reason string `json:"reason"`
		Prefix string `json:"prefix"`
	}

	if err := llm.ParseLLMJSONResponse(response, &result); err != nil {
		decision := &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Domain %s requires authorization (failed to parse LLM response)", displayDomain),
			RequiresUserInput: true,
		}
		a.recordLLMDecision(false, authorizationRecord{
			Kind:   "domain",
			Target: displayDomain,
			Reason: decision.Reason,
		})
		return decision, nil
	}

	if result.Safe {
		decision := &AuthorizationDecision{Allowed: true}
		recordReason := result.Reason
		if recordReason == "" {
			recordReason = fmt.Sprintf("LLM judged %s safe", displayDomain)
		}
		a.recordLLMDecision(true, authorizationRecord{
			Kind:   "domain",
			Target: displayDomain,
			Reason: recordReason,
		})
		return decision, nil
	}

	reason := result.Reason
	if reason == "" {
		reason = fmt.Sprintf("Domain %s requires authorization for network access", displayDomain)
	}

	decision := &AuthorizationDecision{
		Allowed:           false,
		Reason:            reason,
		RequiresUserInput: true,
	}
	a.recordLLMDecision(false, authorizationRecord{
		Kind:   "domain",
		Target: displayDomain,
		Reason: reason,
	})
	return decision, nil
}

// judgeShellCommandWithLLM delegates shell command authorization to the summarize client.
func (a *AuthorizationActor) judgeShellCommandWithLLM(ctx context.Context, command string) (*AuthorizationDecision, error) {
	if a.summarizeClient == nil {
		decision := &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Command requires authorization: %s", command),
			RequiresUserInput: true,
		}
		a.recordLLMDecision(false, authorizationRecord{
			Kind:   "shell",
			Target: command,
			Reason: decision.Reason,
		})
		return decision, nil
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

	if history := a.formatLLMDecisionHistory(); history != "" {
		prompt = fmt.Sprintf("%s\n\nRecent authorization outcomes to use as reference:\n%s", prompt, history)
	}

	response, err := a.summarizeClient.Complete(ctx, prompt)
	if err != nil {
		decision := &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Command requires authorization (LLM analysis failed): %s", command),
			RequiresUserInput: true,
		}
		a.recordLLMDecision(false, authorizationRecord{
			Kind:   "shell",
			Target: command,
			Reason: decision.Reason,
		})
		return decision, nil
	}

	var result struct {
		Harmless bool   `json:"harmless"`
		Reason   string `json:"reason"`
		Prefix   string `json:"prefix"`
	}

	if err := llm.ParseLLMJSONResponse(response, &result); err != nil {
		decision := &AuthorizationDecision{
			Allowed:                false,
			Reason:                 fmt.Sprintf("Command requires authorization (failed to parse LLM response): %s", command),
			RequiresUserInput:      true,
			SuggestedCommandPrefix: "",
		}
		a.recordLLMDecision(false, authorizationRecord{
			Kind:   "shell",
			Target: command,
			Reason: decision.Reason,
		})
		return decision, nil
	}

	suggestedPrefix := strings.TrimSpace(result.Prefix)
	if result.Harmless {
		decision := &AuthorizationDecision{Allowed: true, SuggestedCommandPrefix: suggestedPrefix}
		recordReason := result.Reason
		if recordReason == "" {
			recordReason = fmt.Sprintf("LLM judged %s harmless", command)
		}
		a.recordLLMDecision(true, authorizationRecord{
			Kind:   "shell",
			Target: command,
			Reason: recordReason,
		})
		return decision, nil
	}

	reason := result.Reason
	if reason == "" {
		reason = fmt.Sprintf("Command requires authorization: %s", command)
	}

	if suggestedPrefix != "" {
		reason = fmt.Sprintf("%s\nApproving will also allow future commands starting with %q to run without additional prompts in this project.", reason, suggestedPrefix)
	}

	decision := &AuthorizationDecision{
		Allowed:                false,
		Reason:                 reason,
		RequiresUserInput:      true,
		SuggestedCommandPrefix: suggestedPrefix,
	}
	a.recordLLMDecision(false, authorizationRecord{
		Kind:   "shell",
		Target: command,
		Reason: reason,
	})
	return decision, nil
}

func (a *AuthorizationActor) recordLLMDecision(success bool, record authorizationRecord) {
	if a == nil {
		return
	}
	if success {
		a.lastLLMSuccesses = appendLLMRecord(a.lastLLMSuccesses, record)
		return
	}
	a.lastLLMDeclines = appendLLMRecord(a.lastLLMDeclines, record)
}

func appendLLMRecord(records []authorizationRecord, record authorizationRecord) []authorizationRecord {
	records = append(records, record)
	if len(records) > 10 {
		records = records[len(records)-10:]
	}
	return records
}

func (a *AuthorizationActor) formatLLMDecisionHistory() string {
	if a == nil {
		return ""
	}
	if len(a.lastLLMSuccesses) == 0 && len(a.lastLLMDeclines) == 0 {
		return ""
	}

	var sb strings.Builder
	if len(a.lastLLMSuccesses) > 0 {
		sb.WriteString("Successful authorizations:\n")
		for i := len(a.lastLLMSuccesses) - 1; i >= 0; i-- {
			record := a.lastLLMSuccesses[i]
			sb.WriteString(fmt.Sprintf("- [%s] %s — %s\n", record.Kind, record.Target, record.Reason))
		}
	}

	if len(a.lastLLMDeclines) > 0 {
		sb.WriteString("Declined authorizations:\n")
		for i := len(a.lastLLMDeclines) - 1; i >= 0; i-- {
			record := a.lastLLMDeclines[i]
			sb.WriteString(fmt.Sprintf("- [%s] %s — %s\n", record.Kind, record.Target, record.Reason))
		}
	}

	return strings.TrimSpace(sb.String())
}
