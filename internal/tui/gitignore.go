package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type gitIgnoreEvaluator interface {
	ignores(absPath string) bool
}

type gitIgnoreChecker struct {
	repoRoot string
	cache    map[string]bool
	mu       sync.Mutex
}

func newGitIgnoreChecker(workingDir string) (gitIgnoreEvaluator, error) {
	root, err := findGitRepositoryRoot(workingDir)
	if err != nil {
		return nil, err
	}
	return &gitIgnoreChecker{
		repoRoot: root,
		cache:    make(map[string]bool),
	}, nil
}

func (c *gitIgnoreChecker) ignores(absPath string) bool {
	relPath, err := filepath.Rel(c.repoRoot, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return false
	}

	c.mu.Lock()
	ignored, ok := c.cache[relPath]
	c.mu.Unlock()
	if ok {
		return ignored
	}

	cmd := exec.Command("git", "-C", c.repoRoot, "check-ignore", "--quiet", "--", relPath)
	err = cmd.Run()
	ignored = err == nil

	c.mu.Lock()
	c.cache[relPath] = ignored
	c.mu.Unlock()
	return ignored
}

func findGitRepositoryRoot(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
