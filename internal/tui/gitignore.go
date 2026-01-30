package tui

import (
	"context"

	"github.com/codefionn/scriptschnell/internal/vcs"
)

type gitIgnoreEvaluator interface {
	ignores(absPath string) bool
}

// gitIgnoreAdapter adapts the VCS interface to the gitIgnoreEvaluator interface
// This provides backward compatibility with existing code that uses gitIgnoreEvaluator
type gitIgnoreAdapter struct {
	vcs vcs.VCS
}

func (a *gitIgnoreAdapter) ignores(absPath string) bool {
	if a.vcs == nil {
		return false
	}
	ignored, _ := a.vcs.IsIgnored(context.Background(), absPath)
	return ignored
}
