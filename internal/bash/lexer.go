// Package bash provides a bash virtual environment with AST parsing,
// command routing, and virtual filesystem support.
package bash

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TokenType represents the type of a token
type TokenType int

const (
	// Special tokens
	TokenEOF TokenType = iota
	TokenError
	TokenComment

	// Literals
	TokenWord       // Unquoted word
	TokenString     // Double-quoted string (with possible expansions)
	TokenRawString  // Single-quoted string (no expansions)
	TokenNumber     // Numeric literal
	TokenAssign     // Variable assignment (follows WORD)

	// Operators
	TokenPipe       // |
	TokenAnd        // &&
	TokenOr         // ||
	TokenSemicolon  // ;
	TokenNewline    // \n
	TokenBackground // &

	// Redirections
	TokenRedirectOut    // >
	TokenRedirectIn     // <
	TokenRedirectAppend // >>
	TokenRedirectInOut  // <>
	TokenRedirectDupIn  // <&
	TokenRedirectDupOut // >&
	TokenRedirectClobber // >|
	TokenHeredoc        // <<
	TokenHeredocStrip   // <<-
	TokenHerestring     // <<<

	// Punctuation
	TokenLParen    // (
	TokenRParen    // )
	TokenLBrace    // {
	TokenRBrace    // }
	TokenLBracket  // [
	TokenRBracket  // ]
	TokenComma     // ,

	// Keywords
	TokenIf
	TokenThen
	TokenElse
	TokenElif
	TokenFi
	TokenCase
	TokenEsac
	TokenIn
	TokenFor
	TokenWhile
	TokenUntil
	TokenDo
	TokenDone
	TokenFunction
	TokenSelect
	TokenTime
	TokenCoproc

	// Builtins that affect parsing
	TokenExport
	TokenLocal
	TokenDeclare
	TokenReadonly
	TokenRead
	TokenShift
	TokenUnset

	// Variable expansion markers
	TokenDollar     // $
	TokenDollarLParen // $(
	TokenDollarLBrace // ${
	TokenDollarLParenParen // $((
	TokenBacktick // `

	// Special parameters
	TokenBang      // !
	TokenHash      // #
	TokenStar      // *
	TokenAt        // @

	// Pattern/regex
	TokenQuestion  // ?
	TokenColon     // :
	TokenEqual     // =
	TokenPlus      // +
	TokenMinus     // -
	TokenSlash     // /
	TokenPercent   // %
	TokenCaret     // ^
	TokenTilde     // ~
)

// Token represents a lexical token
type Token struct {
	Type     TokenType
	Value    string
	Position Position
}

// String returns a string representation of the token
func (t Token) String() string {
	switch t.Type {
	case TokenEOF:
		return "EOF"
	case TokenError:
		return "Error(" + t.Value + ")"
	case TokenComment:
		return "Comment(" + t.Value + ")"
	case TokenWord:
		return "Word(" + t.Value + ")"
	case TokenString:
		return "String(" + t.Value + ")"
	case TokenRawString:
		return "RawString(" + t.Value + ")"
	case TokenNumber:
		return "Number(" + t.Value + ")"
	case TokenAssign:
		return "Assign(" + t.Value + ")"
	default:
		return tokenName(t.Type)
	}
}

// tokenNames maps token types to their string names
var tokenNames = map[TokenType]string{
	TokenEOF:            "EOF",
	TokenError:         "Error",
	TokenComment:       "Comment",
	TokenWord:          "Word",
	TokenString:        "String",
	TokenRawString:     "RawString",
	TokenNumber:        "Number",
	TokenAssign:        "Assign",
	TokenPipe:          "Pipe",
	TokenAnd:           "And",
	TokenOr:            "Or",
	TokenSemicolon:     "Semicolon",
	TokenNewline:       "Newline",
	TokenBackground:    "Background",
	TokenRedirectOut:   "RedirectOut",
	TokenRedirectIn:    "RedirectIn",
	TokenRedirectAppend: "RedirectAppend",
	TokenRedirectInOut: "RedirectInOut",
	TokenRedirectDupIn: "RedirectDupIn",
	TokenRedirectDupOut: "RedirectDupOut",
	TokenRedirectClobber: "RedirectClobber",
	TokenHeredoc:       "Heredoc",
	TokenHeredocStrip:  "HeredocStrip",
	TokenHerestring:    "Herestring",
	TokenLParen:        "LParen",
	TokenRParen:        "RParen",
	TokenLBrace:        "LBrace",
	TokenRBrace:        "RBrace",
	TokenLBracket:      "LBracket",
	TokenRBracket:      "RBracket",
	TokenComma:         "Comma",
	TokenIf:            "If",
	TokenThen:          "Then",
	TokenElse:          "Else",
	TokenElif:          "Elif",
	TokenFi:            "Fi",
	TokenCase:          "Case",
	TokenEsac:          "Esac",
	TokenIn:            "In",
	TokenFor:           "For",
	TokenWhile:         "While",
	TokenUntil:         "Until",
	TokenDo:            "Do",
	TokenDone:          "Done",
	TokenFunction:      "Function",
	TokenSelect:        "Select",
	TokenTime:          "Time",
	TokenCoproc:        "Coproc",
	TokenExport:        "Export",
	TokenLocal:         "Local",
	TokenDeclare:       "Declare",
	TokenReadonly:      "Readonly",
	TokenRead:          "Read",
	TokenShift:         "Shift",
	TokenUnset:         "Unset",
	TokenDollar:        "Dollar",
	TokenDollarLParen:  "DollarLParen",
	TokenDollarLBrace:  "DollarLBrace",
	TokenDollarLParenParen: "DollarLParenParen",
	TokenBacktick:      "Backtick",
	TokenBang:          "Bang",
	TokenHash:          "Hash",
	TokenStar:          "Star",
	TokenAt:            "At",
	TokenQuestion:      "Question",
	TokenColon:         "Colon",
	TokenEqual:         "Equal",
	TokenPlus:          "Plus",
	TokenMinus:         "Minus",
	TokenSlash:         "Slash",
	TokenPercent:       "Percent",
	TokenCaret:         "Caret",
	TokenTilde:         "Tilde",
}

func tokenName(t TokenType) string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("Token(%d)", t)
}

// keywords maps keyword strings to token types
var keywords = map[string]TokenType{
	"if":       TokenIf,
	"then":     TokenThen,
	"else":     TokenElse,
	"elif":     TokenElif,
	"fi":       TokenFi,
	"case":     TokenCase,
	"esac":     TokenEsac,
	"in":       TokenIn,
	"for":      TokenFor,
	"while":    TokenWhile,
	"until":    TokenUntil,
	"do":       TokenDo,
	"done":     TokenDone,
	"function": TokenFunction,
	"select":   TokenSelect,
	"time":     TokenTime,
	"coproc":   TokenCoproc,
	"export":   TokenExport,
	"local":    TokenLocal,
	"declare":  TokenDeclare,
	"readonly": TokenReadonly,
	"read":     TokenRead,
	"shift":    TokenShift,
	"unset":    TokenUnset,
}

// Lexer tokenizes bash script input
type Lexer struct {
	input    string    // Input string
	pos      int       // Current position in input
	start    int       // Start position of current token
	line     int       // Current line number (1-indexed)
	column   int       // Current column number (1-indexed)
	lastChar rune      // Last character read
	width    int       // Width of last rune read
	tokens   []Token   // Token buffer
	heredocs []*heredocPending // Pending heredocs to process
}

type heredocPending struct {
	delimiter string
	stripTabs bool
	expand    bool
	startPos  Position
}

// NewLexer creates a new lexer for the given input
func NewLexer(input string) *Lexer {
	return &Lexer{
		input:  input,
		line:   1,
		column: 1,
		tokens: make([]Token, 0),
		heredocs: make([]*heredocPending, 0),
	}
}

// currentPosition returns the current position
func (l *Lexer) currentPosition() Position {
	return Position{
		Line:   l.line,
		Column: l.column,
		Offset: l.pos,
	}
}

// read reads the next rune from input
func (l *Lexer) read() rune {
	if l.pos >= len(l.input) {
		l.width = 0
		l.lastChar = 0
		return 0
	}

	r, w := utf8.DecodeRuneInString(l.input[l.pos:])
	l.width = w
	l.pos += w
	l.lastChar = r

	// Update line/column tracking
	if r == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}

	return r
}

// unread backs up one rune
func (l *Lexer) unread() {
	if l.width > 0 {
		l.pos -= l.width
		if l.lastChar == '\n' {
			l.line--
			// Note: column tracking is approximate after unread across newline
		} else {
			l.column--
		}
	}
}

// peek returns the next rune without advancing
func (l *Lexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.input[l.pos:])
	return r
}

// peekNext returns the rune after the next without advancing
func (l *Lexer) peekNext() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	_, w := utf8.DecodeRuneInString(l.input[l.pos:])
	if l.pos+w >= len(l.input) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.input[l.pos+w:])
	return r
}

// emit adds a token to the token buffer
func (l *Lexer) emit(t TokenType, startPos Position) {
	value := l.input[l.start:l.pos]
	l.tokens = append(l.tokens, Token{
		Type:     t,
		Value:    value,
		Position: startPos,
	})
	l.start = l.pos
}

// emitToken adds a token with explicit value
func (l *Lexer) emitToken(t TokenType, value string, pos Position) {
	l.tokens = append(l.tokens, Token{
		Type:     t,
		Value:    value,
		Position: pos,
	})
	l.start = l.pos
}

// skipWhitespace skips over whitespace (but not newlines)
func (l *Lexer) skipWhitespace() {
	for {
		r := l.peek()
		if r == ' ' || r == '\t' {
			l.read()
		} else {
			break
		}
	}
	l.start = l.pos
}

// NextToken returns the next token from the lexer
func (l *Lexer) NextToken() Token {
	if len(l.tokens) > 0 {
		t := l.tokens[0]
		l.tokens = l.tokens[1:]
		return t
	}

	return l.nextToken()
}

// nextToken generates and returns the next token
func (l *Lexer) nextToken() Token {
	l.skipWhitespace()
	startPos := l.currentPosition()

	r := l.read()
	if r == 0 {
		return l.makeToken(TokenEOF, "", startPos)
	}

	switch r {
	case '\n':
		tok := l.makeToken(TokenNewline, "", startPos)
		// Process pending heredocs after newline
		l.processHeredocs()
		return tok

	case ';':
		return l.makeToken(TokenSemicolon, "", startPos)

	case '|':
		if l.peek() == '|' {
			l.read()
			return l.makeToken(TokenOr, "", startPos)
		}
		return l.makeToken(TokenPipe, "", startPos)

	case '&':
		if l.peek() == '&' {
			l.read()
			return l.makeToken(TokenAnd, "", startPos)
		}
		return l.makeToken(TokenBackground, "", startPos)

	case '<':
		return l.lexLessThan(startPos)

	case '>':
		return l.lexGreaterThan(startPos)

	case '(':
		return l.makeToken(TokenLParen, "", startPos)

	case ')':
		return l.makeToken(TokenRParen, "", startPos)

	case '{':
		return l.makeToken(TokenLBrace, "", startPos)

	case '}':
		return l.makeToken(TokenRBrace, "", startPos)

	case '[':
		return l.makeToken(TokenLBracket, "", startPos)

	case ']':
		return l.makeToken(TokenRBracket, "", startPos)

	case ',':
		return l.makeToken(TokenComma, "", startPos)

	case '#':
		return l.lexComment(startPos)

	case '$':
		return l.lexDollar(startPos)

	case '`':
		return l.makeToken(TokenBacktick, "", startPos)

	case '!':
		return l.makeToken(TokenBang, "", startPos)

	case '*':
		return l.makeToken(TokenStar, "", startPos)

	case '?':
		return l.makeToken(TokenQuestion, "", startPos)

	case ':':
		return l.makeToken(TokenColon, "", startPos)

	case '=':
		return l.makeToken(TokenEqual, "", startPos)

	case '+':
		return l.makeToken(TokenPlus, "", startPos)

	case '-':
		return l.makeToken(TokenMinus, "", startPos)

	case '/':
		return l.makeToken(TokenSlash, "", startPos)

	case '%':
		return l.makeToken(TokenPercent, "", startPos)

	case '^':
		return l.makeToken(TokenCaret, "", startPos)

	case '~':
		return l.makeToken(TokenTilde, "", startPos)

	case '@':
		return l.makeToken(TokenAt, "", startPos)

	case '\'':
		l.unread()
		return l.lexSingleQuotedString()

	case '"':
		l.unread()
		return l.lexDoubleQuotedString()

	default:
		// Check if it's the start of a word
		if isWordStart(r) {
			l.unread()
			return l.lexWord()
		}

		// Unknown character
		return l.makeToken(TokenError, string(r), startPos)
	}
}

// makeToken creates a token with the proper value
func (l *Lexer) makeToken(t TokenType, value string, pos Position) Token {
	if value == "" {
		value = l.input[l.start:l.pos]
	}
	l.start = l.pos
	return Token{
		Type:     t,
		Value:    value,
		Position: pos,
	}
}

// lexLessThan handles < and related operators
func (l *Lexer) lexLessThan(startPos Position) Token {
	r := l.peek()

	switch r {
	case '<':
		l.read()
		// Check for heredoc, heredoc-strip, or herestring
		r2 := l.peek()
		switch r2 {
		case '-':
			l.read()
			return l.makeToken(TokenHeredocStrip, "", startPos)
		case '<':
			l.read()
			return l.makeToken(TokenHerestring, "", startPos)
		default:
			return l.makeToken(TokenHeredoc, "", startPos)
		}

	case '>':
		l.read()
		return l.makeToken(TokenRedirectInOut, "", startPos)

	case '&':
		l.read()
		return l.makeToken(TokenRedirectDupIn, "", startPos)

	case '(':
		l.read()
		return l.makeToken(TokenDollarLParenParen, "", startPos) // <( is process substitution

	default:
		return l.makeToken(TokenRedirectIn, "", startPos)
	}
}

// lexGreaterThan handles > and related operators
func (l *Lexer) lexGreaterThan(startPos Position) Token {
	r := l.peek()

	switch r {
	case '>':
		l.read()
		return l.makeToken(TokenRedirectAppend, "", startPos)

	case '&':
		l.read()
		return l.makeToken(TokenRedirectDupOut, "", startPos)

	case '|':
		l.read()
		return l.makeToken(TokenRedirectClobber, "", startPos)

	case '(':
		l.read()
		return l.makeToken(TokenError, ">( process substitution not supported", startPos)

	default:
		return l.makeToken(TokenRedirectOut, "", startPos)
	}
}

// lexComment handles # comments
func (l *Lexer) lexComment(startPos Position) Token {
	var buf strings.Builder

	for {
		r := l.read()
		if r == 0 || r == '\n' {
			if r == '\n' {
				l.unread() // Put back newline for next token
			}
			break
		}
		buf.WriteRune(r)
	}

	return l.makeToken(TokenComment, buf.String(), startPos)
}

// lexDollar handles $ and variable expansions
func (l *Lexer) lexDollar(startPos Position) Token {
	r := l.peek()

	switch r {
	case '(':
		l.read()
		r2 := l.peek()
		if r2 == '(' {
			l.read()
			return l.makeToken(TokenDollarLParenParen, "", startPos)
		}
		return l.makeToken(TokenDollarLParen, "", startPos)

	case '{':
		l.read()
		return l.makeToken(TokenDollarLBrace, "", startPos)

	case 0:
		// $ at end of input is just a literal
		return l.makeToken(TokenDollar, "", startPos)

	default:
		// Simple variable like $VAR or $? or $1, etc.
		return l.makeToken(TokenDollar, "", startPos)
	}
}

// lexSingleQuotedString handles single-quoted strings (no expansion)
func (l *Lexer) lexSingleQuotedString() Token {
	startPos := l.currentPosition()
	l.read() // consume opening quote

	var buf strings.Builder

	for {
		r := l.read()
		if r == 0 {
			l.emitToken(TokenError, "unterminated single-quoted string", startPos)
			return
		}
		if r == '\'' {
			// Check for '' (escaped single quote)
			if l.peek() == '\'' {
				l.read()
				buf.WriteRune('\'')
				continue
			}
			break
		}
		buf.WriteRune(r)
	}

	l.emitToken(TokenRawString, buf.String(), startPos)
}

// lexDoubleQuotedString handles double-quoted strings (with expansion)
func (l *Lexer) lexDoubleQuotedString() {
	startPos := l.currentPosition()
	l.read() // consume opening quote

	var buf strings.Builder

	for {
		r := l.read()
		if r == 0 {
			l.emitToken(TokenError, "unterminated double-quoted string", startPos)
			return
		}

		switch r {
		case '"':
			// End of string
			l.emitToken(TokenString, buf.String(), startPos)
			return

		case '\\':
			// Escape sequence
			next := l.read()
			if next == 0 {
				l.emitToken(TokenError, "unterminated escape sequence", startPos)
				return
			}
			switch next {
			case 'n':
				buf.WriteRune('\n')
			case 't':
				buf.WriteRune('\t')
			case 'r':
				buf.WriteRune('\r')
			case '\\', '"', '$', '`':
				buf.WriteRune(next)
			case '\n':
				// Line continuation - skip
			default:
				// Keep backslash for unknown escapes
				buf.WriteRune('\\')
				buf.WriteRune(next)
			}

		case '$':
			// Variable expansion - keep marker for parser
			buf.WriteRune('$')
			if l.peek() == '{' {
				l.read()
				buf.WriteRune('{')
				// Read until closing brace
				braceDepth := 1
				for braceDepth > 0 {
					r := l.read()
					if r == 0 {
						l.emitToken(TokenError, "unterminated variable expansion", startPos)
						return
					}
					buf.WriteRune(r)
					if r == '{' {
						braceDepth++
					} else if r == '}' {
						braceDepth--
					}
				}
			} else if l.peek() == '(' {
				l.read()
				buf.WriteRune('(')
				// Read until closing paren
				parenDepth := 1
				for parenDepth > 0 {
					r := l.read()
					if r == 0 {
						l.emitToken(TokenError, "unterminated command substitution", startPos)
						return
					}
					buf.WriteRune(r)
					if r == '(' {
						parenDepth++
					} else if r == ')' {
						parenDepth--
					}
				}
			} else {
				// Simple variable name
				for {
					r := l.peek()
					if r == 0 || !isNameChar(r) {
						break
					}
					buf.WriteRune(l.read())
				}
			}

		case '`':
			// Command substitution with backticks
			buf.WriteRune('`')
			for {
				r := l.read()
				if r == 0 {
					l.emitToken(TokenError, "unterminated backtick substitution", startPos)
					return
				}
				buf.WriteRune(r)
				if r == '`' {
					break
				}
				if r == '\\' {
					// Escape next character
					if next := l.read(); next != 0 {
						buf.WriteRune(next)
					}
				}
			}

		default:
			buf.WriteRune(r)
		}
	}
}

// lexWord handles unquoted words
func (l *Lexer) lexWord() {
	startPos := l.currentPosition()
	var buf strings.Builder
	hasExpansion := false

	for {
		r := l.peek()
		if r == 0 {
			break
		}

		// Check for special characters that end a word
		if isWordEnd(r) {
			break
		}

		// Handle escape sequences
		if r == '\\' {
			l.read()
			next := l.read()
			if next == 0 {
				break
			}
			if next == '\n' {
				// Line continuation - skip
				continue
			}
			buf.WriteRune(next)
			continue
		}

		// Handle single quotes in word
		if r == '\'' {
			l.read()
			hasExpansion = true
			// Read single-quoted content
			for {
				r := l.read()
				if r == 0 {
					l.emitToken(TokenError, "unterminated single quote in word", startPos)
					return
				}
				if r == '\'' {
					// Check for ''
					if l.peek() == '\'' {
						l.read()
						buf.WriteRune('\'')
						continue
					}
					break
				}
				buf.WriteRune(r)
			}
			continue
		}

		// Handle double quotes in word
		if r == '"' {
			l.read()
			hasExpansion = true
			// Read double-quoted content
			for {
				r := l.read()
				if r == 0 {
					l.emitToken(TokenError, "unterminated double quote in word", startPos)
					return
				}
				if r == '"' {
					break
				}
				if r == '\\' {
					if next := l.read(); next != 0 {
						buf.WriteRune(next)
					}
					continue
				}
				buf.WriteRune(r)
			}
			continue
		}

		// Handle variable expansion
		if r == '$' {
			l.read()
			hasExpansion = true
			buf.WriteRune('$')

			next := l.peek()
			switch next {
			case '{':
				l.read()
				buf.WriteRune('{')
				braceDepth := 1
				for braceDepth > 0 {
					r := l.read()
					if r == 0 {
						l.emitToken(TokenError, "unterminated variable expansion", startPos)
						return
					}
					buf.WriteRune(r)
					if r == '{' {
						braceDepth++
					} else if r == '}' {
						braceDepth--
					}
				}
			case '(':
				l.read()
				r2 := l.peek()
				if r2 == '(' {
					// Arithmetic expansion $(( ... ))
					l.read()
					buf.WriteString("((")
					parenDepth := 2
					for parenDepth > 0 {
						r := l.read()
						if r == 0 {
							l.emitToken(TokenError, "unterminated arithmetic expansion", startPos)
							return
						}
						buf.WriteRune(r)
						if r == '(' {
							parenDepth++
						} else if r == ')' {
							parenDepth--
						}
					}
				} else {
					// Command substitution $( ... )
					buf.WriteRune('(')
					parenDepth := 1
					for parenDepth > 0 {
						r := l.read()
						if r == 0 {
							l.emitToken(TokenError, "unterminated command substitution", startPos)
							return
						}
						buf.WriteRune(r)
						if r == '(' {
							parenDepth++
						} else if r == ')' {
							parenDepth--
						}
					}
				}
			default:
				// Simple variable or special parameter
				if isNameStart(next) || isSpecialParam(next) {
					for {
						r := l.peek()
						if r == 0 || !isNameChar(r) {
							break
						}
						buf.WriteRune(l.read())
					}
				}
			}
			continue
		}

		// Handle backtick command substitution
		if r == '`' {
			l.read()
			hasExpansion = true
			buf.WriteRune('`')
			for {
				r := l.read()
				if r == 0 {
					l.emitToken(TokenError, "unterminated backtick substitution", startPos)
					return
				}
				buf.WriteRune(r)
				if r == '`' {
					break
				}
				if r == '\\' {
					if next := l.read(); next != 0 {
						buf.WriteRune(next)
					}
				}
			}
			continue
		}

		// Handle brace expansion start {
		if r == '{' {
			l.read()
			buf.WriteRune('{')
			// Could be brace expansion {a,b} or just literal {
			continue
		}

		// Handle brace expansion end }
		if r == '}' {
			l.read()
			buf.WriteRune('}')
			continue
		}

		// Handle equals sign (assignment)
		if r == '=' && buf.Len() > 0 {
			// Check if this is a valid assignment
			wordSoFar := buf.String()
			if isValidAssignmentName(wordSoFar) {
				l.read()
				l.emitToken(TokenWord, wordSoFar, startPos)
				l.start = l.pos - 1
				l.emitToken(TokenEqual, "=", Position{
					Line:   l.line,
					Column: l.column - 1,
					Offset: l.pos - 1,
				})
				l.start = l.pos
				// Now lex the value
				l.lexWord() // The value becomes the next token
				return
			}
		}

		// Regular character
		l.read()
		buf.WriteRune(r)
	}

	word := buf.String()

	// Check for keywords
	if tokType, ok := keywords[word]; ok {
		l.emitToken(tokType, word, startPos)
		return
	}

	// Check if it's a number
	if isNumber(word) {
		l.emitToken(TokenNumber, word, startPos)
		return
	}

	// Regular word
	tokenType := TokenWord
	if hasExpansion {
		tokenType = TokenWord // Keep as word, parser will handle expansions
	}

	l.emitToken(tokenType, word, startPos)
}

// processHeredocs handles pending heredocs after a newline
func (l *Lexer) processHeredocs() {
	for _, h := range l.heredocs {
		l.processHeredoc(h)
	}
	l.heredocs = l.heredocs[:0]
}

func (l *Lexer) processHeredoc(h *heredocPending) {
	startPos := h.startPos
	var buf strings.Builder

	for {
		// Read a line
		lineStart := l.pos
		for {
			r := l.read()
			if r == 0 || r == '\n' {
				break
			}
		}
		line := l.input[lineStart:l.pos]

		// Strip leading tabs if heredoc- was used
		if h.stripTabs {
			line = strings.TrimLeft(line, "\t")
		}

		// Check for delimiter
		if strings.TrimRight(line, "\n") == h.delimiter {
			// End of heredoc
			break
		}

		buf.WriteString(line)
	}

	l.emitToken(TokenString, buf.String(), startPos)
}

// collectHeredocInfo collects heredoc delimiter info after << or <<-
func (l *Lexer) collectHeredocInfo(stripTabs bool) {
	l.skipWhitespace()
	startPos := l.currentPosition()

	// Read the delimiter
	var delimBuf strings.Builder
	expand := true

	r := l.peek()
	if r == '\'' {
		// Single-quoted delimiter - no expansion
		l.read()
		expand = false
		for {
			r := l.read()
			if r == 0 || r == '\'' {
				break
			}
			delimBuf.WriteRune(r)
		}
	} else if r == '"' {
		// Double-quoted delimiter
		l.read()
		expand = false
		for {
			r := l.read()
			if r == 0 || r == '"' {
				break
			}
			if r == '\\' {
				if next := l.read(); next != 0 {
					delimBuf.WriteRune(next)
				}
				continue
			}
			delimBuf.WriteRune(r)
		}
	} else {
		// Unquoted delimiter
		for {
			r := l.peek()
			if r == 0 || isWordEnd(r) || r == '\'' || r == '"' {
				break
			}
			delimBuf.WriteRune(l.read())
		}
	}

	l.heredocs = append(l.heredocs, &heredocPending{
		delimiter: delimBuf.String(),
		stripTabs: stripTabs,
		expand:    expand,
		startPos:  startPos,
	})
}

// AllTokens returns all tokens from the lexer
func (l *Lexer) AllTokens() []Token {
	tokens := make([]Token, 0)
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF || tok.Type == TokenError {
			break
		}
	}
	return tokens
}

// Reset resets the lexer with new input
func (l *Lexer) Reset(input string) {
	l.input = input
	l.pos = 0
	l.start = 0
	l.line = 1
	l.column = 1
	l.tokens = l.tokens[:0]
	l.heredocs = l.heredocs[:0]
}

// Helper functions

func isWordStart(r rune) bool {
	return r != 0 && !unicode.IsSpace(r) && !isWordEnd(r)
}

func isWordEnd(r rune) bool {
	switch r {
	case ' ', '\t', '\n', ';', '|', '&', '(', ')', '{', '}', '<', '>', '#':
		return true
	default:
		return false
	}
}

func isNameStart(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

func isNameChar(r rune) bool {
	return isNameStart(r) || (r >= '0' && r <= '9')
}

func isSpecialParam(r rune) bool {
	switch r {
	case '?', '$', '!', '#', '@', '*', '-', '0', '_', '%':
		return true
	default:
		return false
	}
}

func isValidAssignmentName(s string) bool {
	if len(s) == 0 {
		return false
	}
	if !isNameStart(rune(s[0])) {
		return false
	}
	for _, c := range s[1:] {
		if !isNameChar(c) {
			return false
		}
	}
	return true
}

func isNumber(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Lex creates a lexer and returns all tokens
func Lex(input string) []Token {
	l := NewLexer(input)
	return l.AllTokens()
}
