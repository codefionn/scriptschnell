package htmlconv

import (
	"strings"
	"testing"
)

func TestIsHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "DOCTYPE HTML",
			input:    "<!DOCTYPE html><html><body>Test</body></html>",
			expected: true,
		},
		{
			name:     "Simple HTML with tags",
			input:    "<div><p>Hello</p><p>World</p></div>",
			expected: true,
		},
		{
			name:     "Plain text",
			input:    "This is just plain text without any HTML",
			expected: false,
		},
		{
			name:     "Markdown with code",
			input:    "Here's some code: `<div>test</div>`",
			expected: false,
		},
		{
			name:     "Single HTML tag",
			input:    "Check out <a href='test.com'>this link</a>",
			expected: false, // Below threshold
		},
		{
			name:     "Multiple HTML tags",
			input:    "<h1>Title</h1><p>Paragraph 1</p><p>Paragraph 2</p>",
			expected: true,
		},
		{
			name:     "HTML table",
			input:    "<table><tr><td>Cell 1</td><td>Cell 2</td></tr></table>",
			expected: true,
		},
		{
			name:     "Email-style angle brackets",
			input:    "Contact me at <user@example.com>",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHTML(tt.input)
			if result != tt.expected {
				t.Errorf("isHTML() = %v, want %v for input: %s", result, tt.expected, tt.input)
			}
		})
	}
}

func TestConvertIfHTML(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectConverted bool
		checkOutput     func(string) bool
	}{
		{
			name:            "Convert simple HTML",
			input:           "<h1>Title</h1><p>Paragraph text</p>",
			expectConverted: true,
			checkOutput: func(output string) bool {
				return strings.Contains(output, "# Title") && strings.Contains(output, "Paragraph text")
			},
		},
		{
			name:            "Convert HTML with links",
			input:           "<p>Check out <a href='https://example.com'>this link</a></p><p>More text</p><div>Content</div>",
			expectConverted: true,
			checkOutput: func(output string) bool {
				return strings.Contains(output, "[this link](https://example.com)")
			},
		},
		{
			name:            "Plain text unchanged",
			input:           "This is plain text",
			expectConverted: false,
			checkOutput: func(output string) bool {
				return output == "This is plain text"
			},
		},
		{
			name:            "Convert HTML list",
			input:           "<ul><li>Item 1</li><li>Item 2</li><li>Item 3</li></ul>",
			expectConverted: true,
			checkOutput: func(output string) bool {
				return strings.Contains(output, "Item 1") &&
					strings.Contains(output, "Item 2") &&
					strings.Contains(output, "Item 3")
			},
		},
		{
			name: "Convert complex HTML document",
			input: `<!DOCTYPE html>
<html>
<head><title>Test</title></head>
<body>
<h1>Main Title</h1>
<p>This is a <strong>test</strong> paragraph.</p>
<ul>
<li>First item</li>
<li>Second item</li>
</ul>
</body>
</html>`,
			expectConverted: true,
			checkOutput: func(output string) bool {
				return strings.Contains(output, "# Main Title") &&
					strings.Contains(output, "**test**") &&
					strings.Contains(output, "First item")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, converted := ConvertIfHTML(tt.input)

			if converted != tt.expectConverted {
				t.Errorf("ConvertIfHTML() converted = %v, want %v", converted, tt.expectConverted)
			}

			if tt.checkOutput != nil && !tt.checkOutput(output) {
				t.Errorf("ConvertIfHTML() output validation failed. Output:\n%s", output)
			}
		})
	}
}

func TestCleanMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Remove excessive newlines",
			input:    "Line 1\n\n\n\nLine 2",
			expected: "Line 1\n\nLine 2",
		},
		{
			name:     "Trim whitespace",
			input:    "  \n\nContent\n\n  ",
			expected: "Content",
		},
		{
			name:     "Normal markdown unchanged",
			input:    "# Title\n\nParagraph",
			expected: "# Title\n\nParagraph",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("cleanMarkdown() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestConvertHTMLWithHiddenElements(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		shouldNotContain []string
		shouldContain    []string
	}{
		{
			name: "Preserve all content including hidden elements",
			input: `
				<h1>Visible Title</h1>
				<p>Visible paragraph</p>
				<div style="display:none">Hidden content</div>
				<p>More visible content</p>
			`,
			shouldNotContain: []string{},
			shouldContain:    []string{"Visible Title", "Visible paragraph", "Hidden content", "More visible content"},
		},
		{
			name: "Remove script and style tags only",
			input: `
				<h1>Page Title</h1>
				<script>alert('test');</script>
				<style>.hidden { display: none; }</style>
				<p>Page content</p>
			`,
			shouldNotContain: []string{"alert", ".hidden"},
			shouldContain:    []string{"Page Title", "Page content"},
		},
		{
			name: "Preserve navigation and headers",
			input: `
				<header class="site-header">Site Header</header>
				<nav class="navigation">Navigation Menu</nav>
				<div class="content">
					<h1>Main Content</h1>
					<p>Article text</p>
				</div>
				<footer>Site Footer</footer>
			`,
			shouldNotContain: []string{},
			shouldContain:    []string{"Site Header", "Navigation Menu", "Main Content", "Article text", "Site Footer"},
		},
		{
			name: "Preserve aria-hidden elements",
			input: `
				<h1>Title</h1>
				<div aria-hidden="true">Screen reader hidden</div>
				<p>Visible content</p>
			`,
			shouldNotContain: []string{},
			shouldContain:    []string{"Title", "Screen reader hidden", "Visible content"},
		},
		{
			name: "Preserve elements with hidden attribute",
			input: `
				<h1>Title</h1>
				<div hidden>This is hidden</div>
				<p>This is visible</p>
			`,
			shouldNotContain: []string{},
			shouldContain:    []string{"Title", "This is hidden", "This is visible"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, converted := ConvertIfHTML(tt.input)

			if !converted {
				t.Errorf("Expected HTML to be converted, but it wasn't")
				return
			}

			// Check that unwanted content is not present
			for _, unwanted := range tt.shouldNotContain {
				if strings.Contains(output, unwanted) {
					t.Errorf("Output contains unwanted content %q:\n%s", unwanted, output)
				}
			}

			// Check that wanted content is present
			for _, wanted := range tt.shouldContain {
				if !strings.Contains(output, wanted) {
					t.Errorf("Output missing expected content %q:\n%s", wanted, output)
				}
			}
		})
	}
}
