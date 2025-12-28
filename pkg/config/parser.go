package config

import (
	"fmt"
	"io"
	"net"
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
	case "routing-options":
		return p.parseRoutingOptions(config)
	case "protocols":
		return p.parseProtocols(config)
	case "policy-options":
		return p.parsePolicyOptions(config)
	case "security":
		return p.parseSecurity(config)
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

// parseRoutingOptions parses routing-options configuration
func (p *Parser) parseRoutingOptions(config *Config) error {
	if p.current.Type != TokenWord {
		return p.error("expected routing-options parameter")
	}

	param := p.current.Value
	p.nextToken()

	if config.RoutingOptions == nil {
		config.RoutingOptions = &RoutingOptions{}
	}

	switch param {
	case "autonomous-system":
		return p.parseAutonomousSystem(config.RoutingOptions)
	case "router-id":
		return p.parseRouterID(config.RoutingOptions)
	case "static":
		return p.parseStaticRoute(config.RoutingOptions)
	default:
		return p.error(fmt.Sprintf("unsupported routing-options parameter: %s", param))
	}
}

// parseAutonomousSystem parses autonomous-system configuration
func (p *Parser) parseAutonomousSystem(ro *RoutingOptions) error {
	if p.current.Type != TokenNumber {
		return p.error("expected AS number")
	}

	asn, err := strconv.ParseUint(p.current.Value, 10, 32)
	if err != nil {
		return p.error(fmt.Sprintf("invalid AS number: %s", p.current.Value))
	}

	if asn < 1 || asn > 4294967295 {
		return p.error(fmt.Sprintf("AS number out of range (1-4294967295): %d", asn))
	}

	ro.AutonomousSystem = uint32(asn)
	p.nextToken()
	return nil
}

// parseRouterID parses router-id configuration
func (p *Parser) parseRouterID(ro *RoutingOptions) error {
	if p.current.Type != TokenWord {
		return p.error("expected router-id value")
	}

	ro.RouterID = p.current.Value
	p.nextToken()
	return nil
}

// parseStaticRoute parses static route configuration
func (p *Parser) parseStaticRoute(ro *RoutingOptions) error {
	// Expect "route" keyword
	if p.current.Type != TokenWord || p.current.Value != "route" {
		return p.error("expected 'route' keyword")
	}
	p.nextToken()

	// Expect prefix (CIDR)
	if p.current.Type != TokenWord {
		return p.error("expected route prefix in CIDR format")
	}
	prefix := p.current.Value
	p.nextToken()

	// Expect "next-hop" keyword
	if p.current.Type != TokenWord || p.current.Value != "next-hop" {
		return p.error("expected 'next-hop' keyword")
	}
	p.nextToken()

	// Expect next-hop IP
	if p.current.Type != TokenWord {
		return p.error("expected next-hop IP address")
	}
	nextHop := p.current.Value
	p.nextToken()

	staticRoute := &StaticRoute{
		Prefix:  prefix,
		NextHop: nextHop,
	}

	// Optional: distance
	if p.current.Type == TokenWord && p.current.Value == "distance" {
		p.nextToken()
		if p.current.Type != TokenNumber {
			return p.error("expected distance value")
		}
		distance, err := strconv.Atoi(p.current.Value)
		if err != nil {
			return p.error(fmt.Sprintf("invalid distance value: %s", p.current.Value))
		}
		staticRoute.Distance = distance
		p.nextToken()
	}

	// Check for duplicate prefix
	for _, sr := range ro.StaticRoutes {
		if sr.Prefix == prefix {
			return p.error(fmt.Sprintf("duplicate static route prefix: %s", prefix))
		}
	}

	ro.StaticRoutes = append(ro.StaticRoutes, staticRoute)
	return nil
}

// parseProtocols parses protocols configuration
func (p *Parser) parseProtocols(config *Config) error {
	if p.current.Type != TokenWord {
		return p.error("expected protocol name")
	}

	protocol := p.current.Value
	p.nextToken()

	if config.Protocols == nil {
		config.Protocols = &ProtocolConfig{}
	}

	switch protocol {
	case "bgp":
		return p.parseBGP(config.Protocols)
	case "ospf":
		return p.parseOSPF(config.Protocols)
	default:
		return p.error(fmt.Sprintf("unsupported protocol: %s", protocol))
	}
}

// parseBGP parses BGP protocol configuration
func (p *Parser) parseBGP(pc *ProtocolConfig) error {
	if pc.BGP == nil {
		pc.BGP = &BGPConfig{
			Groups: make(map[string]*BGPGroup),
		}
	}

	if p.current.Type != TokenWord {
		return p.error("expected BGP parameter")
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "group":
		return p.parseBGPGroup(pc.BGP)
	default:
		return p.error(fmt.Sprintf("unsupported BGP parameter: %s", param))
	}
}

// parseBGPGroup parses BGP group configuration
func (p *Parser) parseBGPGroup(bgp *BGPConfig) error {
	// Expect group name
	if p.current.Type != TokenWord {
		return p.error("expected BGP group name")
	}
	groupName := p.current.Value
	p.nextToken()

	if bgp.Groups[groupName] == nil {
		bgp.Groups[groupName] = &BGPGroup{
			Neighbors: make(map[string]*BGPNeighbor),
		}
	}
	group := bgp.Groups[groupName]

	// Expect parameter
	if p.current.Type != TokenWord {
		return p.error("expected BGP group parameter")
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "type":
		return p.parseBGPGroupType(group)
	case "neighbor":
		return p.parseBGPNeighbor(group)
	case "import":
		return p.parseBGPGroupImport(group)
	case "export":
		return p.parseBGPGroupExport(group)
	default:
		return p.error(fmt.Sprintf("unsupported BGP group parameter: %s", param))
	}
}

// parseBGPGroupType parses BGP group type
func (p *Parser) parseBGPGroupType(group *BGPGroup) error {
	if p.current.Type != TokenWord {
		return p.error("expected group type (internal or external)")
	}

	groupType := p.current.Value
	if groupType != "internal" && groupType != "external" {
		return p.error(fmt.Sprintf("invalid group type: %s (must be 'internal' or 'external')", groupType))
	}

	group.Type = groupType
	p.nextToken()
	return nil
}

// parseBGPNeighbor parses BGP neighbor configuration
func (p *Parser) parseBGPNeighbor(group *BGPGroup) error {
	// Expect neighbor IP
	if p.current.Type != TokenWord {
		return p.error("expected neighbor IP address")
	}
	neighborIP := p.current.Value
	p.nextToken()

	if group.Neighbors[neighborIP] == nil {
		group.Neighbors[neighborIP] = &BGPNeighbor{
			IP: neighborIP,
		}
	}
	neighbor := group.Neighbors[neighborIP]

	// Expect parameter
	if p.current.Type != TokenWord {
		return p.error("expected neighbor parameter")
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "peer-as":
		if p.current.Type != TokenNumber {
			return p.error("expected peer AS number")
		}
		peerAS, err := strconv.ParseUint(p.current.Value, 10, 32)
		if err != nil {
			return p.error(fmt.Sprintf("invalid peer AS number: %s", p.current.Value))
		}
		if peerAS < 1 || peerAS > 4294967295 {
			return p.error(fmt.Sprintf("peer AS number out of range (1-4294967295): %d", peerAS))
		}
		neighbor.PeerAS = uint32(peerAS)
		p.nextToken()
		return nil
	case "description":
		if p.current.Type != TokenString && p.current.Type != TokenWord {
			return p.error("expected description text")
		}
		neighbor.Description = p.current.Value
		p.nextToken()
		return nil
	case "local-address":
		if p.current.Type != TokenWord {
			return p.error("expected local address")
		}
		neighbor.LocalAddress = p.current.Value
		p.nextToken()
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported neighbor parameter: %s", param))
	}
}

// parseBGPGroupImport parses BGP group import policy
func (p *Parser) parseBGPGroupImport(group *BGPGroup) error {
	if p.current.Type != TokenWord {
		return p.error("expected import policy name")
	}
	group.Import = p.current.Value
	p.nextToken()
	return nil
}

// parseBGPGroupExport parses BGP group export policy
func (p *Parser) parseBGPGroupExport(group *BGPGroup) error {
	if p.current.Type != TokenWord {
		return p.error("expected export policy name")
	}
	group.Export = p.current.Value
	p.nextToken()
	return nil
}

// parseOSPF parses OSPF protocol configuration
func (p *Parser) parseOSPF(pc *ProtocolConfig) error {
	if pc.OSPF == nil {
		pc.OSPF = &OSPFConfig{
			Areas: make(map[string]*OSPFArea),
		}
	}

	if p.current.Type != TokenWord {
		return p.error("expected OSPF parameter")
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "area":
		return p.parseOSPFArea(pc.OSPF)
	case "router-id":
		return p.parseOSPFRouterID(pc.OSPF)
	default:
		return p.error(fmt.Sprintf("unsupported OSPF parameter: %s", param))
	}
}

// parseOSPFRouterID parses OSPF router-id configuration
func (p *Parser) parseOSPFRouterID(ospf *OSPFConfig) error {
	if p.current.Type != TokenWord {
		return p.error("expected router-id value")
	}

	ospf.RouterID = p.current.Value
	p.nextToken()
	return nil
}

// parseOSPFArea parses OSPF area configuration
func (p *Parser) parseOSPFArea(ospf *OSPFConfig) error {
	// Expect area ID
	if p.current.Type != TokenWord && p.current.Type != TokenNumber {
		return p.error("expected area ID")
	}
	areaID := p.current.Value
	p.nextToken()

	if ospf.Areas[areaID] == nil {
		ospf.Areas[areaID] = &OSPFArea{
			AreaID:     areaID,
			Interfaces: make(map[string]*OSPFInterface),
		}
	}
	area := ospf.Areas[areaID]

	// Expect "interface" keyword
	if p.current.Type != TokenWord || p.current.Value != "interface" {
		return p.error("expected 'interface' keyword")
	}
	p.nextToken()

	// Expect interface name
	if p.current.Type != TokenWord {
		return p.error("expected interface name")
	}
	ifName := p.current.Value
	p.nextToken()

	if area.Interfaces[ifName] == nil {
		area.Interfaces[ifName] = &OSPFInterface{
			Name: ifName,
		}
	}
	ospfIf := area.Interfaces[ifName]

	// Optional parameters
	for p.current.Type == TokenWord {
		param := p.current.Value
		p.nextToken()

		switch param {
		case "passive":
			ospfIf.Passive = true
		case "metric":
			if p.current.Type != TokenNumber {
				return p.error("expected metric value")
			}
			metric, err := strconv.Atoi(p.current.Value)
			if err != nil {
				return p.error(fmt.Sprintf("invalid metric value: %s", p.current.Value))
			}
			ospfIf.Metric = metric
			p.nextToken()
		case "priority":
			if p.current.Type != TokenNumber {
				return p.error("expected priority value")
			}
			priority, err := strconv.Atoi(p.current.Value)
			if err != nil {
				return p.error(fmt.Sprintf("invalid priority value: %s", p.current.Value))
			}
			ospfIf.Priority = priority
			p.nextToken()
		default:
			// Not an OSPF interface parameter, break the loop
			return nil
		}
	}

	return nil
}

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
	config.PolicyOptions.PrefixLists[listName].Prefixes = append(
		config.PolicyOptions.PrefixLists[listName].Prefixes,
		prefix,
	)

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
	var asn, value uint32
	var remainder string
	n, err := fmt.Sscanf(community, "%d:%d%s", &asn, &value, &remainder)
	if err != nil || n != 2 {
		return fmt.Errorf("invalid community format %q, expected ASN:value or well-known community (no-export, no-advertise, local-AS, no-peer)", community)
	}

	return nil
}

// parseSecurity parses security configuration (Phase 3)
// Syntax:
//   set security netconf ssh port <port>
//   set security users user <username> password <password>
//   set security users user <username> role <role>
//   set security users user <username> ssh-key "<key>"
//   set security rate-limit per-ip <limit>
//   set security rate-limit per-user <limit>
func (p *Parser) parseSecurity(config *Config) error {
	if p.current.Type != TokenWord {
		return p.error("expected security parameter")
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "netconf":
		return p.parseSecurityNETCONF(config)
	case "users":
		return p.parseSecurityUsers(config)
	case "rate-limit":
		return p.parseSecurityRateLimit(config)
	default:
		return p.error(fmt.Sprintf("unsupported security parameter: %s", param))
	}
}

// parseSecurityNETCONF parses NETCONF configuration
// Syntax: set security netconf ssh port <port>
func (p *Parser) parseSecurityNETCONF(config *Config) error {
	if config.Security == nil {
		config.Security = &SecurityConfig{}
	}

	if p.current.Type != TokenWord || p.current.Value != "ssh" {
		return p.error("expected 'ssh' after 'netconf'")
	}
	p.nextToken()

	if p.current.Type != TokenWord || p.current.Value != "port" {
		return p.error("expected 'port' after 'ssh'")
	}
	p.nextToken()

	if p.current.Type != TokenWord && p.current.Type != TokenNumber {
		return p.error("expected port number")
	}

	port, err := strconv.Atoi(p.current.Value)
	if err != nil {
		return p.error(fmt.Sprintf("invalid port number: %s", p.current.Value))
	}

	if port < 1 || port > 65535 {
		return p.error(fmt.Sprintf("port number out of range: %d", port))
	}

	if config.Security.NETCONF == nil {
		config.Security.NETCONF = &NETCONFConfig{}
	}
	if config.Security.NETCONF.SSH == nil {
		config.Security.NETCONF.SSH = &NETCONFSSHConfig{}
	}
	config.Security.NETCONF.SSH.Port = port

	p.nextToken()
	return nil
}

// parseSecurityUsers parses user configuration
// Syntax:
//   set security users user <username> password <password>
//   set security users user <username> role <role>
//   set security users user <username> ssh-key "<key>"
func (p *Parser) parseSecurityUsers(config *Config) error {
	if config.Security == nil {
		config.Security = &SecurityConfig{}
	}
	if config.Security.Users == nil {
		config.Security.Users = make(map[string]*UserConfig)
	}

	if p.current.Type != TokenWord || p.current.Value != "user" {
		return p.error("expected 'user' after 'users'")
	}
	p.nextToken()

	if p.current.Type != TokenWord {
		return p.error("expected username")
	}

	username := p.current.Value
	p.nextToken()

	// Get or create user
	if config.Security.Users[username] == nil {
		config.Security.Users[username] = &UserConfig{
			Username: username,
		}
	}
	user := config.Security.Users[username]

	if p.current.Type != TokenWord {
		return p.error("expected user parameter (password, role, ssh-key)")
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "password":
		if p.current.Type != TokenWord {
			return p.error("expected password value")
		}
		user.Password = p.current.Value
		p.nextToken()

	case "role":
		if p.current.Type != TokenWord {
			return p.error("expected role value")
		}
		role := p.current.Value
		if role != "admin" && role != "operator" && role != "read-only" {
			return p.error(fmt.Sprintf("invalid role: %s (must be admin, operator, or read-only)", role))
		}
		user.Role = role
		p.nextToken()

	case "ssh-key":
		if p.current.Type != TokenString {
			return p.error("expected SSH key string")
		}
		user.SSHKey = p.current.Value
		p.nextToken()

	default:
		return p.error(fmt.Sprintf("unsupported user parameter: %s", param))
	}

	return nil
}

// parseSecurityRateLimit parses rate limit configuration
// Syntax:
//   set security rate-limit per-ip <limit>
//   set security rate-limit per-user <limit>
func (p *Parser) parseSecurityRateLimit(config *Config) error {
	if config.Security == nil {
		config.Security = &SecurityConfig{}
	}
	if config.Security.RateLimit == nil {
		config.Security.RateLimit = &RateLimitConfig{}
	}

	if p.current.Type != TokenWord {
		return p.error("expected rate-limit parameter")
	}

	param := p.current.Value
	p.nextToken()

	if p.current.Type != TokenWord && p.current.Type != TokenNumber {
		return p.error("expected rate limit value")
	}

	limit, err := strconv.Atoi(p.current.Value)
	if err != nil {
		return p.error(fmt.Sprintf("invalid rate limit: %s", p.current.Value))
	}

	if limit < 1 || limit > 1000 {
		return p.error(fmt.Sprintf("rate limit out of range: %d (must be 1-1000)", limit))
	}

	switch param {
	case "per-ip":
		config.Security.RateLimit.PerIP = limit
	case "per-user":
		config.Security.RateLimit.PerUser = limit
	default:
		return p.error(fmt.Sprintf("unsupported rate-limit parameter: %s", param))
	}

	p.nextToken()
	return nil
}
