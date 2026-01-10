package htmlconv

import (
	"bytes"
	"regexp"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/codefionn/scriptschnell/internal/logger"
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

	// Try to extract main content
	mainContent := findMainContent(doc)
	if mainContent != nil {
		// Remove unwanted nodes from main content only
		removeUnwantedNodes(mainContent)
	} else {
		// Fallback: remove unwanted nodes from entire document
		removeUnwantedNodes(doc)
		mainContent = doc
	}

	// Render back to HTML string
	var buf bytes.Buffer
	if err := html.Render(&buf, mainContent); err != nil {
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

// findMainContent attempts to find the main content node in the HTML document
func findMainContent(doc *html.Node) *html.Node {
	if doc.Type != html.DocumentNode {
		return doc
	}

	// Priority order for finding main content:
	// 1. <main> tag
	// 2. <article> tag
	// 3. elements with semantic classes/ids (content, article, main, post, etc.)
	// 4. <body> as fallback

	var candidates []*html.Node

	// Search for main content indicators
	var search func(n *html.Node)
	search = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tagName := strings.ToLower(n.Data)

			// Check for semantic tags
			switch tagName {
			case "main":
				candidates = append(candidates, n)
			case "article":
				candidates = append(candidates, n)
			case "body":
				candidates = append(candidates, n)
			default:
				// Check for common content identifiers in class/id
				if hasContentIdentifier(n) {
					candidates = append(candidates, n)
				}
			}
		}

		// Recursively search children
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			search(c)
		}
	}

	search(doc)

	// Return the highest priority candidate
	for _, candidate := range candidates {
		if candidate.Type == html.ElementNode {
			tagName := strings.ToLower(candidate.Data)
			if tagName == "main" {
				return candidate
			}
		}
	}

	for _, candidate := range candidates {
		if candidate.Type == html.ElementNode {
			tagName := strings.ToLower(candidate.Data)
			if tagName == "article" {
				return candidate
			}
		}
	}

	// Check for content identifiers
	for _, candidate := range candidates {
		if hasContentIdentifier(candidate) {
			return candidate
		}
	}

	// Fallback to body
	for _, candidate := range candidates {
		if candidate.Type == html.ElementNode && strings.ToLower(candidate.Data) == "body" {
			return candidate
		}
	}

	return doc
}

// hasContentIdentifier checks if a node has class or id attributes that suggest it contains main content
func hasContentIdentifier(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}

	contentIdentifiers := []string{
		"content", "main", "article", "post", "entry", "story",
		"text", "body-content", "page-content", "main-content",
	}

	// Check id attribute
	for _, attr := range n.Attr {
		if strings.ToLower(attr.Key) == "id" {
			for _, id := range contentIdentifiers {
				if strings.Contains(strings.ToLower(attr.Val), id) {
					return true
				}
			}
		}

		// Check class attribute
		if strings.ToLower(attr.Key) == "class" {
			classes := strings.Fields(attr.Val)
			for _, class := range classes {
				for _, id := range contentIdentifiers {
					if strings.Contains(strings.ToLower(class), id) {
						return true
					}
				}
			}
		}
	}

	return false
}

// shouldRemoveNode determines if a node should be removed
func shouldRemoveNode(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}

	// Remove non-content elements and navigation elements
	unwantedTags := map[string]bool{
		"script":   true,
		"style":    true,
		"noscript": true,
		"meta":     true,
		"link":     true,
		"head":     true, // Head contains metadata we don't need
		"header":   true, // Skip headers/navigation
		"footer":   true, // Skip footers
		"nav":      true, // Skip navigation
		"aside":    true, // Skip sidebars
		"iframe":   true, // Skip embedded content
		"svg":      true, // Skip SVG graphics
	}

	return unwantedTags[n.Data]
}
