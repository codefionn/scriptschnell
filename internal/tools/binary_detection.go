package tools

import (
	"path/filepath"
	"strings"
)

// Known binary extensions where reading raw bytes is not useful to the LLM.
var binaryExtensions = map[string]struct{}{
	".exe":   {},
	".dll":   {},
	".so":    {},
	".dylib": {},
	".a":     {},
	".lib":   {},
	".o":     {},
	".obj":   {},
	".wasm":  {},
}

func isBinaryExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := binaryExtensions[ext]
	return ok
}

// hasBinaryContent uses a simple heuristic (presence of NUL bytes in the first 512 bytes)
// to determine if the data is likely binary.
func hasBinaryContent(data []byte) bool {
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

func isLikelyBinaryFile(path string, data []byte) bool {
	return isBinaryExtension(path) || hasBinaryContent(data)
}
