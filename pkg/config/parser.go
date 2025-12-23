package config

import (
	"fmt"
	"io"
	"strconv"

	"github.com/akam1o/arca-router/pkg/errors"
)

// Parser parses set-style configuration
type Parser struct {
	lexer   *Lexer
	current Token
	peek    Token
}

// NewParser creates a new parser from an io.Reader
func NewParser(r io.Reader) *Parser {
	p := &Parser{
		lexer: NewLexer(r),
	}
	// Read two tokens to initialize current and peek
	p.nextToken()
	p.nextToken()
	return p
}

// Parse parses the entire configuration and returns a Config
func (p *Parser) Parse() (*Config, error) {
	config := NewConfig()

	for p.current.Type != TokenEOF {
		// Skip empty lines
		if p.current.Type == TokenEOL {
			p.nextToken()
			continue
		}

		if err := p.parseStatement(config); err != nil {
			return nil, err
		}

		// Expect EOL or EOF after each statement
		if p.current.Type != TokenEOL && p.current.Type != TokenEOF {
			return nil, p.error("expected end of line after statement")
		}

		// Consume the EOL token
		if p.current.Type == TokenEOL {
			p.nextToken()
		}
	}

	return config, nil
}

// nextToken advances to the next token
func (p *Parser) nextToken() {
	p.current = p.peek
	p.peek = p.lexer.NextToken()
}

// parseStatement parses a single set statement
func (p *Parser) parseStatement(config *Config) error {
	// Check for lexer errors
	if p.current.Type == TokenError {
		return p.lexerError(p.current.Value)
	}

	// Expect "set" keyword
	if p.current.Type != TokenSet {
		return p.error(fmt.Sprintf("expected 'set', got %s", p.current.Type))
	}
	p.nextToken()

	// Check for lexer errors
	if p.current.Type == TokenError {
		return p.lexerError(p.current.Value)
	}

	// Determine the top-level keyword
	if p.current.Type != TokenWord {
		return p.error(fmt.Sprintf("expected keyword after 'set', got %s", p.current.Type))
	}

	keyword := p.current.Value
	p.nextToken()

	switch keyword {
	case "system":
		return p.parseSystem(config)
	case "interfaces":
		return p.parseInterfaces(config)
	default:
		return p.error(fmt.Sprintf("unsupported keyword: %s", keyword))
	}
}

// parseSystem parses system configuration
func (p *Parser) parseSystem(config *Config) error {
	if p.current.Type != TokenWord {
		return p.error("expected system parameter")
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "host-name":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected hostname value")
		}
		if config.System == nil {
			config.System = &SystemConfig{}
		}
		config.System.HostName = p.current.Value
		p.nextToken()
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported system parameter: %s", param))
	}
}

// parseInterfaces parses interface configuration
func (p *Parser) parseInterfaces(config *Config) error {
	// Expect interface name
	if p.current.Type != TokenWord {
		return p.error("expected interface name")
	}

	ifName := p.current.Value
	p.nextToken()

	iface := config.GetOrCreateInterface(ifName)

	// Determine the interface parameter
	if p.current.Type != TokenWord {
		return p.error("expected interface parameter")
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "description":
		return p.parseInterfaceDescription(iface)
	case "unit":
		return p.parseInterfaceUnit(iface)
	default:
		return p.error(fmt.Sprintf("unsupported interface parameter: %s", param))
	}
}

// parseInterfaceDescription parses interface description
func (p *Parser) parseInterfaceDescription(iface *Interface) error {
	if p.current.Type != TokenString && p.current.Type != TokenWord {
		return p.error("expected description text")
	}

	iface.Description = p.current.Value
	p.nextToken()
	return nil
}

// parseInterfaceUnit parses interface unit configuration
func (p *Parser) parseInterfaceUnit(iface *Interface) error {
	// Expect unit number
	if p.current.Type != TokenNumber {
		return p.error("expected unit number")
	}

	unitNum, err := strconv.Atoi(p.current.Value)
	if err != nil {
		return p.error(fmt.Sprintf("invalid unit number: %s", p.current.Value))
	}
	p.nextToken()

	unit := iface.GetOrCreateUnit(unitNum)

	// Expect "family" keyword
	if p.current.Type != TokenWord || p.current.Value != "family" {
		return p.error("expected 'family' keyword")
	}
	p.nextToken()

	// Expect family name (inet, inet6)
	if p.current.Type != TokenWord {
		return p.error("expected family name")
	}

	familyName := p.current.Value
	p.nextToken()

	family := unit.GetOrCreateFamily(familyName)

	// Expect "address" keyword
	if p.current.Type != TokenWord || p.current.Value != "address" {
		return p.error("expected 'address' keyword")
	}
	p.nextToken()

	// Expect CIDR address
	if p.current.Type != TokenWord {
		return p.error("expected IP address in CIDR format")
	}

	address := p.current.Value
	family.Addresses = append(family.Addresses, address)
	p.nextToken()

	return nil
}

// error creates a parse error
func (p *Parser) error(msg string) error {
	return errors.New(
		errors.ErrCodeConfigParseError,
		fmt.Sprintf("Parse error at line %d, column %d: %s", p.current.Line, p.current.Column, msg),
		"The configuration file contains invalid syntax",
		"Review the configuration file and fix the syntax error",
	)
}

// lexerError creates an error from a lexer error message
func (p *Parser) lexerError(msg string) error {
	return errors.New(
		errors.ErrCodeConfigParseError,
		fmt.Sprintf("Lexer error at line %d, column %d: %s", p.current.Line, p.current.Column, msg),
		"The configuration file contains invalid characters or formatting",
		"Review the configuration file and fix the syntax error",
	)
}
