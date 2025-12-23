package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Lexer performs lexical analysis on configuration text
type Lexer struct {
	reader *bufio.Reader
	line   int
	column int
	// Current character
	ch rune
	// EOF flag
	eof bool
}

// NewLexer creates a new lexer from an io.Reader
func NewLexer(r io.Reader) *Lexer {
	l := &Lexer{
		reader: bufio.NewReader(r),
		line:   1,
		column: 0,
	}
	l.readChar()
	return l
}

// NextToken returns the next token from the input
func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.eof {
		return Token{Type: TokenEOF, Line: l.line, Column: l.column}
	}

	token := Token{Line: l.line, Column: l.column}

	switch {
	case l.ch == '\n':
		token.Type = TokenEOL
		l.readChar()
		return token
	case l.ch == '#':
		l.skipLine()
		// After comment, return EOL if we hit newline
		return l.NextToken()
	case l.ch == '"':
		return l.readString()
	case isWordChar(l.ch):
		return l.readWord()
	default:
		token.Type = TokenError
		token.Value = fmt.Sprintf("unexpected character: %c", l.ch)
		l.readChar()
		return token
	}
}

// readChar reads the next character from the input
func (l *Lexer) readChar() {
	ch, _, err := l.reader.ReadRune()
	if err != nil {
		l.eof = true
		l.ch = 0
		return
	}

	l.ch = ch
	if ch == '\n' {
		l.line++
		l.column = 0
	} else {
		l.column++
	}
}

// peekChar returns the next character without consuming it
func (l *Lexer) peekChar() rune {
	ch, _, err := l.reader.ReadRune()
	if err != nil {
		return 0
	}
	l.reader.UnreadRune()
	return ch
}

// skipWhitespace skips whitespace except newlines
func (l *Lexer) skipWhitespace() {
	for !l.eof && unicode.IsSpace(l.ch) && l.ch != '\n' {
		l.readChar()
	}
}

// skipLine skips the rest of the current line
func (l *Lexer) skipLine() {
	for !l.eof && l.ch != '\n' {
		l.readChar()
	}
	if l.ch == '\n' {
		l.readChar()
	}
}

// readWord reads a word token
func (l *Lexer) readWord() Token {
	token := Token{Line: l.line, Column: l.column}
	var sb strings.Builder

	for !l.eof && isWordChar(l.ch) {
		sb.WriteRune(l.ch)
		l.readChar()
	}

	value := sb.String()
	if value == "set" {
		token.Type = TokenSet
	} else if isNumber(value) {
		token.Type = TokenNumber
	} else {
		token.Type = TokenWord
	}
	token.Value = value

	return token
}

// isNumber returns true if the string is a pure number
func isNumber(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, ch := range s {
		if !unicode.IsDigit(ch) {
			return false
		}
	}
	return true
}

// readString reads a quoted string token
func (l *Lexer) readString() Token {
	token := Token{Line: l.line, Column: l.column, Type: TokenString}
	var sb strings.Builder

	// Skip opening quote
	l.readChar()

	for !l.eof && l.ch != '"' {
		if l.ch == '\\' {
			l.readChar()
			if l.eof {
				token.Type = TokenError
				token.Value = "unexpected EOF in string"
				return token
			}
			// Simple escape handling
			switch l.ch {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case '"':
				sb.WriteRune('"')
			case '\\':
				sb.WriteRune('\\')
			default:
				sb.WriteRune(l.ch)
			}
		} else {
			sb.WriteRune(l.ch)
		}
		l.readChar()
	}

	if l.eof {
		token.Type = TokenError
		token.Value = "unterminated string"
		return token
	}

	// Skip closing quote
	l.readChar()

	token.Value = sb.String()
	return token
}

// isWordChar returns true if the character is valid in a word
func isWordChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '-' || ch == '_' || ch == '/' || ch == '.' || ch == ':'
}
