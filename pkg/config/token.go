package config

// TokenType represents the type of a token
type TokenType int

const (
	// TokenEOF indicates end of file
	TokenEOF TokenType = iota
	// TokenEOL indicates end of line (statement boundary)
	TokenEOL
	// TokenSet is the "set" keyword
	TokenSet
	// TokenWord is a general word token
	TokenWord
	// TokenString is a quoted string
	TokenString
	// TokenNumber is a numeric value
	TokenNumber
	// TokenError indicates a lexer error
	TokenError
)

// Token represents a single token from the lexer
type Token struct {
	Type   TokenType
	Value  string
	Line   int
	Column int
}

// String returns a string representation of the token type
func (t TokenType) String() string {
	switch t {
	case TokenEOF:
		return "EOF"
	case TokenEOL:
		return "EOL"
	case TokenSet:
		return "SET"
	case TokenWord:
		return "WORD"
	case TokenString:
		return "STRING"
	case TokenNumber:
		return "NUMBER"
	case TokenError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}
