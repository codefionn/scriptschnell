package secretdetect

import (
	"regexp"
)

// Common regex patterns for secret detection
var (
	// AWS Access Key ID
	awsAccessKeyIDRegex = regexp.MustCompile(`(A3T[A-Z0-9]|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}`)

	// AWS Secret Access Key (more prone to false positives, usually needs context)
	// awsSecretAccessKeyRegex = regexp.MustCompile(`(?i)aws(.{0,20})?(?-i)['\"][0-9a-zA-Z\/+]{40}['\"]`)

	// OpenAI API Key (sk-...)
	openAIRegex = regexp.MustCompile(`sk-[a-zA-Z0-9]{32,}`)

	// OpenAI Project Key (sk-proj-...)
	openAIProjRegex = regexp.MustCompile(`sk-proj-[a-zA-Z0-9_\-]{32,}`)

	// Anthropic API Key
	anthropicRegex = regexp.MustCompile(`sk-ant-api03-[a-zA-Z0-9_\-]{20,}`)

	// Google API Key
	googleAPIRegex = regexp.MustCompile(`AIza[0-9A-Za-z\\-_]{35}`)

	// GitHub Personal Access Token
	githubPatRegex = regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`)

	// GitHub OAuth Token
	githubOAuthRegex = regexp.MustCompile(`gho_[a-zA-Z0-9]{36}`)

	// Slack Bot Token
	slackBotTokenRegex = regexp.MustCompile(`xoxb-[0-9]{10,12}-[0-9]{10,12}-[a-zA-Z0-9]{24}`)

	// Slack User Token
	slackUserTokenRegex = regexp.MustCompile(`xoxp-[0-9]{10,12}-[0-9]{10,12}-[0-9]{10,12}-[a-zA-Z0-9]{32}`)

	// RSA Private Key
	rsaPrivateKeyRegex = regexp.MustCompile(`-----BEGIN RSA PRIVATE KEY-----`)

	// SSH Private Key
	sshPrivateKeyRegex = regexp.MustCompile(`-----BEGIN OPENSSH PRIVATE KEY-----`)

	// Generic Private Key
	genericPrivateKeyRegex = regexp.MustCompile(`-----BEGIN PRIVATE KEY-----`)

	// PGP Private Key
	pgpPrivateKeyRegex = regexp.MustCompile(`-----BEGIN PGP PRIVATE KEY BLOCK-----`)
)

// GetDefaultPatterns returns a list of common secret patterns.
func GetDefaultPatterns() []SecretPattern {
	return []SecretPattern{
		{
			Name:        "AWS Access Key ID",
			Regex:       awsAccessKeyIDRegex,
			Description: "AWS Access Key ID",
			Severity:    SeverityHigh,
		},
		{
			Name:        "OpenAI API Key",
			Regex:       openAIRegex,
			Description: "OpenAI API Key starting with sk-",
			Severity:    SeverityCritical,
		},
		{
			Name:        "OpenAI Project Key",
			Regex:       openAIProjRegex,
			Description: "OpenAI Project API Key starting with sk-proj-",
			Severity:    SeverityCritical,
		},
		{
			Name:        "Anthropic API Key",
			Regex:       anthropicRegex,
			Description: "Anthropic API Key",
			Severity:    SeverityCritical,
		},
		{
			Name:        "Google API Key",
			Regex:       googleAPIRegex,
			Description: "Google Cloud API Key",
			Severity:    SeverityHigh,
		},
		{
			Name:        "GitHub PAT",
			Regex:       githubPatRegex,
			Description: "GitHub Personal Access Token",
			Severity:    SeverityCritical,
		},
		{
			Name:        "GitHub OAuth",
			Regex:       githubOAuthRegex,
			Description: "GitHub OAuth Token",
			Severity:    SeverityCritical,
		},
		{
			Name:        "Slack Bot Token",
			Regex:       slackBotTokenRegex,
			Description: "Slack Bot Token",
			Severity:    SeverityHigh,
		},
		{
			Name:        "Slack User Token",
			Regex:       slackUserTokenRegex,
			Description: "Slack User Token",
			Severity:    SeverityHigh,
		},
		{
			Name:        "RSA Private Key",
			Regex:       rsaPrivateKeyRegex,
			Description: "RSA Private Key Header",
			Severity:    SeverityCritical,
		},
		{
			Name:        "SSH Private Key",
			Regex:       sshPrivateKeyRegex,
			Description: "OpenSSH Private Key Header",
			Severity:    SeverityCritical,
		},
		{
			Name:        "Generic Private Key",
			Regex:       genericPrivateKeyRegex,
			Description: "Generic Private Key Header",
			Severity:    SeverityCritical,
		},
		{
			Name:        "PGP Private Key",
			Regex:       pgpPrivateKeyRegex,
			Description: "PGP Private Key Block",
			Severity:    SeverityCritical,
		},
	}
}
