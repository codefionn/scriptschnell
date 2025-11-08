package fs

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// gitignoreMatcher handles gitignore pattern matching
type gitignoreMatcher struct {
	patterns []*gitignorePattern
}

type gitignorePattern struct {
	pattern   string
	regex     *regexp.Regexp
	isNegated bool
	isDir     bool
}

// parseGitignore parses a .gitignore file and returns a matcher
func parseGitignore(gitignorePath string) (*gitignoreMatcher, error) {
	file, err := os.Open(gitignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &gitignoreMatcher{patterns: []*gitignorePattern{}}, nil
		}
		return nil, err
	}
	defer file.Close()

	return parseGitignoreFromReader(bufio.NewScanner(file))
}

// parseGitignoreFromBytes parses gitignore patterns from a byte slice
func parseGitignoreFromBytes(data []byte) (*gitignoreMatcher, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	return parseGitignoreFromReader(scanner)
}

// parseGitignoreFromReader parses gitignore patterns from a scanner
func parseGitignoreFromReader(scanner *bufio.Scanner) (*gitignoreMatcher, error) {
	matcher := &gitignoreMatcher{patterns: []*gitignorePattern{}}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		pattern := &gitignorePattern{
			pattern: line,
		}

		// Check for negation
		if strings.HasPrefix(line, "!") {
			pattern.isNegated = true
			line = strings.TrimPrefix(line, "!")
		}

		// Check if pattern is directory-only
		if strings.HasSuffix(line, "/") {
			pattern.isDir = true
			line = strings.TrimSuffix(line, "/")
		}

		// Convert gitignore pattern to regex
		regexPattern := gitignorePatternToRegex(line)
		pattern.regex = regexp.MustCompile(regexPattern)
		matcher.patterns = append(matcher.patterns, pattern)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return matcher, nil
}

// gitignorePatternToRegex converts a gitignore pattern to a regex pattern
func gitignorePatternToRegex(pattern string) string {
	// Escape special regex characters except * and ?
	pattern = regexp.QuoteMeta(pattern)

	// Replace escaped wildcards back
	pattern = strings.ReplaceAll(pattern, `\*\*`, ".*")
	pattern = strings.ReplaceAll(pattern, `\*`, "[^/]*")
	pattern = strings.ReplaceAll(pattern, `\?`, "[^/]")

	// If pattern doesn't start with /, it can match at any level
	if !strings.HasPrefix(pattern, "/") {
		pattern = "(^|/)" + pattern
	} else {
		pattern = "^" + strings.TrimPrefix(pattern, "/")
	}

	// Anchor at the end
	pattern = pattern + "($|/)"

	return pattern
}

// matches checks if a path matches any gitignore pattern
func (m *gitignoreMatcher) matches(relPath string, isDir bool) bool {
	if m == nil || len(m.patterns) == 0 {
		return false
	}

	// Normalize path (remove leading ./)
	relPath = strings.TrimPrefix(relPath, "./")

	ignored := false
	for _, p := range m.patterns {
		// Skip directory-only patterns for files
		if p.isDir && !isDir {
			continue
		}

		if p.regex.MatchString(relPath) {
			if p.isNegated {
				ignored = false
			} else {
				ignored = true
			}
		}
	}

	return ignored
}
