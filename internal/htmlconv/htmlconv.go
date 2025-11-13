package htmlconv

import (
	"bytes"
	"regexp"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"golang.org/x/net/html"
)

// HTML tag pattern for detection
var htmlTagPattern = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)\b[^>]*>`)

// Threshold for considering text as HTML (number of HTML tags)
const htmlTagThreshold = 3

// ConvertIfHTML detects if the input is HTML and converts it to markdown if needed.
// Returns the converted text and a boolean indicating if conversion was performed.
func ConvertIfHTML(input string) (string, bool) {
	if !isHTML(input) {
		return input, false
	}

	logger.Info("Detected HTML content in prompt, converting to markdown")

	// Preprocess HTML to remove unwanted elements
	cleanedHTML, err := preprocessHTML(input)
	if err != nil {
		logger.Warn("Failed to preprocess HTML: %v, using original", err)
		cleanedHTML = input
	}

	// Convert to markdown
	markdown, err := htmltomarkdown.ConvertString(cleanedHTML)
	if err != nil {
		logger.Warn("Failed to convert HTML to markdown: %v", err)
		return input, false
	}

	// Clean up the markdown output
	markdown = cleanMarkdown(markdown)

	logger.Debug("Converted HTML to markdown (%d -> %d bytes)", len(input), len(markdown))
	return markdown, true
}

// preprocessHTML cleans up HTML by removing unwanted elements
func preprocessHTML(input string) (string, error) {
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return input, err
	}

	// Remove unwanted nodes
	removeUnwantedNodes(doc)

	// Render back to HTML string
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return input, err
	}

	result := buf.String()

	// Debug: log the preprocessed HTML
	logger.Debug("Preprocessed HTML (%d -> %d bytes)", len(input), len(result))

	return result, nil
}

// isHTML detects if the input text is likely HTML
func isHTML(input string) bool {
	// Quick check for common HTML patterns
	trimmed := strings.TrimSpace(input)

	// Check if it starts with common HTML patterns
	if strings.HasPrefix(trimmed, "<!DOCTYPE") ||
		strings.HasPrefix(trimmed, "<!doctype") ||
		strings.HasPrefix(trimmed, "<html") ||
		strings.HasPrefix(trimmed, "<HTML") {
		return true
	}

	// Count HTML tags
	matches := htmlTagPattern.FindAllString(input, -1)
	tagCount := len(matches)

	if tagCount == 0 {
		return false
	}

	// If we have enough tags, check for common HTML elements
	if tagCount >= htmlTagThreshold {
		return true
	}

	// Even with fewer tags, check for common HTML structural elements
	lowerInput := strings.ToLower(input)
	hasHTMLStructure := strings.Contains(lowerInput, "<body") ||
		strings.Contains(lowerInput, "<div") ||
		strings.Contains(lowerInput, "<table") ||
		strings.Contains(lowerInput, "<ul>") ||
		strings.Contains(lowerInput, "<ol>") ||
		strings.Contains(lowerInput, "<h1") ||
		strings.Contains(lowerInput, "<h2")

	// If we have 2+ tags and HTML structure, it's likely HTML
	if tagCount >= 2 && hasHTMLStructure {
		return true
	}

	return false
}

// cleanMarkdown performs post-processing cleanup on converted markdown
func cleanMarkdown(markdown string) string {
	// Remove excessive blank lines (more than 2 consecutive)
	multipleNewlines := regexp.MustCompile(`\n{3,}`)
	markdown = multipleNewlines.ReplaceAllString(markdown, "\n\n")

	// Trim leading/trailing whitespace
	markdown = strings.TrimSpace(markdown)

	return markdown
}

// removeUnwantedNodes recursively removes unwanted elements from the HTML tree
func removeUnwantedNodes(n *html.Node) {
	// Process children first (depth-first)
	child := n.FirstChild
	for child != nil {
		next := child.NextSibling
		removeUnwantedNodes(child)
		child = next
	}

	// Check if this node should be removed
	if shouldRemoveNode(n) {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}
}

// shouldRemoveNode determines if a node should be removed
func shouldRemoveNode(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}

	// Only remove true non-content elements (scripts, styles, metadata)
	// Keep everything else including hidden content
	unwantedTags := map[string]bool{
		"script":   true,
		"style":    true,
		"noscript": true,
		"meta":     true,
		"link":     true,
		"head":     true, // Head contains metadata we don't need
	}

	return unwantedTags[n.Data]
}
