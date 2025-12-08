package syntax

import (
	"regexp"
	"strings"
)

// codeBlockRegex matches markdown fenced code blocks with optional language specifier
// Format: ```language\ncode\n``` or ```\ncode\n```
var codeBlockRegex = regexp.MustCompile("(?s)```(\\w*)\\n(.*?)```")

// HighlightMarkdownCodeBlocks finds all code blocks in markdown and applies syntax highlighting
func (h *Highlighter) HighlightMarkdownCodeBlocks(markdown string) string {
	return codeBlockRegex.ReplaceAllStringFunc(markdown, func(match string) string {
		// Extract language and code
		submatch := codeBlockRegex.FindStringSubmatch(match)
		if len(submatch) < 3 {
			return match
		}

		language := submatch[1]
		code := submatch[2]

		// If no language specified or highlighting not supported, return as-is
		if language == "" {
			return match
		}

		// Apply syntax highlighting
		highlighted, err := h.Highlight(code, language)
		if err != nil {
			// On error, return original
			return match
		}

		// Reconstruct code block with highlighted content
		// Note: This version highlights code blocks directly,
		// which should only be used when NOT passing through glamour
		return "```" + language + "\n" + highlighted + "```"
	})
}

// HighlightAfterGlamour applies syntax highlighting to code blocks in markdown
// that has already been rendered by glamour. This function looks for code blocks
// in glamour's output format and applies highlighting to them.
//
// DEPRECATED: This function is no longer used and doesn't work correctly.
// Glamour consumes markdown code fences during rendering, so this function
// cannot find code blocks in Glamour's output. Instead, syntax highlighting
// is now handled entirely by Glamour's built-in Chroma support. See commit
// history for context on the ANSI control sequence issue this was meant to solve.
func (h *Highlighter) HighlightAfterGlamour(renderedMarkdown string) string {
	// For glamour-rendered output, we need to be more careful about finding code blocks
	// Glamour typically renders code blocks with specific styling that we need to detect

	// First, let's try the standard regex approach in case glamour preserves the structure
	var result strings.Builder
	lastEnd := 0

	matches := codeBlockRegex.FindAllStringSubmatchIndex(renderedMarkdown, -1)
	if len(matches) > 0 {
		// Process found code blocks
		for _, match := range matches {
			// Write text before the code block
			result.WriteString(renderedMarkdown[lastEnd:match[0]])

			// Extract language and code
			languageStart, languageEnd := match[2], match[3]
			codeStart, codeEnd := match[4], match[5]

			language := renderedMarkdown[languageStart:languageEnd]
			code := renderedMarkdown[codeStart:codeEnd]

			// Apply syntax highlighting if language is specified
			if language != "" {
				highlighted, err := h.Highlight(code, language)
				if err == nil {
					result.WriteString(highlighted)
					lastEnd = match[1]
					continue
				}
			}

			// Fallback: write code as-is
			result.WriteString(code)
			lastEnd = match[1]
		}

		// Write remaining text
		result.WriteString(renderedMarkdown[lastEnd:])
		return result.String()
	}

	// If no standard code blocks found, return as-is
	// Glamour might have a different format that we can't easily parse
	return renderedMarkdown
}

// HighlightMarkdownCodeBlocksPlain highlights code blocks and returns them without markdown fencing
// This is useful for displaying code directly in the terminal without markdown rendering
func (h *Highlighter) HighlightMarkdownCodeBlocksPlain(markdown string) string {
	var result strings.Builder
	lastEnd := 0

	matches := codeBlockRegex.FindAllStringSubmatchIndex(markdown, -1)
	for _, match := range matches {
		// Write text before the code block
		result.WriteString(markdown[lastEnd:match[0]])

		// Extract language and code
		languageStart, languageEnd := match[2], match[3]
		codeStart, codeEnd := match[4], match[5]

		language := markdown[languageStart:languageEnd]
		code := markdown[codeStart:codeEnd]

		// Apply syntax highlighting if language is specified
		if language != "" {
			highlighted, err := h.Highlight(code, language)
			if err == nil {
				result.WriteString(highlighted)
				lastEnd = match[1]
				continue
			}
		}

		// Fallback: write code as-is
		result.WriteString(code)
		lastEnd = match[1]
	}

	// Write remaining text
	result.WriteString(markdown[lastEnd:])

	return result.String()
}
