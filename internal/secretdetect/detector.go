package secretdetect

import (
	"bufio"
	"os"
	"strings"
)

// DetectorImpl implements the Detector interface.
type DetectorImpl struct {
	patterns []SecretPattern
}

// NewDetector creates a new detector with default patterns.
func NewDetector() *DetectorImpl {
	return &DetectorImpl{
		patterns: GetDefaultPatterns(),
	}
}

// NewEmptyDetector creates a new detector with no patterns.
func NewEmptyDetector() *DetectorImpl {
	return &DetectorImpl{
		patterns: []SecretPattern{},
	}
}

// AddPattern adds a new pattern to the detector.
func (d *DetectorImpl) AddPattern(pattern SecretPattern) {
	d.patterns = append(d.patterns, pattern)
}

// Scan scans the provided content for secrets.
func (d *DetectorImpl) Scan(content string) []SecretMatch {
	var matches []SecretMatch

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lineNum := i + 1
		for _, pattern := range d.patterns {
			// Find all matches in the line
			locs := pattern.Regex.FindAllStringIndex(line, -1)
			for _, loc := range locs {
				matchText := line[loc[0]:loc[1]]
				matches = append(matches, SecretMatch{
					PatternName: pattern.Name,
					MatchedText: matchText,
					LineNumber:  lineNum,
					Column:      loc[0] + 1,
					FilePath:    "",  // Not applicable for string scan
					Confidence:  1.0, // Regex match is usually high confidence
				})
			}
		}
	}
	return matches
}

// ScanLines scans a slice of lines for secrets.
func (d *DetectorImpl) ScanLines(lines []string) []SecretMatch {
	var matches []SecretMatch
	for i, line := range lines {
		lineNum := i + 1
		for _, pattern := range d.patterns {
			locs := pattern.Regex.FindAllStringIndex(line, -1)
			for _, loc := range locs {
				matchText := line[loc[0]:loc[1]]
				matches = append(matches, SecretMatch{
					PatternName: pattern.Name,
					MatchedText: matchText,
					LineNumber:  lineNum,
					Column:      loc[0] + 1,
					FilePath:    "",
					Confidence:  1.0,
				})
			}
		}
	}
	return matches
}

// ScanFile scans a file for secrets.
func (d *DetectorImpl) ScanFile(path string) ([]SecretMatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []SecretMatch
	scanner := bufio.NewScanner(file)
	lineNum := 0

	// Buffer for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024) // 1MB max line size

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		for _, pattern := range d.patterns {
			locs := pattern.Regex.FindAllStringIndex(line, -1)
			for _, loc := range locs {
				matchText := line[loc[0]:loc[1]]
				matches = append(matches, SecretMatch{
					PatternName: pattern.Name,
					MatchedText: matchText,
					LineNumber:  lineNum,
					Column:      loc[0] + 1,
					FilePath:    path,
					Confidence:  1.0,
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return matches, err
	}

	return matches, nil
}
