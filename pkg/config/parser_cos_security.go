package config

import (
	"fmt"
	"strconv"
)

// parseClassOfService parses QoS and traffic-control configuration.
func (p *Parser) parseClassOfService(config *Config) error {
	if config.ClassOfService == nil {
		config.ClassOfService = &ClassOfServiceConfig{
			ForwardingClasses:      make(map[string]*ForwardingClass),
			TrafficControlProfiles: make(map[string]*TrafficControlProfile),
			Interfaces:             make(map[string]*CoSInterface),
		}
	}
	if p.current.Type != TokenWord {
		return p.error("expected class-of-service parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "forwarding-class":
		return p.parseForwardingClass(config.ClassOfService)
	case "traffic-control-profile":
		return p.parseTrafficControlProfile(config.ClassOfService)
	case "interfaces":
		return p.parseCoSInterface(config.ClassOfService)
	default:
		return p.error(fmt.Sprintf("unsupported class-of-service parameter: %s", param))
	}
}

func (p *Parser) parseForwardingClass(cos *ClassOfServiceConfig) error {
	if p.current.Type != TokenWord {
		return p.error("expected forwarding-class name")
	}
	name := p.current.Value
	p.nextToken()
	if p.current.Type != TokenWord || p.current.Value != "queue" {
		return p.error("expected 'queue' after forwarding-class name")
	}
	p.nextToken()
	if p.current.Type != TokenNumber {
		return p.error("expected queue number")
	}
	queue, err := strconv.Atoi(p.current.Value)
	if err != nil {
		return p.error(fmt.Sprintf("invalid queue number: %s", p.current.Value))
	}
	if cos.ForwardingClasses == nil {
		cos.ForwardingClasses = make(map[string]*ForwardingClass)
	}
	cos.ForwardingClasses[name] = &ForwardingClass{Name: name, Queue: queue}
	p.nextToken()
	return nil
}

func (p *Parser) parseTrafficControlProfile(cos *ClassOfServiceConfig) error {
	if p.current.Type != TokenWord {
		return p.error("expected traffic-control-profile name")
	}
	name := p.current.Value
	p.nextToken()
	if cos.TrafficControlProfiles == nil {
		cos.TrafficControlProfiles = make(map[string]*TrafficControlProfile)
	}
	if cos.TrafficControlProfiles[name] == nil {
		cos.TrafficControlProfiles[name] = &TrafficControlProfile{Name: name}
	}
	profile := cos.TrafficControlProfiles[name]

	if p.current.Type != TokenWord {
		return p.error("expected traffic-control-profile parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "shaping-rate":
		if p.current.Type != TokenNumber {
			return p.error("expected shaping-rate value")
		}
		rate, err := strconv.ParseUint(p.current.Value, 10, 64)
		if err != nil {
			return p.error(fmt.Sprintf("invalid shaping-rate: %s", p.current.Value))
		}
		profile.ShapingRate = rate
		p.nextToken()
		return nil
	case "scheduler-map":
		if p.current.Type != TokenWord {
			return p.error("expected scheduler-map name")
		}
		profile.SchedulerMap = p.current.Value
		p.nextToken()
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported traffic-control-profile parameter: %s", param))
	}
}

func (p *Parser) parseCoSInterface(cos *ClassOfServiceConfig) error {
	if p.current.Type != TokenWord {
		return p.error("expected class-of-service interface name")
	}
	name := p.current.Value
	p.nextToken()
	if p.current.Type != TokenWord || p.current.Value != "output-traffic-control-profile" {
		return p.error("expected output-traffic-control-profile")
	}
	p.nextToken()
	if p.current.Type != TokenWord {
		return p.error("expected traffic-control-profile name")
	}
	if cos.Interfaces == nil {
		cos.Interfaces = make(map[string]*CoSInterface)
	}
	cos.Interfaces[name] = &CoSInterface{
		Name:                        name,
		OutputTrafficControlProfile: p.current.Value,
	}
	p.nextToken()
	return nil
}

// parseSecurity parses security configuration (Phase 3)
// Syntax:
//
//	set security netconf ssh enabled <true|false>
//	set security netconf ssh listen-address <address>
//	set security netconf ssh port <port>
//	set security users user <username> password <password>
//	set security users user <username> role <role>
//	set security users user <username> ssh-key "<key>"
//	set security rate-limit per-ip <limit>
//	set security rate-limit per-user <limit>
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
// Syntax:
//
//	set security netconf ssh enabled <true|false>
//	set security netconf ssh listen-address <address>
//	set security netconf ssh port <port>
func (p *Parser) parseSecurityNETCONF(config *Config) error {
	if config.Security == nil {
		config.Security = &SecurityConfig{}
	}

	if p.current.Type != TokenWord || p.current.Value != "ssh" {
		return p.error("expected 'ssh' after 'netconf'")
	}
	p.nextToken()

	if p.current.Type != TokenWord {
		return p.error("expected netconf ssh parameter")
	}
	param := p.current.Value
	p.nextToken()

	if config.Security.NETCONF == nil {
		config.Security.NETCONF = &NETCONFConfig{}
	}
	if config.Security.NETCONF.SSH == nil {
		config.Security.NETCONF.SSH = &NETCONFSSHConfig{}
	}
	ssh := config.Security.NETCONF.SSH

	switch param {
	case "enabled":
		enabled, err := p.parseBool()
		if err != nil {
			return err
		}
		ssh.Enabled = enabled
		ssh.EnabledSet = true
		return nil
	case "listen-address":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected netconf ssh listen address")
		}
		ssh.ListenAddress = p.current.Value
		p.nextToken()
		return nil
	case "port":
		if p.current.Type != TokenWord && p.current.Type != TokenNumber {
			return p.error("expected port number")
		}
		port, err := strconv.Atoi(p.current.Value)
		if err != nil {
			return p.error(fmt.Sprintf("invalid port number: %s", p.current.Value))
		}
		if port < 0 || port > 65535 {
			return p.error(fmt.Sprintf("port number out of range: %d", port))
		}
		ssh.Port = port
		p.nextToken()
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported netconf ssh parameter: %s", param))
	}
}

// parseSecurityUsers parses user configuration
// Syntax:
//
//	set security users user <username> password <password>
//	set security users user <username> role <role>
//	set security users user <username> ssh-key "<key>"
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
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected password value")
		}
		password, err := NormalizePasswordForStorage(p.current.Value)
		if err != nil {
			return p.error(fmt.Sprintf("failed to protect password value: %v", err))
		}
		user.Password = password
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
//
//	set security rate-limit per-ip <limit>
//	set security rate-limit per-user <limit>
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

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
