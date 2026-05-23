package config

import (
	"fmt"
	"strconv"
)

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
	case "bfd":
		return p.parseBFD(config.Protocols)
	case "bgp":
		return p.parseBGP(config.Protocols)
	case "evpn":
		return p.parseEVPN(config.Protocols)
	case "ospf":
		return p.parseOSPF(config.Protocols)
	case "ospf3":
		return p.parseOSPF3(config.Protocols)
	case "mpls":
		return p.parseMPLS(config.Protocols)
	case "vrrp":
		return p.parseVRRP(config.Protocols)
	default:
		return p.error(fmt.Sprintf("unsupported protocol: %s", protocol))
	}
}

func (p *Parser) parseEVPN(pc *ProtocolConfig) error {
	if pc.EVPN == nil {
		pc.EVPN = &EVPNConfig{VNIs: make(map[int]*EVPNVNI)}
	}
	if p.current.Type != TokenWord || p.current.Value != "vni" {
		return p.error("expected 'vni' after protocols evpn")
	}
	p.nextToken()
	if p.current.Type != TokenNumber {
		return p.error("expected EVPN VNI")
	}
	vni, err := strconv.Atoi(p.current.Value)
	if err != nil {
		return p.error(fmt.Sprintf("invalid EVPN VNI: %s", p.current.Value))
	}
	p.nextToken()
	if pc.EVPN.VNIs == nil {
		pc.EVPN.VNIs = make(map[int]*EVPNVNI)
	}
	if pc.EVPN.VNIs[vni] == nil {
		pc.EVPN.VNIs[vni] = &EVPNVNI{VNI: vni}
	}
	evpnVNI := pc.EVPN.VNIs[vni]

	if p.current.Type != TokenWord {
		return p.error("expected EVPN VNI parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "type":
		if p.current.Type != TokenWord {
			return p.error("expected EVPN VNI type")
		}
		evpnVNI.Type = p.current.Value
		p.nextToken()
		return nil
	case "bridge-domain":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected EVPN bridge-domain")
		}
		evpnVNI.BridgeDomain = p.current.Value
		p.nextToken()
		return nil
	case "vlan-id":
		if p.current.Type != TokenNumber {
			return p.error("expected EVPN VLAN ID")
		}
		vlanID, err := strconv.Atoi(p.current.Value)
		if err != nil {
			return p.error(fmt.Sprintf("invalid EVPN VLAN ID: %s", p.current.Value))
		}
		evpnVNI.VLANID = vlanID
		p.nextToken()
		return nil
	case "routing-instance":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected EVPN routing-instance")
		}
		evpnVNI.RoutingInstance = p.current.Value
		p.nextToken()
		return nil
	case "route-distinguisher":
		if p.current.Type != TokenWord {
			return p.error("expected EVPN route distinguisher")
		}
		evpnVNI.RouteDistinguisher = p.current.Value
		p.nextToken()
		return nil
	case "vrf-target":
		if p.current.Type != TokenWord {
			return p.error("expected EVPN vrf-target")
		}
		if p.current.Value == "import" || p.current.Value == "export" {
			direction := p.current.Value
			p.nextToken()
			if p.current.Type != TokenWord {
				return p.error(fmt.Sprintf("expected EVPN vrf-target %s value", direction))
			}
			switch direction {
			case "import":
				evpnVNI.VRFTargetImport = appendUniqueString(evpnVNI.VRFTargetImport, p.current.Value)
			case "export":
				evpnVNI.VRFTargetExport = appendUniqueString(evpnVNI.VRFTargetExport, p.current.Value)
			}
			p.nextToken()
			return nil
		}
		evpnVNI.VRFTarget = p.current.Value
		p.nextToken()
		return nil
	case "source-interface":
		if p.current.Type != TokenWord {
			return p.error("expected EVPN source interface")
		}
		evpnVNI.SourceInterface = p.current.Value
		p.nextToken()
		return nil
	case "source-address":
		if p.current.Type != TokenWord {
			return p.error("expected EVPN source address")
		}
		evpnVNI.SourceAddress = p.current.Value
		p.nextToken()
		return nil
	case "multicast-group":
		if p.current.Type != TokenWord {
			return p.error("expected EVPN multicast group")
		}
		evpnVNI.MulticastGroup = p.current.Value
		p.nextToken()
		return nil
	case "remote-vtep":
		if p.current.Type != TokenWord {
			return p.error("expected EVPN remote VTEP")
		}
		evpnVNI.RemoteVTEP = p.current.Value
		p.nextToken()
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported EVPN VNI parameter: %s", param))
	}
}

func (p *Parser) parseBFD(pc *ProtocolConfig) error {
	if pc.BFD == nil {
		pc.BFD = &BFDConfig{
			Profiles: make(map[string]*BFDProfile),
			Peers:    make(map[string]*BFDPeer),
		}
	}
	if p.current.Type != TokenWord {
		return p.error("expected BFD parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "profile":
		return p.parseBFDProfile(pc.BFD)
	case "peer":
		return p.parseBFDPeer(pc.BFD)
	default:
		return p.error(fmt.Sprintf("unsupported BFD parameter: %s", param))
	}
}

func (p *Parser) parseBFDProfile(bfd *BFDConfig) error {
	if p.current.Type != TokenWord && p.current.Type != TokenString {
		return p.error("expected BFD profile name")
	}
	name := p.current.Value
	p.nextToken()
	if bfd.Profiles == nil {
		bfd.Profiles = make(map[string]*BFDProfile)
	}
	if bfd.Profiles[name] == nil {
		bfd.Profiles[name] = &BFDProfile{Name: name}
	}
	profile := bfd.Profiles[name]

	if p.current.Type != TokenWord {
		return p.error("expected BFD profile parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "detect-multiplier":
		value, err := p.parseBFDNumber("detect-multiplier")
		if err != nil {
			return err
		}
		profile.DetectMultiplier = value
		return nil
	case "receive-interval":
		value, err := p.parseBFDNumber("receive-interval")
		if err != nil {
			return err
		}
		profile.ReceiveInterval = value
		return nil
	case "transmit-interval":
		value, err := p.parseBFDNumber("transmit-interval")
		if err != nil {
			return err
		}
		profile.TransmitInterval = value
		return nil
	case "echo-mode":
		profile.EchoMode = true
		return nil
	case "passive-mode":
		profile.PassiveMode = true
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported BFD profile parameter: %s", param))
	}
}

func (p *Parser) parseBFDPeer(bfd *BFDConfig) error {
	if p.current.Type != TokenWord && p.current.Type != TokenString {
		return p.error("expected BFD peer address")
	}
	address := p.current.Value
	p.nextToken()
	if bfd.Peers == nil {
		bfd.Peers = make(map[string]*BFDPeer)
	}
	if bfd.Peers[address] == nil {
		bfd.Peers[address] = &BFDPeer{Address: address}
	}
	peer := bfd.Peers[address]

	if p.current.Type != TokenWord {
		return p.error("expected BFD peer parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "local-address":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected BFD peer local-address")
		}
		peer.LocalAddress = p.current.Value
		p.nextToken()
		return nil
	case "interface":
		if p.current.Type != TokenWord {
			return p.error("expected BFD peer interface name")
		}
		peer.Interface = p.current.Value
		p.nextToken()
		return nil
	case "vrf":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected BFD peer VRF name")
		}
		peer.VRF = p.current.Value
		p.nextToken()
		return nil
	case "multihop":
		peer.Multihop = true
		return nil
	case "profile":
		if p.current.Type != TokenWord && p.current.Type != TokenString {
			return p.error("expected BFD peer profile name")
		}
		peer.Profile = p.current.Value
		p.nextToken()
		return nil
	case "detect-multiplier":
		value, err := p.parseBFDNumber("detect-multiplier")
		if err != nil {
			return err
		}
		peer.DetectMultiplier = value
		return nil
	case "receive-interval":
		value, err := p.parseBFDNumber("receive-interval")
		if err != nil {
			return err
		}
		peer.ReceiveInterval = value
		return nil
	case "transmit-interval":
		value, err := p.parseBFDNumber("transmit-interval")
		if err != nil {
			return err
		}
		peer.TransmitInterval = value
		return nil
	case "echo-mode":
		peer.EchoMode = true
		return nil
	case "passive-mode":
		peer.PassiveMode = true
		return nil
	case "shutdown":
		peer.Shutdown = true
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported BFD peer parameter: %s", param))
	}
}

func (p *Parser) parseBFDNumber(name string) (int, error) {
	if p.current.Type != TokenNumber {
		return 0, p.error(fmt.Sprintf("expected BFD %s value", name))
	}
	value, err := strconv.Atoi(p.current.Value)
	if err != nil {
		return 0, p.error(fmt.Sprintf("invalid BFD %s: %s", name, p.current.Value))
	}
	p.nextToken()
	return value, nil
}

func (p *Parser) parseMPLS(pc *ProtocolConfig) error {
	if pc.MPLS == nil {
		pc.MPLS = &MPLSConfig{}
	}
	if p.current.Type != TokenWord || p.current.Value != "interface" {
		return p.error("expected 'interface' after protocols mpls")
	}
	p.nextToken()
	if p.current.Type != TokenWord {
		return p.error("expected MPLS interface name")
	}
	pc.MPLS.Interfaces = appendUniqueString(pc.MPLS.Interfaces, p.current.Value)
	p.nextToken()
	return nil
}

func (p *Parser) parseVRRP(pc *ProtocolConfig) error {
	if pc.VRRP == nil {
		pc.VRRP = &VRRPConfig{Groups: make(map[string]*VRRPGroup)}
	}
	if p.current.Type != TokenWord || p.current.Value != "group" {
		return p.error("expected 'group' after protocols vrrp")
	}
	p.nextToken()
	if p.current.Type != TokenWord && p.current.Type != TokenNumber {
		return p.error("expected VRRP group name")
	}
	groupName := p.current.Value
	p.nextToken()
	if pc.VRRP.Groups[groupName] == nil {
		pc.VRRP.Groups[groupName] = &VRRPGroup{Name: groupName}
	}
	group := pc.VRRP.Groups[groupName]

	if p.current.Type != TokenWord {
		return p.error("expected VRRP group parameter")
	}
	param := p.current.Value
	p.nextToken()

	switch param {
	case "interface":
		if p.current.Type != TokenWord {
			return p.error("expected VRRP interface name")
		}
		group.Interface = p.current.Value
		p.nextToken()
		return nil
	case "virtual-address":
		if p.current.Type != TokenWord {
			return p.error("expected VRRP virtual address")
		}
		group.VirtualAddress = p.current.Value
		p.nextToken()
		return nil
	case "priority":
		if p.current.Type != TokenNumber {
			return p.error("expected VRRP priority")
		}
		priority, err := strconv.Atoi(p.current.Value)
		if err != nil {
			return p.error(fmt.Sprintf("invalid VRRP priority: %s", p.current.Value))
		}
		group.Priority = priority
		p.nextToken()
		return nil
	case "preempt":
		group.Preempt = true
		return nil
	default:
		return p.error(fmt.Sprintf("unsupported VRRP group parameter: %s", param))
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
	case "bfd":
		neighbor.BFD = true
		if p.current.Type == TokenWord && p.current.Value == "profile" {
			p.nextToken()
			if p.current.Type != TokenWord && p.current.Type != TokenString {
				return p.error("expected BFD profile name")
			}
			neighbor.BFDProfile = p.current.Value
			p.nextToken()
		}
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
		pc.OSPF = newOSPFConfig()
	}
	return p.parseOSPFConfig(pc.OSPF, "OSPF")
}

// parseOSPF3 parses OSPFv3 protocol configuration
func (p *Parser) parseOSPF3(pc *ProtocolConfig) error {
	if pc.OSPF3 == nil {
		pc.OSPF3 = newOSPFConfig()
	}
	return p.parseOSPFConfig(pc.OSPF3, "OSPF3")
}

func newOSPFConfig() *OSPFConfig {
	return &OSPFConfig{
		Areas: make(map[string]*OSPFArea),
	}
}

func (p *Parser) parseOSPFConfig(ospf *OSPFConfig, protocolName string) error {
	if p.current.Type != TokenWord {
		return p.error(fmt.Sprintf("expected %s parameter", protocolName))
	}

	param := p.current.Value
	p.nextToken()

	switch param {
	case "area":
		return p.parseOSPFArea(ospf)
	case "router-id":
		return p.parseOSPFRouterID(ospf)
	default:
		return p.error(fmt.Sprintf("unsupported %s parameter: %s", protocolName, param))
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
			ospfIf.PrioritySet = true
			p.nextToken()
		case "bfd":
			ospfIf.BFD = true
			if p.current.Type == TokenWord && p.current.Value == "profile" {
				p.nextToken()
				if p.current.Type != TokenWord && p.current.Type != TokenString {
					return p.error("expected BFD profile name")
				}
				ospfIf.BFDProfile = p.current.Value
				p.nextToken()
			}
		default:
			// Not an OSPF interface parameter, break the loop
			return nil
		}
	}

	return nil
}
