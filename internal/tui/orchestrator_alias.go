package tui

import orchestratorpkg "github.com/statcode-ai/statcode-ai/internal/orchestrator"

type (
	AuthorizationCallback = orchestratorpkg.AuthorizationCallback
	StatusCallback        = orchestratorpkg.StatusCallback
	ContextUsageCallback  = orchestratorpkg.ContextUsageCallback
)

type Orchestrator = orchestratorpkg.Orchestrator

var NewOrchestrator = orchestratorpkg.NewOrchestrator
