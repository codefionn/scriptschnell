package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: calculator <expression>")
		os.Exit(1)
	}

	expr := os.Args[1]
	result, err := evaluate(expr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	fmt.Println(result)
}

// evaluate parses arithmetic expressions while respecting operator precedence and parentheses.
func evaluate(expr string) (float64, error) {
	p := &parser{input: expr}
	p.skipSpaces()
	if p.isEnd() {
		return 0, errors.New("empty expression")
	}

	value, err := p.parseExpression()
	if err != nil {
		return 0, err
	}

	p.skipSpaces()
	if !p.isEnd() {
		return 0, fmt.Errorf("unexpected character: %c", p.peek())
	}

	return value, nil
}

type parser struct {
	input string
	pos   int
}

func (p *parser) parseExpression() (float64, error) {
	value, err := p.parseTerm()
	if err != nil {
		return 0, err
	}

	for {
		p.skipSpaces()
		switch {
		case p.match('+'):
			rhs, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			value += rhs
		case p.match('-'):
			rhs, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			value -= rhs
		default:
			return value, nil
		}
	}
}

func (p *parser) parseTerm() (float64, error) {
	value, err := p.parseFactor()
	if err != nil {
		return 0, err
	}

	for {
		p.skipSpaces()
		switch {
		case p.match('*'):
			rhs, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			value *= rhs
		case p.match('/'):
			rhs, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			if rhs == 0 {
				return 0, errors.New("division by zero")
			}
			value /= rhs
		default:
			return value, nil
		}
	}
}

func (p *parser) parseFactor() (float64, error) {
	p.skipSpaces()

	if p.match('+') {
		return p.parseFactor()
	}
	if p.match('-') {
		value, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return -value, nil
	}

	if p.match('(') {
		value, err := p.parseExpression()
		if err != nil {
			return 0, err
		}
		p.skipSpaces()
		if !p.match(')') {
			return 0, errors.New("missing closing parenthesis")
		}
		return value, nil
	}

	return p.parseNumber()
}

func (p *parser) parseNumber() (float64, error) {
	p.skipSpaces()

	start := p.pos
	dotSeen := false
	for !p.isEnd() {
		ch := p.peek()
		if ch >= '0' && ch <= '9' {
			p.pos++
			continue
		}
		if ch == '.' {
			if dotSeen {
				break
			}
			dotSeen = true
			p.pos++
			continue
		}
		break
	}

	if start == p.pos {
		return 0, errors.New("expected number")
	}

	valueStr := p.input[start:p.pos]
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", valueStr)
	}
	return value, nil
}

func (p *parser) skipSpaces() {
	for !p.isEnd() {
		switch p.peek() {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) match(target byte) bool {
	if p.isEnd() || p.peek() != target {
		return false
	}
	p.pos++
	return true
}

func (p *parser) peek() byte {
	return p.input[p.pos]
}

func (p *parser) isEnd() bool {
	return p.pos >= len(p.input)
}
