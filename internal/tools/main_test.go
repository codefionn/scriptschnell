package tools

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "tools-cache-*")
	if err == nil {
		_ = os.Setenv("XDG_CACHE_HOME", tmpDir)
	}

	code := m.Run()

	if err == nil {
		_ = os.RemoveAll(tmpDir)
	}

	os.Exit(code)
}
