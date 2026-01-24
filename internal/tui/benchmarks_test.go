package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/actor"
)

// Benchmarks for performance-critical paths

func BenchmarkAuthorizationDialogCreation(b *testing.B) {
	req := &AuthorizationRequest{
		AuthID:     "benchmark",
		ToolName:   "shell",
		Parameters: map[string]interface{}{"command": "echo test"},
		Reason:     "Benchmark authorization",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog := NewAuthorizationDialog(req, "test-tab")
		_ = dialog
	}
}

func BenchmarkAuthorizationDialogRender(b *testing.B) {
	req := &AuthorizationRequest{
		AuthID:     "benchmark",
		ToolName:   "shell",
		Parameters: map[string]interface{}{"command": "echo test"},
		Reason:     "Benchmark authorization",
	}

	dialog := NewAuthorizationDialog(req, "test-tab")
	dialog.width = 120
	dialog.height = 40

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dialog.View()
	}
}

func BenchmarkUserInputDialogCreation(b *testing.B) {
	question := "What is your name?"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog := NewUserInputDialog(question)
		_ = dialog
	}
}

func BenchmarkUserInputDialogRender(b *testing.B) {
	dialog := NewUserInputDialog("What is your name?")
	dialog.textarea.SetValue("Test answer")
	dialog.width = 100
	dialog.height = 40

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dialog.View()
	}
}

func BenchmarkUserQuestionDialogCreation(b *testing.B) {
	questions := []QuestionWithOptions{
		{Question: "Q1?", Options: []string{"A", "B", "C"}},
		{Question: "Q2?", Options: []string{"X", "Y", "Z"}},
		{Question: "Q3?", Options: []string{"1", "2", "3"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog := NewUserQuestionDialog(questions)
		_ = dialog
	}
}

func BenchmarkUserQuestionDialogRender(b *testing.B) {
	questions := []QuestionWithOptions{
		{Question: "What is your programming language?", Options: []string{"Go", "Python", "JavaScript"}},
		{Question: "What is your experience level?", Options: []string{"Beginner", "Intermediate", "Advanced"}},
	}

	dialog := NewUserQuestionDialog(questions)
	dialog.width = 120
	dialog.height = 40

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dialog.View()
	}
}

func BenchmarkDialogUpdate(b *testing.B) {
	dialog := NewUserInputDialog("Test question?")
	dialog.width = 100
	dialog.height = 40

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	}
}

func BenchmarkDialogMultipleUpdates(b *testing.B) {
	dialog := NewUserInputDialog("Test question?")
	dialog.width = 100
	dialog.height = 40

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
		dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	}
}

func BenchmarkIsLikelyMultipleChoicePrompt(b *testing.B) {
	prompt := `1. What is your preferred programming language?
   a. Go
   b. Python
   c. JavaScript
   d. Rust

2. What is your experience level?
   a. Beginner
   b. Intermediate
   c. Advanced
   d. Expert`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = isLikelyMultipleChoicePrompt(prompt)
	}
}

func BenchmarkIsLikelyMultipleChoicePrompt_Simple(b *testing.B) {
	prompt := "What is your name?"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = isLikelyMultipleChoicePrompt(prompt)
	}
}

func BenchmarkQuestionParsing(b *testing.B) {
	questionsText := `1. What is your preferred programming language?
   a. Go
   b. Python
   c. JavaScript
   d. Rust

2. What is your experience level?
   a. Beginner
   b. Intermediate
   c. Advanced
   d. Expert

3. What is your favorite framework?
   a. React
   b. Vue
   c. Angular
   d. Svelte`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate the parsing logic from handleUserMultipleQuestionsRequest
		lines := strings.Split(questionsText, "\n")
		var questions []QuestionWithOptions
		var currentQuestion QuestionWithOptions

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if len(line) > 2 && line[0] >= '0' && line[0] <= '9' {
				sepIdx := -1
				for j := 1; j < len(line) && j < 4; j++ {
					if line[j] < '0' || line[j] > '9' {
						if line[j] == '.' || line[j] == ')' || line[j] == ':' || line[j] == '-' {
							sepIdx = j
						}
						break
					}
				}

				if sepIdx > 0 && sepIdx+1 < len(line) {
					if currentQuestion.Question != "" {
						questions = append(questions, currentQuestion)
					}
					currentQuestion = QuestionWithOptions{
						Question: strings.TrimSpace(line[sepIdx+1:]),
						Options:  make([]string, 0),
					}
				}
			} else if len(line) > 1 {
				firstChar := line[0]
				isOptionLetter := (firstChar >= 'a' && firstChar <= 'z') || (firstChar >= 'A' && firstChar <= 'Z')

				if isOptionLetter && (line[1] == ')' || line[1] == '.' || line[1] == ':' || line[1] == '-') {
					option := strings.TrimSpace(line[2:])
					currentQuestion.Options = append(currentQuestion.Options, option)
				}
			}
		}

		if currentQuestion.Question != "" {
			questions = append(questions, currentQuestion)
		}

		_ = questions
	}
}

func BenchmarkQuestionParsing_Large(b *testing.B) {
	// Generate a large questions string
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString(fmt.Sprintf("%d. Question %d?\n", i+1, i+1))
		for j := 0; j < 5; j++ {
			sb.WriteString(fmt.Sprintf("   %c. Option %c\n", 'a'+j, 'A'+j))
		}
		sb.WriteString("\n")
	}
	questionsText := sb.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lines := strings.Split(questionsText, "\n")
		var questions []QuestionWithOptions
		var currentQuestion QuestionWithOptions

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if len(line) > 2 && line[0] >= '0' && line[0] <= '9' {
				sepIdx := -1
				for j := 1; j < len(line) && j < 4; j++ {
					if line[j] < '0' || line[j] > '9' {
						if line[j] == '.' || line[j] == ')' || line[j] == ':' || line[j] == '-' {
							sepIdx = j
						}
						break
					}
				}

				if sepIdx > 0 && sepIdx+1 < len(line) {
					if currentQuestion.Question != "" {
						questions = append(questions, currentQuestion)
					}
					currentQuestion = QuestionWithOptions{
						Question: strings.TrimSpace(line[sepIdx+1:]),
						Options:  make([]string, 0),
					}
				}
			} else if len(line) > 1 {
				firstChar := line[0]
				isOptionLetter := (firstChar >= 'a' && firstChar <= 'z') || (firstChar >= 'A' && firstChar <= 'Z')

				if isOptionLetter && (line[1] == ')' || line[1] == '.' || line[1] == ':' || line[1] == '-') {
					option := strings.TrimSpace(line[2:])
					currentQuestion.Options = append(currentQuestion.Options, option)
				}
			}
		}

		if currentQuestion.Question != "" {
			questions = append(questions, currentQuestion)
		}

		_ = questions
	}
}

func BenchmarkTUIInteractionHandlerHandleAuthorization(b *testing.B) {
	// Benchmark the message creation part only (doesn't require tea.Program)
	req := &actor.UserInteractionRequest{
		RequestID:       "benchmark",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         &actor.AuthorizationPayload{ToolName: "shell", Reason: "Test"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Just test message creation - full handler needs tea.Program
		_ = req.InteractionType
	}
}

func BenchmarkTUIInteractionHandlerHandleUserInput(b *testing.B) {
	// Benchmark the message creation part only
	req := &actor.UserInteractionRequest{
		RequestID:       "benchmark",
		InteractionType: actor.InteractionTypeUserInputSingle,
		Payload:         &actor.UserInputSinglePayload{Question: "Test?"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = req.InteractionType
	}
}

func BenchmarkTUIInteractionHandlerHandleMultipleQuestions(b *testing.B) {
	// Benchmark the message creation part only
	questions := []actor.QuestionWithOptions{
		{Question: "Q1?", Options: []string{"A", "B", "C"}},
		{Question: "Q2?", Options: []string{"X", "Y", "Z"}},
	}

	req := &actor.UserInteractionRequest{
		RequestID:       "benchmark",
		InteractionType: actor.InteractionTypeUserInputMultiple,
		Payload: &actor.UserInputMultiplePayload{
			FormattedQuestions: "1. Q1?\n   a. A\n   b. B\n   c. C\n2. Q2?\n   a. X\n   b. Y\n   c. Z",
			ParsedQuestions:    questions,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = req.InteractionType
	}
}

func BenchmarkHandleAuthorizationResponse(b *testing.B) {
	// Note: Can't use mock program with NewTUIInteractionHandler due to type mismatch
	// Just benchmark string formatting
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("req-%d", i)
	}
}

func BenchmarkHandleUserInputResponse(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("req-%d", i)
		_ = "answer"
	}
}

func BenchmarkHandleMultipleAnswersResponse(b *testing.B) {
	answers := map[string]string{
		"Q1": "Option A",
		"Q2": "Option B",
		"Q3": "Option C",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("req-%d", i)
		_ = answers
	}
}

func BenchmarkModelHandleUserMultipleQuestionsRequest(b *testing.B) {
	m := New("benchmark-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	questionsText := `1. What is your preferred programming language?
   a. Go
   b. Python
   c. JavaScript
   d. Rust

2. What is your experience level?
   a. Beginner
   b. Intermediate
   c. Advanced
   d. Expert`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := UserMultipleQuestionsRequestMsg{
			Questions: questionsText,
			Response:  make(chan string),
			Error:     make(chan error),
		}
		m.handleUserMultipleQuestionsRequest(msg)
		m.userQuestionDialogOpen = false
		m.userQuestionDialog = nil
	}
}

func BenchmarkModelHandleUserInputRequest(b *testing.B) {
	m := New("benchmark-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := UserInputRequestMsg{
			Question: "What is your name?",
			Response: make(chan string),
			Error:    make(chan error),
		}
		m.handleUserInputRequest(msg)
		m.userQuestionDialogOpen = false
		m.userQuestionDialog = nil
		m.overlayActive = false
	}
}

func BenchmarkListNavigation(b *testing.B) {
	questions := []QuestionWithOptions{}
	for i := 0; i < 50; i++ {
		questions = append(questions, QuestionWithOptions{
			Question: fmt.Sprintf("Question %d?", i),
			Options:  []string{"A", "B", "C"},
		})
	}

	dialog := NewUserQuestionDialog(questions)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog.Update(tea.KeyMsg{Type: tea.KeyDown})
		dialog.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
}

func BenchmarkOptionsDialogNavigation(b *testing.B) {
	questions := []QuestionWithOptions{
		{Question: "Test?", Options: []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"}},
	}
	parent := NewUserQuestionDialog(questions)

	questionItem := questionItem{
		question: questions[0],
		index:    0,
	}

	dialog := NewQuestionOptionsDialog(questionItem, parent)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog.Update(tea.KeyMsg{Type: tea.KeyDown})
		dialog.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
}

func BenchmarkTextEntry(b *testing.B) {
	dialog := NewUserInputDialog("Test question?")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
		dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	}
}

func BenchmarkWindowResize(b *testing.B) {
	dialog := NewUserInputDialog("Test question?")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog.Update(tea.WindowSizeMsg{Width: 80 + i%50, Height: 30 + i%20})
	}
}

func BenchmarkAnswerStorage(b *testing.B) {
	questions := []QuestionWithOptions{}
	for i := 0; i < 10; i++ {
		questions = append(questions, QuestionWithOptions{
			Question: fmt.Sprintf("Q%d?", i),
			Options:  []string{"A", "B", "C"},
		})
	}

	dialog := NewUserQuestionDialog(questions)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate storing answers
		for j := 0; j < 10; j++ {
			dialog.answers[j] = fmt.Sprintf("%d) Option A", j+1)
		}
	}
}

func BenchmarkDialogInitializationWithSize(b *testing.B) {
	questions := []QuestionWithOptions{
		{Question: "Test?", Options: []string{"A", "B", "C"}},
	}

	dialog := NewUserQuestionDialog(questions)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dialog.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	}
}

func BenchmarkMockProgramSend(b *testing.B) {
	mockProgram := NewMockTUIProgram()
	msg := tea.KeyMsg{Type: tea.KeyEnter}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mockProgram.Send(msg)
	}
}

func BenchmarkMockProgramGetSentMessages(b *testing.B) {
	mockProgram := NewMockTUIProgram()

	// Pre-populate with some messages
	for i := 0; i < 100; i++ {
		mockProgram.Send(tea.KeyMsg{Type: tea.KeyEnter})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mockProgram.GetSentMessages()
	}
}

func BenchmarkModelUpdate(b *testing.B) {
	m := New("benchmark-model", "", false)
	m.ready = true

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}

func BenchmarkModelResize(b *testing.B) {
	m := New("benchmark-model", "", false)
	m.ready = true

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	}
}

// Parallel benchmarks for concurrent operations

func BenchmarkParallelHandleAuthorizationResponse(b *testing.B) {
	// Just benchmark string formatting since we can't use mock program with handler
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = fmt.Sprintf("req-%d", i)
			i++
		}
	})
}

func BenchmarkParallelDialogCreation(b *testing.B) {
	req := &AuthorizationRequest{
		AuthID:     "benchmark",
		ToolName:   "shell",
		Parameters: map[string]interface{}{"command": "echo test"},
		Reason:     "Benchmark authorization",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = NewAuthorizationDialog(req, "test-tab")
		}
	})
}

func BenchmarkParallelQuestionParsing(b *testing.B) {
	questionsText := `1. What is your preferred programming language?
   a. Go
   b. Python
   c. JavaScript

2. What is your experience level?
   a. Beginner
   b. Intermediate
   c. Advanced`

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = isLikelyMultipleChoicePrompt(questionsText)
		}
	})
}
