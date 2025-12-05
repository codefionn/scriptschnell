package secretdetect

import (
	"sort"
	"strings"
)

// Redact replaces detected secrets in the content with a default placeholder.
func Redact(content string, matches []SecretMatch) string {
	return RedactWithPlaceholder(content, "[REDACTED]", matches)
}

// RedactWithPlaceholder replaces detected secrets with a custom placeholder.
func RedactWithPlaceholder(content string, placeholder string, matches []SecretMatch) string {
	if len(matches) == 0 {
		return content
	}

	lines := strings.Split(content, "\n")

	// Group matches by line number
	matchesByLine := make(map[int][]SecretMatch)
	for _, m := range matches {
		matchesByLine[m.LineNumber] = append(matchesByLine[m.LineNumber], m)
	}

	for lineNum, lineMatches := range matchesByLine {
		if lineNum < 1 || lineNum > len(lines) {
			continue
		}

		// 0-indexed line index
		idx := lineNum - 1
		line := lines[idx]

		// Sort matches by column descending to avoid index shifting
		sort.Slice(lineMatches, func(i, j int) bool {
			return lineMatches[i].Column > lineMatches[j].Column
		})

		// Apply replacements
		for _, m := range lineMatches {
			colIdx := m.Column - 1 // Convert 1-based to 0-based
			if colIdx < 0 || colIdx >= len(line) {
				continue
			}

			endIdx := colIdx + len(m.MatchedText)
			if endIdx > len(line) {
				endIdx = len(line)
			}

			// Replace
			line = line[:colIdx] + placeholder + line[endIdx:]
		}

		lines[idx] = line
	}

	return strings.Join(lines, "\n")
}
