package tui

import orchestratorpkg "github.com/codefionn/scriptschnell/internal/orchestrator"

type (
	AuthorizationCallback   = orchestratorpkg.AuthorizationCallback
	ProgressCallback        = orchestratorpkg.ProgressCallback
	ProgressUpdate          = orchestratorpkg.ProgressUpdate
	ContextUsageCallback    = orchestratorpkg.ContextUsageCallback
	OpenRouterUsageCallback = orchestratorpkg.OpenRouterUsageCallback
)

type Orchestrator = orchestratorpkg.Orchestrator

var NewOrchestrator = orchestratorpkg.NewOrchestrator
