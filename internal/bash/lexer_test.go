package bash

import (
	"testing"
)

func TestLexerBasicTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: "echo",
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "echo hello world",
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenWord, Value: "hello"},
				{Type: TokenWord, Value: "world"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "ls -la",
			expected: []Token{
				{Type: TokenWord, Value: "ls"},
				{Type: TokenWord, Value: "-la"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "grep pattern file.txt",
			expected: []Token{
				{Type: TokenWord, Value: "grep"},
				{Type: TokenWord, Value: "pattern"},
				{Type: TokenWord, Value: "file.txt"},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: %v", i, tok)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
				if tok.Value != tt.expected[i].Value {
					t.Errorf("token %d: expected value %q, got %q", i, tt.expected[i].Value, tok.Value)
				}
			}
		})
	}
}

func TestLexerOperators(t *testing.T) {
	tests := []struct {
		input    string
		expected []TokenType
	}{
		{"cmd1 | cmd2", []TokenType{TokenWord, TokenPipe, TokenWord, TokenEOF}},
		{"cmd1 && cmd2", []TokenType{TokenWord, TokenAnd, TokenWord, TokenEOF}},
		{"cmd1 || cmd2", []TokenType{TokenWord, TokenOr, TokenWord, TokenEOF}},
		{"cmd1 ; cmd2", []TokenType{TokenWord, TokenSemicolon, TokenWord, TokenEOF}},
		{"cmd &", []TokenType{TokenWord, TokenBackground, TokenEOF}},
		{"cmd1\ncmd2", []TokenType{TokenWord, TokenNewline, TokenWord, TokenEOF}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i] {
					t.Errorf("token %d: expected %v, got %v (value: %q)", i, tt.expected[i], tok.Type, tok.Value)
				}
			}
		})
	}
}

func TestLexerRedirections(t *testing.T) {
	tests := []struct {
		input    string
		expected []TokenType
	}{
		{"cat < file", []TokenType{TokenWord, TokenRedirectIn, TokenWord, TokenEOF}},
		{"cat > file", []TokenType{TokenWord, TokenRedirectOut, TokenWord, TokenEOF}},
		{"cat >> file", []TokenType{TokenWord, TokenRedirectAppend, TokenWord, TokenEOF}},
		{"cat 2>&1", []TokenType{TokenWord, TokenNumber, TokenRedirectDupOut, TokenNumber, TokenEOF}},
		{"cat <> file", []TokenType{TokenWord, TokenRedirectInOut, TokenWord, TokenEOF}},
		{"cat >| file", []TokenType{TokenWord, TokenRedirectClobber, TokenWord, TokenEOF}},
		{"cat 2<&1", []TokenType{TokenWord, TokenNumber, TokenRedirectDupIn, TokenNumber, TokenEOF}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: %v (value: %q)", i, tok.Type, tok.Value)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i] {
					t.Errorf("token %d: expected %v, got %v (value: %q)", i, tt.expected[i], tok.Type, tok.Value)
				}
			}
		})
	}
}

func TestLexerHeredoc(t *testing.T) {
	tests := []struct {
		input    string
		expected []TokenType
	}{
		{"cat << EOF", []TokenType{TokenWord, TokenHeredoc, TokenWord, TokenEOF}},
		{"cat <<- EOF", []TokenType{TokenWord, TokenHeredocStrip, TokenWord, TokenEOF}},
		{"cat <<< string", []TokenType{TokenWord, TokenHerestring, TokenWord, TokenEOF}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i] {
					t.Errorf("token %d: expected %v, got %v (value: %q)", i, tt.expected[i], tok.Type, tok.Value)
				}
			}
		})
	}
}

func TestLexerSingleQuotedStrings(t *testing.T) {
	tests := []struct {
		input       string
		expected    []Token
	}{
		{
			input: `'hello world'`,
			expected: []Token{
				{Type: TokenRawString, Value: "hello world"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: `echo 'no $expansion here'`,
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenRawString, Value: "no $expansion here"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: `echo 'it''s escaped'`,
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenRawString, Value: "it's escaped"},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
				if tok.Value != tt.expected[i].Value {
					t.Errorf("token %d: expected value %q, got %q", i, tt.expected[i].Value, tok.Value)
				}
			}
		})
	}
}

func TestLexerDoubleQuotedStrings(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: `"hello world"`,
			expected: []Token{
				{Type: TokenString, Value: "hello world"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: `echo "with $var expansion"`,
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenString, Value: `with $var expansion`},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: `"line1\nline2"`,
			expected: []Token{
				{Type: TokenString, Value: "line1\nline2"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: `"tab\there"`,
			expected: []Token{
				{Type: TokenString, Value: "tab\there"},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: type=%v value=%q", i, tok.Type, tok.Value)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
				if tok.Value != tt.expected[i].Value {
					t.Errorf("token %d: expected value %q, got %q", i, tt.expected[i].Value, tok.Value)
				}
			}
		})
	}
}

func TestLexerVariableExpansion(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: "$VAR",
			expected: []Token{
				{Type: TokenDollar, Value: "$"},
				{Type: TokenWord, Value: "VAR"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "${VAR}",
			expected: []Token{
				{Type: TokenDollarLBrace, Value: "${"},
				{Type: TokenWord, Value: "VAR}"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "$(command)",
			expected: []Token{
				{Type: TokenDollarLParen, Value: "$("},
				{Type: TokenWord, Value: "command"},
				{Type: TokenRParen, Value: ")"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "$((1+2))",
			expected: []Token{
				{Type: TokenDollarLParenParen, Value: "$(("},
				{Type: TokenNumber, Value: "1"},
				{Type: TokenPlus, Value: "+"},
				{Type: TokenNumber, Value: "2"},
				{Type: TokenRParen, Value: ")"},
				{Type: TokenRParen, Value: ")"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "echo $VAR",
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenDollar, Value: "$"},
				{Type: TokenWord, Value: "VAR"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "echo ${VAR:-default}",
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenDollarLBrace, Value: "${"},
				{Type: TokenWord, Value: "VAR:-default}"},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: type=%v value=%q", i, tok.Type, tok.Value)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
				if tok.Value != tt.expected[i].Value {
					t.Errorf("token %d: expected value %q, got %q", i, tt.expected[i].Value, tok.Value)
				}
			}
		})
	}
}

func TestLexerVariableInWord(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: "echo $HOME/file",
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenWord, Value: "$HOME/file"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "echo ${VAR}_suffix",
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenWord, Value: "${VAR}_suffix"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "echo pre_${VAR}_post",
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenWord, Value: "pre_${VAR}_post"},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: type=%v value=%q", i, tok.Type, tok.Value)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
				if tok.Value != tt.expected[i].Value {
					t.Errorf("token %d: expected value %q, got %q", i, tt.expected[i].Value, tok.Value)
				}
			}
		})
	}
}

func TestLexerKeywords(t *testing.T) {
	tests := []struct {
		input    string
		expected []TokenType
	}{
		{"if true", []TokenType{TokenIf, TokenWord, TokenEOF}},
		{"then echo", []TokenType{TokenThen, TokenWord, TokenEOF}},
		{"else echo", []TokenType{TokenElse, TokenWord, TokenEOF}},
		{"elif true", []TokenType{TokenElif, TokenWord, TokenEOF}},
		{"fi", []TokenType{TokenFi, TokenEOF}},
		{"case $var in", []TokenType{TokenCase, TokenDollar, TokenWord, TokenIn, TokenEOF}},
		{"esac", []TokenType{TokenEsac, TokenEOF}},
		{"for i in", []TokenType{TokenFor, TokenWord, TokenIn, TokenEOF}},
		{"while true", []TokenType{TokenWhile, TokenWord, TokenEOF}},
		{"until true", []TokenType{TokenUntil, TokenWord, TokenEOF}},
		{"do echo", []TokenType{TokenDo, TokenWord, TokenEOF}},
		{"done", []TokenType{TokenDone, TokenEOF}},
		{"function name", []TokenType{TokenFunction, TokenWord, TokenEOF}},
		{"export VAR", []TokenType{TokenExport, TokenWord, TokenEOF}},
		{"local var", []TokenType{TokenLocal, TokenWord, TokenEOF}},
		{"declare -a arr", []TokenType{TokenDeclare, TokenWord, TokenWord, TokenEOF}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: %v (value: %q)", i, tok.Type, tok.Value)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i] {
					t.Errorf("token %d: expected %v, got %v (value: %q)", i, tt.expected[i], tok.Type, tok.Value)
				}
			}
		})
	}
}

func TestLexerPunctuation(t *testing.T) {
	tests := []struct {
		input    string
		expected []TokenType
	}{
		{"(cmd)", []TokenType{TokenLParen, TokenWord, TokenRParen, TokenEOF}},
		{"{ cmd; }", []TokenType{TokenLBrace, TokenWord, TokenSemicolon, TokenRBrace, TokenEOF}},
		{"[ -f file ]", []TokenType{TokenLBracket, TokenWord, TokenWord, TokenRBracket, TokenEOF}},
		{"a,b,c", []TokenType{TokenWord, TokenComma, TokenWord, TokenComma, TokenWord, TokenEOF}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i] {
					t.Errorf("token %d: expected %v, got %v (value: %q)", i, tt.expected[i], tok.Type, tok.Value)
				}
			}
		})
	}
}

func TestLexerComments(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: "# this is a comment",
			expected: []Token{
				{Type: TokenComment, Value: "# this is a comment"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "echo hello # comment",
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenWord, Value: "hello"},
				{Type: TokenComment, Value: "# comment"},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
				if tok.Value != tt.expected[i].Value {
					t.Errorf("token %d: expected value %q, got %q", i, tt.expected[i].Value, tok.Value)
				}
			}
		})
	}
}

func TestLexerAssignments(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: "VAR=value",
			expected: []Token{
				{Type: TokenWord, Value: "VAR"},
				{Type: TokenEqual, Value: "="},
				{Type: TokenWord, Value: "value"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "export VAR=value",
			expected: []Token{
				{Type: TokenExport, Value: "export"},
				{Type: TokenWord, Value: "VAR"},
				{Type: TokenEqual, Value: "="},
				{Type: TokenWord, Value: "value"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "VAR='single quoted'",
			expected: []Token{
				{Type: TokenWord, Value: "VAR"},
				{Type: TokenEqual, Value: "="},
				{Type: TokenRawString, Value: "single quoted"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: `VAR="double quoted"`,
			expected: []Token{
				{Type: TokenWord, Value: "VAR"},
				{Type: TokenEqual, Value: "="},
				{Type: TokenString, Value: "double quoted"},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: type=%v value=%q", i, tok.Type, tok.Value)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
				if tok.Value != tt.expected[i].Value {
					t.Errorf("token %d: expected value %q, got %q", i, tt.expected[i].Value, tok.Value)
				}
			}
		})
	}
}

func TestLexerComplexCommands(t *testing.T) {
	tests := []struct {
		input string
		desc  string
		minTokens int // minimum expected tokens
	}{
		{
			input: "cat file.txt | grep pattern | wc -l",
			desc:  "pipeline",
			minTokens: 7,
		},
		{
			input: "if [ -f file ]; then cat file; fi",
			desc:  "if statement",
			minTokens: 10,
		},
		{
			input: "for i in 1 2 3; do echo $i; done",
			desc:  "for loop",
			minTokens: 12,
		},
		{
			input: "while true; do echo loop; done",
			desc:  "while loop",
			minTokens: 8,
		},
		{
			input: "cmd1 && cmd2 || cmd3",
			desc:  "and/or list",
			minTokens: 5,
		},
		{
			input: "echo \"result: $(cat file)\"",
			desc:  "command substitution in string",
			minTokens: 3,
		},
		{
			input: "cat << 'EOF'\nhello\nworld\nEOF",
			desc:  "heredoc",
			minTokens: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			// Check we got at least minTokens (excluding EOF)
			actualTokens := len(tokens) - 1 // exclude EOF
			if actualTokens < tt.minTokens {
				t.Errorf("expected at least %d tokens, got %d", tt.minTokens, actualTokens)
				for i, tok := range tokens {
					t.Logf("token[%d]: type=%v value=%q", i, tok.Type, tok.Value)
				}
			}

			// Last token should be EOF
			if tokens[len(tokens)-1].Type != TokenEOF {
				t.Errorf("expected last token to be EOF, got %v", tokens[len(tokens)-1].Type)
			}
		})
	}
}

func TestLexerPosition(t *testing.T) {
	input := "echo hello\nworld"
	l := NewLexer(input)
	tokens := l.AllTokens()

	// echo should be at line 1, column 1
	echoTok := tokens[0]
	if echoTok.Value != "echo" {
		t.Fatalf("expected first token to be 'echo', got %q", echoTok.Value)
	}
	if echoTok.Position.Line != 1 {
		t.Errorf("expected echo to be on line 1, got %d", echoTok.Position.Line)
	}
	if echoTok.Position.Column != 1 {
		t.Errorf("expected echo to be at column 1, got %d", echoTok.Position.Column)
	}

	// Find 'world' token (after newline)
	var worldTok *Token
	for i, tok := range tokens {
		if tok.Value == "world" {
			worldTok = &tokens[i]
			break
		}
	}

	if worldTok == nil {
		t.Fatal("expected to find 'world' token")
	}
	if worldTok.Position.Line != 2 {
		t.Errorf("expected world to be on line 2, got %d", worldTok.Position.Line)
	}
	if worldTok.Position.Column != 1 {
		t.Errorf("expected world to be at column 1, got %d", worldTok.Position.Column)
	}
}

func TestLexerBacktick(t *testing.T) {
	input := "echo `date`"
	l := NewLexer(input)
	tokens := l.AllTokens()

	if len(tokens) < 2 {
		t.Fatalf("expected at least 2 tokens, got %d", len(tokens))
	}

	// First token should be 'echo'
	if tokens[0].Value != "echo" {
		t.Errorf("expected first token to be 'echo', got %q", tokens[0].Value)
	}

	// Second token should contain the backtick substitution
	if tokens[1].Type != TokenWord {
		t.Errorf("expected second token to be Word, got %v", tokens[1].Type)
	}
	if tokens[1].Value != "`date`" {
		t.Errorf("expected second token value to be '`date`', got %q", tokens[1].Value)
	}
}

func TestLexerSpecialChars(t *testing.T) {
	tests := []struct {
		input string
		tokenType TokenType
	}{
		{"!", TokenBang},
		{"#", TokenHash},
		{"*", TokenStar},
		{"@", TokenAt},
		{"?", TokenQuestion},
		{":", TokenColon},
		{"+", TokenPlus},
		{"-", TokenMinus},
		{"/", TokenSlash},
		{"%", TokenPercent},
		{"^", TokenCaret},
		{"~", TokenTilde},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tok := l.NextToken()

			if tok.Type != tt.tokenType {
				t.Errorf("expected token type %v, got %v", tt.tokenType, tok.Type)
			}
		})
	}
}

func TestLexerUnterminatedString(t *testing.T) {
	tests := []struct {
		input string
		desc  string
	}{
		{`"unterminated`, "unterminated double quote"},
		{`'unterminated`, "unterminated single quote"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			// Should have error token
			hasError := false
			for _, tok := range tokens {
				if tok.Type == TokenError {
					hasError = true
					break
				}
			}

			if !hasError {
				t.Error("expected error token for unterminated string")
			}
		})
	}
}

func TestLexerEscapes(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: `echo hello\ world`,
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenWord, Value: "hello world"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "echo line1\\\nline2",
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenWord, Value: "line1line2"},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: type=%v value=%q", i, tok.Type, tok.Value)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
				if tok.Value != tt.expected[i].Value {
					t.Errorf("token %d: expected value %q, got %q", i, tt.expected[i].Value, tok.Value)
				}
			}
		})
	}
}

func TestLexerMixedQuotes(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: `echo "hello"'world'`,
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenWord, Value: `"hello"'world'`},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: `echo $VAR"suffix"`,
			expected: []Token{
				{Type: TokenWord, Value: "echo"},
				{Type: TokenWord, Value: `$VAR"suffix"`},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: type=%v value=%q", i, tok.Type, tok.Value)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
			}
		})
	}
}

func TestLexerReset(t *testing.T) {
	l := NewLexer("echo hello")

	// Get first token
	tok := l.NextToken()
	if tok.Value != "echo" {
		t.Errorf("expected 'echo', got %q", tok.Value)
	}

	// Reset with new input
	l.Reset("cat file")
	tok = l.NextToken()
	if tok.Value != "cat" {
		t.Errorf("expected 'cat' after reset, got %q", tok.Value)
	}
}

func TestLexFunction(t *testing.T) {
	tokens := Lex("echo hello")

	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens, got %d", len(tokens))
	}

	expected := []struct {
		t   TokenType
		v   string
	}{
		{TokenWord, "echo"},
		{TokenWord, "hello"},
		{TokenEOF, ""},
	}

	for i, exp := range expected {
		if tokens[i].Type != exp.t {
			t.Errorf("token %d: expected type %v, got %v", i, exp.t, tokens[i].Type)
		}
		if tokens[i].Value != exp.v {
			t.Errorf("token %d: expected value %q, got %q", i, exp.v, tokens[i].Value)
		}
	}
}

func TestLexerTokenString(t *testing.T) {
	tests := []struct {
		token    Token
		expected string
	}{
		{Token{Type: TokenEOF, Value: ""}, "EOF"},
		{Token{Type: TokenError, Value: "test error"}, "Error(test error)"},
		{Token{Type: TokenWord, Value: "echo"}, "Word(echo)"},
		{Token{Type: TokenString, Value: "hello"}, "String(hello)"},
		{Token{Type: TokenPipe, Value: "|"}, "Pipe"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.token.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestLexerNumbers(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: "123",
			expected: []Token{
				{Type: TokenNumber, Value: "123"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			input: "cat 2>&1",
			expected: []Token{
				{Type: TokenWord, Value: "cat"},
				{Type: TokenNumber, Value: "2"},
				{Type: TokenRedirectDupOut, Value: ">&"},
				{Type: TokenNumber, Value: "1"},
				{Type: TokenEOF, Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer(tt.input)
			tokens := l.AllTokens()

			if len(tokens) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(tokens))
				for i, tok := range tokens {
					t.Logf("token[%d]: type=%v value=%q", i, tok.Type, tok.Value)
				}
				return
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i].Type {
					t.Errorf("token %d: expected type %v, got %v", i, tt.expected[i].Type, tok.Type)
				}
				if tok.Value != tt.expected[i].Value {
					t.Errorf("token %d: expected value %q, got %q", i, tt.expected[i].Value, tok.Value)
				}
			}
		})
	}
}
