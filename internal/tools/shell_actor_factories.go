package tools

import (
	"github.com/statcode-ai/statcode-ai/internal/actor"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// NewShellToolWithActorFactory creates a factory for shell tools that use ShellActor
func NewShellToolWithActorFactory(shellActor actor.ShellActor, sess *session.Session, workingDir string) ToolFactory {
	return func(registry *Registry) ToolExecutor {
		return NewShellToolWithActor(sess, workingDir, shellActor)
	}
}

// NewStatusProgramToolWithActorFactory creates a factory for status program tools that use ShellActor
func NewStatusProgramToolWithActorFactory(shellActor actor.ShellActor, sess *session.Session) ToolFactory {
	return func(registry *Registry) ToolExecutor {
		return NewStatusProgramToolWithActor(sess, shellActor)
	}
}

// NewWaitProgramToolWithActorFactory creates a factory for wait program tools that use ShellActor
func NewWaitProgramToolWithActorFactory(shellActor actor.ShellActor, sess *session.Session) ToolFactory {
	return func(registry *Registry) ToolExecutor {
		return NewWaitProgramToolWithActor(sess, shellActor)
	}
}

// NewStopProgramToolWithActorFactory creates a factory for stop program tools that use ShellActor
func NewStopProgramToolWithActorFactory(shellActor actor.ShellActor) ToolFactory {
	return func(registry *Registry) ToolExecutor {
		return NewStopProgramToolWithActor(shellActor)
	}
}

// NewSandboxToolWithActorFactory creates a factory for sandbox tools that use ShellActor
func NewSandboxToolWithActorFactory(shellActor actor.ShellActor, sess *session.Session, authorizer Authorizer, configurer func(*SandboxToolWithActor)) ToolFactory {
	return func(registry *Registry) ToolExecutor {
		tool := NewSandboxToolWithActor(sess, shellActor, authorizer)
		if configurer != nil {
			configurer(tool)
		}
		return tool
	}
}
