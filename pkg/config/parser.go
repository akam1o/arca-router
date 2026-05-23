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
	case "chassis":
		return p.parseChassis(config)
	case "interfaces":
		return p.parseInterfaces(config)
	case "routing-options":
		return p.parseRoutingOptions(config)
	case "routing-instances":
		return p.parseRoutingInstances(config)
	case "protocols":
		return p.parseProtocols(config)
	case "policy-options":
		return p.parsePolicyOptions(config)
	case "class-of-service":
		return p.parseClassOfService(config)
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
	case "services":
		return p.parseSystemServices(config)
	default:
		return p.error(fmt.Sprintf("unsupported system parameter: %s", param))
	}
}

func (p *Parser) parseSystemServices(config *Config) error {
	if p.current.Type != TokenWord {
		return p.error("expected system service name")
	}
	service := p.current.Value
	p.nextToken()

	if config.System == nil {
		config.System = &SystemConfig{}
	}
	if config.System.Services == nil {
		config.System.Services = &SystemServicesConfig{}
	}

	switch service {
	case "web-ui":
		return p.parseWebUIService(config.System.Services)
	case "prometheus":
		return p.parsePrometheusService(config.System.Services)
	case "snmp":
		return p.parseSNMPService(config.System.Services)
	default:
		return p.error(fmt.Sprintf("unsupported system service: %s", service))
	}
}

func (p *Parser) parseWebUIService(services *SystemServicesConfig) error {
	if services.WebUI == nil {
		services.WebUI = &WebUIConfig{}
	}
	web := services.WebUI

	if p.current.Type != TokenWord {
		return p.error("expected web-ui parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "enabled":
		return p.parseServiceEnabled(func(enabled bool) {
			web.Enabled = enabled
		})
	case "listen-address":
		return p.parseServiceListenAddress("web-ui", func(listenAddress string) {
			web.ListenAddress = listenAddress
		})
	case "port":
		return p.parseServicePort("web-ui", func(port int) {
			web.Port = port
		})
	default:
		return p.error(fmt.Sprintf("unsupported web-ui parameter: %s", param))
	}
}

func (p *Parser) parsePrometheusService(services *SystemServicesConfig) error {
	if services.Prometheus == nil {
		services.Prometheus = &PrometheusConfig{}
	}
	prometheus := services.Prometheus

	if p.current.Type != TokenWord {
		return p.error("expected prometheus parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "enabled":
		return p.parseServiceEnabled(func(enabled bool) {
			prometheus.Enabled = enabled
		})
	case "listen-address":
		return p.parseServiceListenAddress("prometheus", func(listenAddress string) {
			prometheus.ListenAddress = listenAddress
		})
	case "port":
		return p.parseServicePort("prometheus", func(port int) {
			prometheus.Port = port
		})
	default:
		return p.error(fmt.Sprintf("unsupported prometheus parameter: %s", param))
	}
}

func (p *Parser) parseSNMPService(services *SystemServicesConfig) error {
	if services.SNMP == nil {
		services.SNMP = &SNMPConfig{}
	}
	snmp := services.SNMP

	if p.current.Type != TokenWord {
		return p.error("expected snmp parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "enabled":
		return p.parseServiceEnabled(func(enabled bool) {
			snmp.Enabled = enabled
		})
	case "listen-address":
		return p.parseServiceListenAddress("snmp", func(listenAddress string) {
			snmp.ListenAddress = listenAddress
		})
	case "port":
		return p.parseServicePort("snmp", func(port int) {
			snmp.Port = port
		})
	case "community":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected snmp community")
		}
		snmp.Community = p.current.Value
		p.nextToken()
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported snmp parameter: %s", param))
	}
}

func (p *Parser) parseServiceEnabled(set func(bool)) error {
	enabled, err := p.parseBool()
	if err != nil {
		return err
	}
	set(enabled)
	return nil
}

func (p *Parser) parseServiceListenAddress(serviceName string, set func(string)) error {
	if p.current.Type != TokenWord && p.current.Type != TokenString {
		return p.error(fmt.Sprintf("expected %s listen address", serviceName))
	}
	set(p.current.Value)
	p.nextToken()
	return nil
}

func (p *Parser) parseServicePort(serviceName string, set func(int)) error {
	if p.current.Type != TokenNumber {
		return p.error(fmt.Sprintf("expected %s port", serviceName))
	}
	port, err := strconv.Atoi(p.current.Value)
	if err != nil {
		return p.error(fmt.Sprintf("invalid %s port: %s", serviceName, p.current.Value))
	}
	set(port)
	p.nextToken()
	return nil
}

func (p *Parser) parseBool() (bool, error) {
	if p.current.Type != TokenWord {
		return false, p.error("expected boolean value")
	}
	switch p.current.Value {
	case "true", "yes", "on", "enable", "enabled":
		p.nextToken()
		return true, nil
	case "false", "no", "off", "disable", "disabled":
		p.nextToken()
		return false, nil
	default:
		return false, p.error(fmt.Sprintf("invalid boolean value: %s", p.current.Value))
	}
}

// parseChassis parses chassis-level HA configuration.
func (p *Parser) parseChassis(config *Config) error {
	if p.current.Type != TokenWord || p.current.Value != "cluster" {
		return p.error("expected 'cluster' after chassis")
	}
	p.nextToken()

	if config.Chassis == nil {
		config.Chassis = &ChassisConfig{}
	}
	if config.Chassis.Cluster == nil {
		config.Chassis.Cluster = &ClusterConfig{
			Nodes: make(map[string]*ClusterNode),
		}
	}
	cluster := config.Chassis.Cluster

	if p.current.Type != TokenWord {
		return p.error("expected cluster parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "enabled":
		enabled, err := p.parseBool()
		if err != nil {
			return err
		}
		cluster.Enabled = enabled
		return nil
	case "node":
		return p.parseClusterNode(cluster)
	case "sync":
		return p.parseClusterSync(cluster)
	default:
		return p.error(fmt.Sprintf("unsupported cluster parameter: %s", param))
	}
}

func (p *Parser) parseClusterNode(cluster *ClusterConfig) error {
	if p.current.Type != TokenWord {
		return p.error("expected cluster node name")
	}
	name := p.current.Value
	p.nextToken()

	if cluster.Nodes == nil {
		cluster.Nodes = make(map[string]*ClusterNode)
	}
	if cluster.Nodes[name] == nil {
		cluster.Nodes[name] = &ClusterNode{Name: name}
	}
	node := cluster.Nodes[name]

	if p.current.Type != TokenWord {
		return p.error("expected cluster node parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "address":
		if p.current.Type != TokenWord {
			return p.error("expected cluster node address")
		}
		node.Address = p.current.Value
		p.nextToken()
		return nil
	case "priority":
		if p.current.Type != TokenNumber {
			return p.error("expected cluster node priority")
		}
		priority, err := strconv.Atoi(p.current.Value)
		if err != nil {
			return p.error(fmt.Sprintf("invalid cluster node priority: %s", p.current.Value))
		}
		node.Priority = priority
		p.nextToken()
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported cluster node parameter: %s", param))
	}
}

func (p *Parser) parseClusterSync(cluster *ClusterConfig) error {
	if p.current.Type != TokenWord || p.current.Value != "etcd" {
		return p.error("expected 'etcd' after cluster sync")
	}
	p.nextToken()
	if p.current.Type != TokenWord || p.current.Value != "endpoint" {
		return p.error("expected 'endpoint' after cluster sync etcd")
	}
	p.nextToken()
	if p.current.Type != TokenWord && p.current.Type != TokenString {
		return p.error("expected etcd endpoint")
	}
	if cluster.Sync == nil {
		cluster.Sync = &ClusterSyncConfig{}
	}
	if cluster.Sync.Etcd == nil {
		cluster.Sync.Etcd = &EtcdSyncConfig{}
	}
	cluster.Sync.Etcd.Endpoints = appendUniqueString(cluster.Sync.Etcd.Endpoints, p.current.Value)
	p.nextToken()
	return nil
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
	family.Addresses = appendUniqueString(family.Addresses, address)
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

	for p.current.Type == TokenWord {
		switch p.current.Value {
		case "distance":
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
		case "bfd":
			staticRoute.BFD = true
			p.nextToken()
		case "profile":
			if !staticRoute.BFD {
				return p.error("expected 'bfd' before static route BFD profile")
			}
			p.nextToken()
			if p.current.Type != TokenWord && p.current.Type != TokenString {
				return p.error("expected BFD profile name")
			}
			staticRoute.BFDProfile = p.current.Value
			p.nextToken()
		case "source":
			if !staticRoute.BFD {
				return p.error("expected 'bfd' before static route BFD source")
			}
			p.nextToken()
			if p.current.Type != TokenWord {
				return p.error("expected BFD source address")
			}
			staticRoute.BFDSource = p.current.Value
			p.nextToken()
		case "multi-hop", "multihop":
			if !staticRoute.BFD {
				return p.error("expected 'bfd' before static route BFD multi-hop")
			}
			staticRoute.BFDMultihop = true
			p.nextToken()
		default:
			return p.error(fmt.Sprintf("unsupported static route parameter: %s", p.current.Value))
		}
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

// parseRoutingInstances parses routing-instance configuration.
func (p *Parser) parseRoutingInstances(config *Config) error {
	if p.current.Type != TokenWord {
		return p.error("expected routing-instance name")
	}
	name := p.current.Value
	p.nextToken()

	if config.RoutingInstances == nil {
		config.RoutingInstances = make(map[string]*RoutingInstance)
	}
	if config.RoutingInstances[name] == nil {
		config.RoutingInstances[name] = &RoutingInstance{Name: name}
	}
	instance := config.RoutingInstances[name]

	if p.current.Type != TokenWord {
		return p.error("expected routing-instance parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "instance-type":
		if p.current.Type != TokenWord {
			return p.error("expected routing-instance type")
		}
		instance.InstanceType = p.current.Value
		p.nextToken()
		return nil
	case "route-distinguisher":
		if p.current.Type != TokenWord {
			return p.error("expected route distinguisher")
		}
		instance.RouteDistinguisher = p.current.Value
		p.nextToken()
		return nil
	case "vrf-target":
		if p.current.Type != TokenWord {
			return p.error("expected vrf-target")
		}
		if p.current.Value == "import" || p.current.Value == "export" {
			direction := p.current.Value
			p.nextToken()
			if p.current.Type != TokenWord {
				return p.error(fmt.Sprintf("expected vrf-target %s value", direction))
			}
			switch direction {
			case "import":
				instance.VRFTargetImport = appendUniqueString(instance.VRFTargetImport, p.current.Value)
			case "export":
				instance.VRFTargetExport = appendUniqueString(instance.VRFTargetExport, p.current.Value)
			}
			p.nextToken()
			return nil
		}
		instance.VRFTarget = p.current.Value
		p.nextToken()
		return nil
	case "vrf-import":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected vrf-import policy")
		}
		instance.VRFImport = appendUniqueString(instance.VRFImport, p.current.Value)
		p.nextToken()
		return nil
	case "vrf-export":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected vrf-export policy")
		}
		instance.VRFExport = appendUniqueString(instance.VRFExport, p.current.Value)
		p.nextToken()
		return nil
	case "interface":
		if p.current.Type != TokenWord {
			return p.error("expected routing-instance interface")
		}
		instance.Interfaces = appendUniqueString(instance.Interfaces, p.current.Value)
		p.nextToken()
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported routing-instance parameter: %s", param))
	}
}
