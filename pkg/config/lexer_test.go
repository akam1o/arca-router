package config

import (
	"strings"
	"testing"
)

func TestLexer_BasicTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "set keyword",
			input: "set",
			want: []Token{
				{Type: TokenSet, Value: "set", Line: 1, Column: 1},
				{Type: TokenEOF, Line: 1, Column: 4},
			},
		},
		{
			name:  "simple words",
			input: "interfaces ge-0/0/0",
			want: []Token{
				{Type: TokenWord, Value: "interfaces", Line: 1, Column: 1},
				{Type: TokenWord, Value: "ge-0/0/0", Line: 1, Column: 12},
				{Type: TokenEOF, Line: 1, Column: 20},
			},
		},
		{
			name:  "number",
			input: "unit 0",
			want: []Token{
				{Type: TokenWord, Value: "unit", Line: 1, Column: 1},
				{Type: TokenNumber, Value: "0", Line: 1, Column: 6},
				{Type: TokenEOF, Line: 1, Column: 7},
			},
		},
		{
			name:  "quoted string",
			input: `description "WAN Uplink"`,
			want: []Token{
				{Type: TokenWord, Value: "description", Line: 1, Column: 1},
				{Type: TokenString, Value: "WAN Uplink", Line: 1, Column: 13},
				{Type: TokenEOF, Line: 1, Column: 26},
			},
		},
		{
			name:  "CIDR address",
			input: "address 192.168.1.1/24",
			want: []Token{
				{Type: TokenWord, Value: "address", Line: 1, Column: 1},
				{Type: TokenWord, Value: "192.168.1.1/24", Line: 1, Column: 9},
				{Type: TokenEOF, Line: 1, Column: 23},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(strings.NewReader(tt.input))
			for i, want := range tt.want {
				got := lexer.NextToken()
				if got.Type != want.Type {
					t.Errorf("token[%d] type = %v, want %v", i, got.Type, want.Type)
				}
				if got.Value != want.Value {
					t.Errorf("token[%d] value = %q, want %q", i, got.Value, want.Value)
				}
				if got.Line != want.Line {
					t.Errorf("token[%d] line = %d, want %d", i, got.Line, want.Line)
				}
			}
		})
	}
}

func TestLexer_Comments(t *testing.T) {
	input := `# This is a comment
set interfaces ge-0/0/0
# Another comment
description "Test"`

	want := []Token{
		{Type: TokenSet, Value: "set", Line: 2, Column: 1},
		{Type: TokenWord, Value: "interfaces", Line: 2, Column: 5},
		{Type: TokenWord, Value: "ge-0/0/0", Line: 2, Column: 16},
		{Type: TokenEOL, Value: "", Line: 3, Column: 1}, // EOL is on line 3 after reading the newline
		{Type: TokenWord, Value: "description", Line: 4, Column: 1},
		{Type: TokenString, Value: "Test", Line: 4, Column: 13},
		{Type: TokenEOF, Line: 4, Column: 19},
	}

	lexer := NewLexer(strings.NewReader(input))
	for i, wantToken := range want {
		got := lexer.NextToken()
		if got.Type != wantToken.Type {
			t.Errorf("token[%d] type = %v, want %v", i, got.Type, wantToken.Type)
		}
		if got.Value != wantToken.Value {
			t.Errorf("token[%d] value = %q, want %q", i, got.Value, wantToken.Value)
		}
		if got.Line != wantToken.Line {
			t.Errorf("token[%d] line = %d, want %d", i, got.Line, wantToken.Line)
		}
	}
}

func TestLexer_MultiLine(t *testing.T) {
	input := `set interfaces ge-0/0/0
set interfaces ge-0/0/1`

	tokens := []Token{}
	lexer := NewLexer(strings.NewReader(input))
	for {
		tok := lexer.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}

	// Expected: set, interfaces, ge-0/0/0, EOL, set, interfaces, ge-0/0/1, EOF (8 tokens, last line has no EOL)
	if len(tokens) != 8 {
		t.Errorf("got %d tokens, want 8", len(tokens))
		for i, tok := range tokens {
			t.Logf("token[%d]: %s %q (line %d)", i, tok.Type, tok.Value, tok.Line)
		}
	}

	if tokens[0].Line != 1 {
		t.Errorf("first token line = %d, want 1", tokens[0].Line)
	}
	if tokens[3].Line != 2 {
		t.Errorf("fourth token line = %d, want 2", tokens[3].Line)
	}
}

func TestLexer_EscapedString(t *testing.T) {
	input := `"Line 1\nLine 2\t\tTabbed"`

	lexer := NewLexer(strings.NewReader(input))
	tok := lexer.NextToken()

	if tok.Type != TokenString {
		t.Errorf("type = %v, want TokenString", tok.Type)
	}

	want := "Line 1\nLine 2\t\tTabbed"
	if tok.Value != want {
		t.Errorf("value = %q, want %q", tok.Value, want)
	}
}

func TestLexer_UnterminatedString(t *testing.T) {
	input := `"unterminated string`

	lexer := NewLexer(strings.NewReader(input))
	tok := lexer.NextToken()

	if tok.Type != TokenError {
		t.Errorf("type = %v, want TokenError", tok.Type)
	}
	if tok.Value != "unterminated string" {
		t.Errorf("value = %q, want %q", tok.Value, "unterminated string")
	}
}

func TestLexer_Empty(t *testing.T) {
	input := ""

	lexer := NewLexer(strings.NewReader(input))
	tok := lexer.NextToken()

	if tok.Type != TokenEOF {
		t.Errorf("type = %v, want TokenEOF", tok.Type)
	}
}

func TestLexer_OnlyComments(t *testing.T) {
	input := `# Comment 1
# Comment 2
# Comment 3`

	lexer := NewLexer(strings.NewReader(input))
	tok := lexer.NextToken()

	if tok.Type != TokenEOF {
		t.Errorf("type = %v, want TokenEOF", tok.Type)
	}
}

func TestLexer_EOLTokens(t *testing.T) {
	input := "set system host-name router-01\nset interfaces ge-0/0/0 description \"Test\""

	lexer := NewLexer(strings.NewReader(input))
	tokens := []Token{}
	for {
		tok := lexer.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}

	// Expected: set, system, host-name, router-01, EOL, set, interfaces, ge-0/0/0, description, "Test", EOF
	if len(tokens) != 11 {
		t.Errorf("got %d tokens, want 11", len(tokens))
		for i, tok := range tokens {
			t.Logf("token[%d]: %s %q", i, tok.Type, tok.Value)
		}
	}

	// Check that EOL appears after first statement
	if tokens[4].Type != TokenEOL {
		t.Errorf("token[4] type = %v, want EOL", tokens[4].Type)
	}
}
