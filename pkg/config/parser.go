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
	case "routing-options":
		return p.parseRoutingOptions(config)
	case "protocols":
		return p.parseProtocols(config)
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
