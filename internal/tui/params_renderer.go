package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ParamType represents the type of a parameter for styling
type ParamType int

const (
	ParamTypePath ParamType = iota
	ParamTypeCommand
	ParamTypeURL
	ParamTypeQuery
	ParamTypeCode
	ParamTypeNumber
	ParamTypeBoolean
	ParamTypeArray
	ParamTypeObject
	ParamTypeString
)

// Color constants for parameter types
const (
	ColorParamPath    = "#6B8EEF" // Blue
	ColorParamCommand = "#50C878" // Green
	ColorParamURL     = "#00CED1" // Cyan
	ColorParamQuery   = "#FFD700" // Gold
	ColorParamCode    = "#9370DB" // Purple
	ColorParamNumber  = "#FFA500" // Orange
	ColorParamBoolean = "#FF6B9D" // Pink
	ColorParamArray   = "#87CEEB" // Sky Blue
	ColorParamObject  = "#DDA0DD" // Plum
	ColorParamString  = "#C0C0C0" // Silver
	ColorParamKey     = "#808080" // Gray
)

// Icon constants for parameter types (Unicode alternatives for terminal)
const (
	IconParamPath    = "📁"
	IconParamCommand = "⌨️"
	IconParamURL     = "🔗"
	IconParamQuery   = "🔍"
	IconParamCode    = "💻"
	IconParamNumber  = "#️⃣"
	IconParamBoolean = "✓"
	IconParamArray   = "[ ]"
	IconParamObject  = "{ }"
	IconParamString  = "\" \""
)

// ParamsRenderer handles formatting of tool parameters for TUI display
type ParamsRenderer struct {
	ts *ToolStyles

	// Styles for parameter display
	keyStyle       lipgloss.Style
	valueStyle     lipgloss.Style
	typeStyle      lipgloss.Style
	containerStyle lipgloss.Style
}

// NewParamsRenderer creates a new parameter renderer
func NewParamsRenderer() *ParamsRenderer {
	return &ParamsRenderer{
		ts: GetToolStyles(),
		keyStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorParamKey)).
			Bold(true),
		valueStyle: lipgloss.NewStyle(),
		typeStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Italic(true),
		containerStyle: lipgloss.NewStyle().
			PaddingLeft(2),
	}
}

// FormatCompactParams formats parameters as compact key-value pairs with styling
func (pr *ParamsRenderer) FormatCompactParams(params map[string]interface{}, toolName string) string {
	if len(params) == 0 {
		return pr.typeStyle.Render("No parameters")
	}

	var lines []string

	// Get sorted keys for consistent ordering
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := params[key]
		line := pr.formatParamLine(key, value, toolName)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// formatParamLine formats a single parameter key-value pair
func (pr *ParamsRenderer) formatParamLine(key string, value interface{}, toolName string) string {
	paramType := pr.detectParamType(key, value)
	icon := pr.getParamIcon(paramType)
	color := pr.getParamColor(paramType)

	// Format key with icon
	keyStyled := pr.keyStyle.Render(fmt.Sprintf("%s %s:", icon, key))

	// Format value based on type
	valueStr := pr.formatParamValue(value, paramType, 60)
	valueStyled := lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Render(valueStr)

	return fmt.Sprintf("  %s %s", keyStyled, valueStyled)
}

// detectParamType determines the type of a parameter based on key name and value
func (pr *ParamsRenderer) detectParamType(key string, value interface{}) ParamType {
	keyLower := strings.ToLower(key)

	// Check key patterns first
	switch {
	case strings.Contains(keyLower, "path") || strings.Contains(keyLower, "file") || strings.Contains(keyLower, "dir"):
		return ParamTypePath
	case strings.Contains(keyLower, "command") || strings.Contains(keyLower, "cmd") || keyLower == "shell":
		return ParamTypeCommand
	case strings.Contains(keyLower, "url") || strings.HasSuffix(keyLower, "_url"):
		return ParamTypeURL
	case strings.Contains(keyLower, "query") || strings.Contains(keyLower, "search"):
		return ParamTypeQuery
	case strings.Contains(keyLower, "code") || keyLower == "content":
		return ParamTypeCode
	}

	// Check value type
	switch v := value.(type) {
	case bool:
		return ParamTypeBoolean
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return ParamTypeNumber
	case []interface{}:
		return ParamTypeArray
	case map[string]interface{}:
		return ParamTypeObject
	case string:
		// Check string content patterns
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			return ParamTypeURL
		}
		if strings.HasPrefix(v, "/") || strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../") {
			return ParamTypePath
		}
		if strings.Contains(v, "\n") || strings.Contains(v, "func ") || strings.Contains(v, "function ") {
			return ParamTypeCode
		}
	}

	return ParamTypeString
}

// getParamColor returns the color for a parameter type
func (pr *ParamsRenderer) getParamColor(pt ParamType) string {
	switch pt {
	case ParamTypePath:
		return ColorParamPath
	case ParamTypeCommand:
		return ColorParamCommand
	case ParamTypeURL:
		return ColorParamURL
	case ParamTypeQuery:
		return ColorParamQuery
	case ParamTypeCode:
		return ColorParamCode
	case ParamTypeNumber:
		return ColorParamNumber
	case ParamTypeBoolean:
		return ColorParamBoolean
	case ParamTypeArray:
		return ColorParamArray
	case ParamTypeObject:
		return ColorParamObject
	default:
		return ColorParamString
	}
}

// getParamIcon returns the icon for a parameter type
func (pr *ParamsRenderer) getParamIcon(pt ParamType) string {
	switch pt {
	case ParamTypePath:
		return IconParamPath
	case ParamTypeCommand:
		return IconParamCommand
	case ParamTypeURL:
		return IconParamURL
	case ParamTypeQuery:
		return IconParamQuery
	case ParamTypeCode:
		return IconParamCode
	case ParamTypeNumber:
		return IconParamNumber
	case ParamTypeBoolean:
		return IconParamBoolean
	case ParamTypeArray:
		return IconParamArray
	case ParamTypeObject:
		return IconParamObject
	default:
		return IconParamString
	}
}

// formatParamValue formats a parameter value for display
func (pr *ParamsRenderer) formatParamValue(value interface{}, paramType ParamType, maxLen int) string {
	var str string

	switch v := value.(type) {
	case bool:
		if v {
			str = "true"
		} else {
			str = "false"
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		str = fmt.Sprintf("%d", v)
	case float32, float64:
		str = fmt.Sprintf("%.2f", v)
	case string:
		str = v
	case []interface{}:
		str = fmt.Sprintf("[%d items]", len(v))
	case map[string]interface{}:
		str = fmt.Sprintf("{%d keys}", len(v))
	case nil:
		str = "null"
	default:
		// Try JSON serialization
		if bytes, err := json.Marshal(v); err == nil {
			str = string(bytes)
		} else {
			str = fmt.Sprintf("%v", v)
		}
	}

	// Truncate if needed
	if len(str) > maxLen {
		return str[:maxLen-3] + "..."
	}
	return str
}

// FormatCompactParamsOneLine formats parameters as a single compact line
func (pr *ParamsRenderer) FormatCompactParamsOneLine(params map[string]interface{}, toolName string) string {
	if len(params) == 0 {
		return ""
	}

	// For one-line display, show only the most important parameter
	primaryParam := extractPrimaryParameter(toolName, params)
	if primaryParam != "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Render(fmt.Sprintf("(%s)", primaryParam))
	}

	// Fall back to parameter count
	count := len(params)
	if count == 1 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Render("(1 param)")
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Render(fmt.Sprintf("(%d params)", count))
}

// FormatParamsBox formats parameters inside a styled box
func (pr *ParamsRenderer) FormatParamsBox(params map[string]interface{}, toolName string, width int) string {
	content := pr.FormatCompactParams(params, toolName)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#444444")).
		Padding(0, 1).
		Width(width - 4)

	return boxStyle.Render(content)
}

// FormatParamSummary creates a brief summary of parameters
func (pr *ParamsRenderer) FormatParamSummary(params map[string]interface{}) string {
	if len(params) == 0 {
		return "no params"
	}

	// Show up to 3 key names
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}

	if len(keys) <= 3 {
		return strings.Join(keys, ", ")
	}

	return fmt.Sprintf("%s, %s, %s + %d more", keys[0], keys[1], keys[2], len(keys)-3)
}

// FormatCollapsedParams formats parameters with a collapsed/expandable view
// Returns (visible content, hidden count, toggle hint)
func (pr *ParamsRenderer) FormatCollapsedParams(params map[string]interface{}, toolName string, isCollapsed bool, maxVisible int) (string, int, string) {
	if len(params) == 0 {
		return pr.typeStyle.Render("No parameters"), 0, ""
	}

	config := DefaultParamCollapseConfig()
	if maxVisible <= 0 {
		maxVisible = config.MaxVisibleParams
	}

	// Get sorted keys for consistent ordering, prioritizing important params
	keys := pr.getSortedParamKeys(params, config)

	if !isCollapsed {
		// Show all parameters
		var lines []string
		for _, key := range keys {
			value := params[key]
			paramType := pr.detectParamType(key, value)
			color := pr.getParamColor(paramType)

			valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
			formattedValue := pr.formatParamValue(value, paramType, 80)

			line := fmt.Sprintf("  %s: %s",
				pr.keyStyle.Render(key),
				valueStyle.Render(formattedValue))
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n"), 0, "▼ collapse"
	}

	// Show limited parameters when collapsed
	var lines []string
	visibleCount := maxVisible
	if visibleCount > len(keys) {
		visibleCount = len(keys)
	}

	for i := 0; i < visibleCount; i++ {
		key := keys[i]
		value := params[key]
		paramType := pr.detectParamType(key, value)
		color := pr.getParamColor(paramType)

		valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		formattedValue := pr.formatParamValue(value, paramType, config.MaxParamValueLength)

		line := fmt.Sprintf("  %s: %s",
			pr.keyStyle.Render(key),
			valueStyle.Render(formattedValue))
		lines = append(lines, line)
	}

	hiddenCount := len(keys) - visibleCount
	toggleHint := "▶ show more"
	if hiddenCount == 0 {
		toggleHint = "▶ expand"
	}

	return strings.Join(lines, "\n"), hiddenCount, toggleHint
}

// getSortedParamKeys returns parameter keys sorted with important ones first
func (pr *ParamsRenderer) getSortedParamKeys(params map[string]interface{}, config *ParamCollapseConfig) []string {
	important := make([]string, 0)
	others := make([]string, 0)

	for key := range params {
		if config.ImportantParams[key] {
			important = append(important, key)
		} else {
			others = append(others, key)
		}
	}

	// Sort both lists alphabetically
	sort.Strings(important)
	sort.Strings(others)

	// Combine with important first
	return append(important, others...)
}
