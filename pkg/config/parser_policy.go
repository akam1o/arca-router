package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// parsePolicyOptions parses policy-options configuration
func (p *Parser) parsePolicyOptions(config *Config) error {
	if p.current.Type != TokenWord {
		return p.error("expected policy-options parameter")
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "prefix-list":
		return p.parsePrefixList(config)
	case "policy-statement":
		return p.parsePolicyStatement(config)
	default:
		return p.error(fmt.Sprintf("unsupported policy-options parameter: %s", param))
	}
}

// parsePrefixList parses a prefix-list configuration
// Format: set policy-options prefix-list <name> <prefix>
func (p *Parser) parsePrefixList(config *Config) error {
	// Expect prefix-list name
	if p.current.Type != TokenWord {
		return p.error("expected prefix-list name")
	}
	listName := p.current.Value
	p.nextToken()

	// Expect prefix (CIDR)
	if p.current.Type != TokenWord {
		return p.error("expected prefix value")
	}
	prefix := p.current.Value

	// Validate CIDR format
	if err := validateCIDR(prefix); err != nil {
		return p.error(fmt.Sprintf("invalid prefix %q: %v", prefix, err))
	}

	p.nextToken()

	// Initialize policy-options if needed
	if config.PolicyOptions == nil {
		config.PolicyOptions = &PolicyOptions{
			PrefixLists:      make(map[string]*PrefixList),
			PolicyStatements: make(map[string]*PolicyStatement),
		}
	}

	// Get or create prefix-list
	if config.PolicyOptions.PrefixLists[listName] == nil {
		config.PolicyOptions.PrefixLists[listName] = &PrefixList{
			Name:     listName,
			Prefixes: make([]string, 0),
		}
	}

	// Add prefix to list
	list := config.PolicyOptions.PrefixLists[listName]
	list.Prefixes = appendUniqueString(list.Prefixes, prefix)

	return nil
}

// parsePolicyStatement parses a policy-statement configuration
// Format: set policy-options policy-statement <name> term <term-name> ...
func (p *Parser) parsePolicyStatement(config *Config) error {
	// Expect policy-statement name
	if p.current.Type != TokenWord {
		return p.error("expected policy-statement name")
	}
	policyName := p.current.Value
	p.nextToken()

	// Expect "term" keyword
	if p.current.Type != TokenWord || p.current.Value != "term" {
		return p.error("expected 'term' keyword")
	}
	p.nextToken()

	// Expect term name
	if p.current.Type != TokenWord {
		return p.error("expected term name")
	}
	termName := p.current.Value
	p.nextToken()

	// Initialize policy-options if needed
	if config.PolicyOptions == nil {
		config.PolicyOptions = &PolicyOptions{
			PrefixLists:      make(map[string]*PrefixList),
			PolicyStatements: make(map[string]*PolicyStatement),
		}
	}

	// Get or create policy-statement
	if config.PolicyOptions.PolicyStatements[policyName] == nil {
		config.PolicyOptions.PolicyStatements[policyName] = &PolicyStatement{
			Name:  policyName,
			Terms: make([]*PolicyTerm, 0),
		}
	}

	// Find or create term
	var term *PolicyTerm
	for _, t := range config.PolicyOptions.PolicyStatements[policyName].Terms {
		if t.Name == termName {
			term = t
			break
		}
	}
	if term == nil {
		term = &PolicyTerm{
			Name: termName,
			From: &PolicyMatchConditions{},
			Then: &PolicyActions{},
		}
		config.PolicyOptions.PolicyStatements[policyName].Terms = append(
			config.PolicyOptions.PolicyStatements[policyName].Terms,
			term,
		)
	}

	// Parse "from" or "then" clauses
	if p.current.Type != TokenWord {
		return p.error("expected 'from' or 'then' keyword")
	}

	keyword := p.current.Value
	p.nextToken()

	switch keyword {
	case "from":
		return p.parsePolicyMatchConditions(term)
	case "then":
		return p.parsePolicyActions(term)
	default:
		return p.error(fmt.Sprintf("expected 'from' or 'then', got '%s'", keyword))
	}
}

// parsePolicyMatchConditions parses match conditions in a policy term
// Format: set policy-options policy-statement <name> term <term> from <condition> <value>
func (p *Parser) parsePolicyMatchConditions(term *PolicyTerm) error {
	if p.current.Type != TokenWord {
		return p.error("expected match condition")
	}

	condition := p.current.Value
	p.nextToken()

	switch condition {
	case "prefix-list":
		// Expect prefix-list name
		if p.current.Type != TokenWord {
			return p.error("expected prefix-list name")
		}
		listName := p.current.Value
		p.nextToken()

		if term.From == nil {
			term.From = &PolicyMatchConditions{}
		}
		term.From.PrefixLists = append(term.From.PrefixLists, listName)
		return nil

	case "protocol":
		// Expect protocol name
		if p.current.Type != TokenWord {
			return p.error("expected protocol name")
		}
		protocol := p.current.Value

		// Validate protocol
		if err := validateProtocol(protocol); err != nil {
			return p.error(fmt.Sprintf("invalid protocol: %v", err))
		}

		p.nextToken()

		if term.From == nil {
			term.From = &PolicyMatchConditions{}
		}
		term.From.Protocol = protocol
		return nil

	case "neighbor":
		// Expect neighbor IP
		if p.current.Type != TokenWord {
			return p.error("expected neighbor IP")
		}
		neighbor := p.current.Value

		// Validate IP address
		if err := validateIPAddress(neighbor); err != nil {
			return p.error(fmt.Sprintf("invalid neighbor IP %q: %v", neighbor, err))
		}

		p.nextToken()

		if term.From == nil {
			term.From = &PolicyMatchConditions{}
		}
		term.From.Neighbor = neighbor
		return nil

	case "as-path":
		// Expect AS path regex
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected AS path pattern")
		}
		asPath := p.current.Value
		p.nextToken()

		if term.From == nil {
			term.From = &PolicyMatchConditions{}
		}
		term.From.ASPath = asPath
		return nil

	default:
		return p.error(fmt.Sprintf("unsupported match condition: %s", condition))
	}
}

// parsePolicyActions parses actions in a policy term
// Format: set policy-options policy-statement <name> term <term> then <action> [value]
func (p *Parser) parsePolicyActions(term *PolicyTerm) error {
	if p.current.Type != TokenWord {
		return p.error("expected action")
	}

	action := p.current.Value
	p.nextToken()

	switch action {
	case "accept":
		if term.Then == nil {
			term.Then = &PolicyActions{}
		}
		acceptValue := true
		term.Then.Accept = &acceptValue
		return nil

	case "reject":
		if term.Then == nil {
			term.Then = &PolicyActions{}
		}
		rejectValue := false
		term.Then.Accept = &rejectValue
		return nil

	case "local-preference":
		// Expect local-preference value
		if p.current.Type != TokenNumber {
			return p.error("expected local-preference value")
		}
		localPref, err := strconv.ParseUint(p.current.Value, 10, 32)
		if err != nil {
			return p.error(fmt.Sprintf("invalid local-preference value: %s", p.current.Value))
		}
		p.nextToken()

		if term.Then == nil {
			term.Then = &PolicyActions{}
		}
		localPrefValue := uint32(localPref)
		term.Then.LocalPreference = &localPrefValue
		return nil

	case "community":
		// Expect community value
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected community value")
		}
		community := p.current.Value

		// Validate community
		if err := validateCommunity(community); err != nil {
			return p.error(fmt.Sprintf("invalid community: %v", err))
		}

		p.nextToken()

		if term.Then == nil {
			term.Then = &PolicyActions{}
		}
		term.Then.Community = community
		return nil

	default:
		return p.error(fmt.Sprintf("unsupported action: %s", action))
	}
}

// validateCIDR validates a CIDR prefix string
func validateCIDR(prefix string) error {
	_, _, err := net.ParseCIDR(prefix)
	if err != nil {
		return fmt.Errorf("invalid CIDR format: %w", err)
	}
	return nil
}

// validateProtocol validates a routing protocol name
func validateProtocol(protocol string) error {
	validProtocols := map[string]bool{
		"bgp":       true,
		"ospf":      true,
		"ospf3":     true,
		"static":    true,
		"connected": true,
		"direct":    true,
		"kernel":    true,
		"rip":       true,
	}
	if !validProtocols[protocol] {
		return fmt.Errorf("unknown protocol %q, valid values: bgp, ospf, ospf3, static, connected, direct, kernel, rip", protocol)
	}
	return nil
}

// validateIPAddress validates an IP address (IPv4 or IPv6)
func validateIPAddress(ip string) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid IP address format")
	}
	return nil
}

// validateCommunity validates a BGP community string
func validateCommunity(community string) error {
	// Valid formats:
	// - "65000:100" (standard community)
	// - "no-export", "no-advertise", "local-AS", "no-peer" (well-known communities)
	wellKnown := map[string]bool{
		"no-export":    true,
		"no-advertise": true,
		"local-AS":     true,
		"no-peer":      true,
	}

	if wellKnown[community] {
		return nil
	}

	// Check standard format: ASN:value (must be exactly this format)
	parts := strings.Split(community, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid community format %q, expected ASN:value or well-known community (no-export, no-advertise, local-AS, no-peer)", community)
	}

	asn, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil || asn > 65535 {
		return fmt.Errorf("invalid community format %q, expected ASN:value or well-known community (no-export, no-advertise, local-AS, no-peer)", community)
	}

	value, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil || value > 65535 {
		return fmt.Errorf("invalid community format %q, expected ASN:value or well-known community (no-export, no-advertise, local-AS, no-peer)", community)
	}

	return nil
}
