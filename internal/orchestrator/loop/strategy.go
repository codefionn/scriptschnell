package loop

import (
	"strings"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// DefaultStrategy implements the standard loop control strategy.
// It handles iteration limits, auto-continue logic, and loop detection.
type DefaultStrategy struct {
	config *Config
}

// NewDefaultStrategy creates a new DefaultStrategy with the specified configuration
func NewDefaultStrategy(config *Config) *DefaultStrategy {
	if config == nil {
		config = DefaultConfig()
	}
	return &DefaultStrategy{config: config}
}

// ShouldContinue determines if the loop should continue after an iteration.
// Returns true if another iteration should be executed.
func (s *DefaultStrategy) ShouldContinue(state State, outcome *IterationOutcome) bool {
	// Check if we've hit the iteration limit
	if state.HasReachedLimit() {
		return false
	}

	// If there was an error, stop
	if outcome != nil && outcome.Result == Error {
		return false
	}

	// If compaction is needed, we should continue after compaction
	if outcome != nil && outcome.Result == CompactionNeeded {
		return true
	}

	// If we broke for any reason other than auto-continue, stop
	if outcome != nil && outcome.Result != Continue && outcome.Result != BreakWithAutoContinue {
		return false
	}

	return true
}

// ShouldAutoContinue determines if auto-continue should be triggered.
// Called when the assistant's response appears incomplete.
func (s *DefaultStrategy) ShouldAutoContinue(state State, content string) bool {
	if !s.config.EnableAutoContinue {
		return false
	}

	// Check if we've reached the auto-continue limit
	if state.HasReachedAutoContinueLimit() {
		return false
	}

	// Check if content ends with incomplete indicators
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}

	// Common incomplete indicators
	incompleteIndicators := []string{
		":",
		":\n",
		"...",
		"(",
		"[",
		"{",
		"\"",
		"'",
		" - ",
		" such as",
		" including",
		" for example",
	}

	for _, indicator := range incompleteIndicators {
		if strings.HasSuffix(trimmed, indicator) {
			return true
		}
	}

	// Check for incomplete code blocks
	if strings.Count(trimmed, "```")%2 != 0 {
		return true
	}

	// Check for incomplete lists (ends with number or bullet)
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 0 {
		lastLine := strings.TrimSpace(lines[len(lines)-1])
		// Check if it looks like the start of a list item
		if matched, _ := matchListPattern(lastLine); matched {
			return true
		}
	}

	return false
}

// matchListPattern checks if a line looks like an incomplete list item
func matchListPattern(line string) (bool, string) {
	// Numbered list pattern (e.g., "1.", "12." )
	for i := 0; i < len(line); i++ {
		if line[i] >= '0' && line[i] <= '9' {
			continue
		}
		if line[i] == '.' && i > 0 {
			// Check if rest is whitespace
			rest := strings.TrimSpace(line[i+1:])
			if rest == "" {
				return true, "numbered list"
			}
		}
		break
	}

	// Bullet patterns
	bullets := []string{"- ", "* ", "+ "}
	for _, bullet := range bullets {
		if strings.HasPrefix(line, bullet) {
			rest := strings.TrimSpace(line[len(bullet):])
			if rest == "" {
				return true, "bullet list"
			}
		}
	}

	return false, ""
}

// GetResult returns the final LoopResult based on the loop's termination state.
func (s *DefaultStrategy) GetResult(state State, lastOutcome *IterationOutcome, terminatedEarly bool) *Result {
	result := &Result{
		IterationsExecuted:   state.Iteration(),
		AutoContinueAttempts: state.AutoContinueAttempts(),
		Metadata:             make(map[string]interface{}),
	}

	// Determine success and termination reason based on outcome
	if lastOutcome == nil {
		result.Success = true
		result.TerminationReason = "completed"
		return result
	}

	switch lastOutcome.Result {
	case Break:
		result.Success = true
		result.TerminationReason = "completed normally"

	case BreakWithAutoContinue:
		result.Success = true
		result.TerminationReason = "auto-continue limit reached"

	case BreakMaxIterations:
		result.Success = true
		result.HitIterationLimit = true
		result.TerminationReason = "maximum iteration limit reached"

	case BreakLoopDetected:
		result.Success = false
		result.LoopDetected = true
		result.TerminationReason = "repetitive loop pattern detected"

	case Error:
		result.Success = false
		result.Error = lastOutcome.Error
		result.TerminationReason = "error occurred"
		if lastOutcome.Error != nil {
			result.TerminationReason += ": " + lastOutcome.Error.Error()
		}

	case CompactionNeeded:
		// This shouldn't happen as a final state, but handle it gracefully
		result.Success = true
		result.TerminationReason = "compaction required"

	case Continue:
		// Loop was interrupted while it should have continued
		if terminatedEarly {
			result.Success = true
			result.TerminationReason = "terminated by external signal"
		} else {
			result.Success = true
			result.TerminationReason = "completed"
		}

	default:
		result.Success = true
		result.TerminationReason = "completed"
	}

	return result
}

// ConservativeStrategy is a more cautious strategy that stops earlier
// to prevent excessive token usage or long-running operations.
type ConservativeStrategy struct {
	DefaultStrategy
}

// NewConservativeStrategy creates a new ConservativeStrategy
func NewConservativeStrategy(config *Config) *ConservativeStrategy {
	if config == nil {
		config = DefaultConfig()
	}
	// Apply conservative defaults
	conservativeConfig := &Config{
		MaxIterations:                     config.MaxIterations / 2, // Half the iterations
		MaxAutoContinueAttempts:           config.MaxAutoContinueAttempts / 2,
		EnableLoopDetection:               true,
		EnableAutoContinue:                config.EnableAutoContinue,
		ContextCompactionThresholdPercent: config.ContextCompactionThresholdPercent - 10, // Compact earlier
		MaxConsecutiveCompactions:         config.MaxConsecutiveCompactions,
	}
	return &ConservativeStrategy{
		DefaultStrategy: *NewDefaultStrategy(conservativeConfig),
	}
}

// ShouldContinue is more conservative about continuing
func (s *ConservativeStrategy) ShouldContinue(state State, outcome *IterationOutcome) bool {
	// Use default logic first
	if !s.DefaultStrategy.ShouldContinue(state, outcome) {
		return false
	}

	// Additional conservative check: stop after any error-like outcome
	if outcome != nil && outcome.Result != Continue && outcome.Result != BreakWithAutoContinue {
		return false
	}

	return true
}

// AggressiveStrategy continues more aggressively, useful for batch operations
type AggressiveStrategy struct {
	DefaultStrategy
}

// NewAggressiveStrategy creates a new AggressiveStrategy
func NewAggressiveStrategy(config *Config) *AggressiveStrategy {
	if config == nil {
		config = DefaultConfig()
	}
	// Apply aggressive defaults
	aggressiveConfig := &Config{
		MaxIterations:                     config.MaxIterations * 2,
		MaxAutoContinueAttempts:           config.MaxAutoContinueAttempts * 2,
		EnableLoopDetection:               true,
		EnableAutoContinue:                true,
		ContextCompactionThresholdPercent: config.ContextCompactionThresholdPercent,
		MaxConsecutiveCompactions:         config.MaxConsecutiveCompactions + 1,
	}
	return &AggressiveStrategy{
		DefaultStrategy: *NewDefaultStrategy(aggressiveConfig),
	}
}

// StrategyFactory creates strategies based on configuration or mode
type StrategyFactory struct {
	defaultConfig *Config
}

// NewStrategyFactory creates a new StrategyFactory
func NewStrategyFactory(config *Config) *StrategyFactory {
	return &StrategyFactory{defaultConfig: config}
}

// Create returns a Strategy based on the specified mode
func (f *StrategyFactory) Create(mode string) Strategy {
	return f.CreateWithConfig(mode, f.defaultConfig)
}

// CreateWithLLMJudge creates a Strategy with LLM judge support.
// For the "llm-judge" mode, llmClient and modelID are required.
// This method is used when you want to create an LLM judge strategy with specific LLM client and model.
func (f *StrategyFactory) CreateWithLLMJudge(mode string, config *Config, llmClient llm.Client, modelID string, session Session) Strategy {
	switch mode {
	case "conservative":
		return NewConservativeStrategy(config)
	case "aggressive":
		return NewAggressiveStrategy(config)
	case "llm-judge":
		// LLM judge strategy requires llmClient and modelID
		if llmClient != nil && modelID != "" {
			return NewLLMJudgeStrategy(config, llmClient, modelID, session)
		}
		// Fall back to default if LLM client not available
		logger.Warn("LLM judge strategy requested but no LLM client/model provided, falling back to default strategy")
		fallthrough
	case "default":
		fallthrough
	default:
		return NewDefaultStrategy(config)
	}
}

// CreateWithConfig returns a Strategy with custom configuration
func (f *StrategyFactory) CreateWithConfig(mode string, config *Config) Strategy {
	switch mode {
	case "conservative":
		return NewConservativeStrategy(config)
	case "aggressive":
		return NewAggressiveStrategy(config)
	case "llm-judge":
		// LLM judge strategy without LLM client - fall back to default
		logger.Warn("LLM judge strategy requested but no LLM client provided, falling back to default strategy")
		fallthrough
	case "default":
		fallthrough
	default:
		return NewDefaultStrategy(config)
	}
}
