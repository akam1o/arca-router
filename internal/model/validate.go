package model

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/akam1o/arca-router/pkg/security"
)

// junosIfacePattern matches the legacy config parser's supported Junos-style
// interface names.
var junosIfacePattern = regexp.MustCompile(`^([a-z]{2}-\d+/\d+/\d+|ae\d+|lo\d+|irb|fxp\d+)$`)

// Validate checks the RouterConfig for semantic correctness.
func (c *RouterConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("configuration is nil")
	}

	if err := c.validateSystem(); err != nil {
		return err
	}
	if err := c.validateInterfaces(); err != nil {
		return err
	}
	if err := c.validateChassis(); err != nil {
		return err
	}
	if err := c.validateRouting(); err != nil {
		return err
	}
	if err := c.validateRoutingInstances(); err != nil {
		return err
	}
	if err := c.validateProtocols(); err != nil {
		return err
	}
	if err := c.validatePolicy(); err != nil {
		return err
	}
	if err := c.validateClassOfService(); err != nil {
		return err
	}
	if err := c.validateSecurity(); err != nil {
		return err
	}
	return nil
}

func (c *RouterConfig) validateSystem() error {
	if c.System == nil || c.System.Services == nil {
		return nil
	}
	if web := c.System.Services.WebUI; web != nil {
		if web.Port < 0 || web.Port > 65535 {
			return fmt.Errorf("system services web-ui: port must be 0-65535, got %d", web.Port)
		}
		if web.ListenAddress != "" && web.ListenAddress != "localhost" && net.ParseIP(web.ListenAddress) == nil {
			return fmt.Errorf("system services web-ui: invalid listen-address %q", web.ListenAddress)
		}
	}
	if prometheus := c.System.Services.Prometheus; prometheus != nil {
		if prometheus.Port < 0 || prometheus.Port > 65535 {
			return fmt.Errorf("system services prometheus: port must be 0-65535, got %d", prometheus.Port)
		}
		if prometheus.ListenAddress != "" && prometheus.ListenAddress != "localhost" && net.ParseIP(prometheus.ListenAddress) == nil {
			return fmt.Errorf("system services prometheus: invalid listen-address %q", prometheus.ListenAddress)
		}
	}
	if snmp := c.System.Services.SNMP; snmp != nil {
		if snmp.Enabled {
			if err := security.ValidateSNMPCommunity(snmp.Community); err != nil {
				return fmt.Errorf("system services snmp: %w", err)
			}
		}
		if snmp.Port < 0 || snmp.Port > 65535 {
			return fmt.Errorf("system services snmp: port must be 0-65535, got %d", snmp.Port)
		}
		if snmp.ListenAddress != "" && snmp.ListenAddress != "localhost" && net.ParseIP(snmp.ListenAddress) == nil {
			return fmt.Errorf("system services snmp: invalid listen-address %q", snmp.ListenAddress)
		}
	}
	return nil
}

func (c *RouterConfig) validateInterfaces() error {
	for name, iface := range c.Interfaces {
		if !junosIfacePattern.MatchString(name) {
			return fmt.Errorf("invalid interface name %q: must be Junos format (e.g. ge-0/0/0, ae0, lo0, irb, fxp0)", name)
		}
		if iface == nil {
			return fmt.Errorf("interface %s is nil", name)
		}
		for unitNum, unit := range iface.Units {
			if unitNum < 0 {
				return fmt.Errorf("interface %s: unit number must be non-negative, got %d", name, unitNum)
			}
			if unit == nil {
				return fmt.Errorf("interface %s unit %d is nil", name, unitNum)
			}
			for familyName, family := range unit.Family {
				if familyName != "inet" && familyName != "inet6" {
					return fmt.Errorf("interface %s unit %d: unsupported family %q", name, unitNum, familyName)
				}
				if family == nil {
					return fmt.Errorf("interface %s unit %d family %s is nil", name, unitNum, familyName)
				}
				for _, addr := range family.Addresses {
					if _, _, err := net.ParseCIDR(addr); err != nil {
						return fmt.Errorf("interface %s unit %d family %s: invalid address %q: %w",
							name, unitNum, familyName, addr, err)
					}
				}
			}
		}
	}
	return nil
}

func (c *RouterConfig) validateRouting() error {
	if c.Routing == nil {
		return nil
	}
	if c.Routing.RouterID != "" {
		if net.ParseIP(c.Routing.RouterID) == nil {
			return fmt.Errorf("routing-options: invalid router-id %q", c.Routing.RouterID)
		}
	}
	for _, route := range c.Routing.StaticRoutes {
		if route == nil {
			return fmt.Errorf("static route entry is nil")
		}
		_, prefixNet, err := net.ParseCIDR(route.Prefix)
		if err != nil {
			return fmt.Errorf("static route: invalid prefix %q: %w", route.Prefix, err)
		}
		nextHopIP := net.ParseIP(route.NextHop)
		if nextHopIP == nil {
			return fmt.Errorf("static route %s: invalid next-hop %q", route.Prefix, route.NextHop)
		}
		if (prefixNet.IP.To4() == nil) != (nextHopIP.To4() == nil) {
			return fmt.Errorf("static route %s: next-hop family does not match prefix", route.Prefix)
		}
		if route.BFDProfile != "" {
			if err := c.validateBFDProfileReference(fmt.Sprintf("static route %s", route.Prefix), route.BFDProfile); err != nil {
				return err
			}
		}
		if route.BFDSource != "" {
			sourceIP := net.ParseIP(route.BFDSource)
			if sourceIP == nil {
				return fmt.Errorf("static route %s: invalid BFD source %q", route.Prefix, route.BFDSource)
			}
			if (nextHopIP.To4() == nil) != (sourceIP.To4() == nil) {
				return fmt.Errorf("static route %s: BFD source family does not match next-hop", route.Prefix)
			}
		}
		if (route.BFDProfile != "" || route.BFDSource != "" || route.BFDMultihop) && !route.BFD {
			return fmt.Errorf("static route %s: BFD options require BFD to be enabled", route.Prefix)
		}
		if route.Distance > 0 && (route.BFD || route.BFDProfile != "" || route.BFDSource != "" || route.BFDMultihop) {
			return fmt.Errorf("static route %s: distance is not supported with BFD monitoring", route.Prefix)
		}
	}
	return nil
}

func (c *RouterConfig) validateChassis() error {
	if c.Chassis == nil || c.Chassis.Cluster == nil {
		return nil
	}
	for name, node := range c.Chassis.Cluster.Nodes {
		if node == nil {
			return fmt.Errorf("chassis cluster node %s is nil", name)
		}
		if node.Address != "" && net.ParseIP(node.Address) == nil {
			return fmt.Errorf("chassis cluster node %s: invalid address %q", name, node.Address)
		}
		if node.Priority < 0 || node.Priority > 255 {
			return fmt.Errorf("chassis cluster node %s: priority must be 0-255, got %d", name, node.Priority)
		}
	}
	if sync := c.Chassis.Cluster.Sync; sync != nil && sync.Etcd != nil {
		for _, endpoint := range sync.Etcd.Endpoints {
			if strings.TrimSpace(endpoint) == "" {
				return fmt.Errorf("chassis cluster sync etcd endpoint must not be empty")
			}
		}
	}
	return nil
}

func (c *RouterConfig) validateRoutingInstances() error {
	for name, instance := range c.RoutingInstances {
		if instance == nil {
			return fmt.Errorf("routing-instance %s is nil", name)
		}
		if instance.InstanceType != "" && instance.InstanceType != "vrf" {
			return fmt.Errorf("routing-instance %s: unsupported instance-type %q", name, instance.InstanceType)
		}
		if instance.RouteDistinguisher != "" && !regexp.MustCompile(`^\d+:\d+$`).MatchString(instance.RouteDistinguisher) {
			return fmt.Errorf("routing-instance %s: invalid route-distinguisher %q", name, instance.RouteDistinguisher)
		}
		importTargetCount := 0
		exportTargetCount := 0
		if instance.VRFTarget != "" {
			if err := validateVRFTargetValue(fmt.Sprintf("routing-instance %s vrf-target", name), instance.VRFTarget); err != nil {
				return err
			}
			importTargetCount++
			exportTargetCount++
		}
		for _, ifName := range instance.Interfaces {
			if err := c.validateInterfaceReference(fmt.Sprintf("routing-instance %s", name), ifName); err != nil {
				return err
			}
		}
		for _, target := range instance.VRFTargetImport {
			if err := validateVRFTargetValue(fmt.Sprintf("routing-instance %s vrf-target import", name), target); err != nil {
				return err
			}
			importTargetCount++
		}
		for _, target := range instance.VRFTargetExport {
			if err := validateVRFTargetValue(fmt.Sprintf("routing-instance %s vrf-target export", name), target); err != nil {
				return err
			}
			exportTargetCount++
		}
		for _, policyName := range instance.VRFImport {
			if err := c.validatePolicyStatementReference(fmt.Sprintf("routing-instance %s vrf-import", name), policyName); err != nil {
				return err
			}
		}
		for _, policyName := range instance.VRFExport {
			if err := c.validatePolicyStatementReference(fmt.Sprintf("routing-instance %s vrf-export", name), policyName); err != nil {
				return err
			}
		}
		if len(instance.VRFImport) > 0 && importTargetCount == 0 {
			return fmt.Errorf("routing-instance %s: vrf-import requires an import vrf-target", name)
		}
		if len(instance.VRFExport) > 0 && exportTargetCount == 0 {
			return fmt.Errorf("routing-instance %s: vrf-export requires an export vrf-target", name)
		}
		if exportTargetCount > 0 && instance.RouteDistinguisher == "" {
			return fmt.Errorf("routing-instance %s: route-distinguisher is required for VPN export", name)
		}
		if (importTargetCount > 0 || exportTargetCount > 0 || len(instance.VRFImport) > 0 || len(instance.VRFExport) > 0) &&
			(c.Routing == nil || c.Routing.AutonomousSystem == 0) {
			return fmt.Errorf("routing-instance %s: routing-options autonomous-system is required for VPN import/export", name)
		}
	}
	return nil
}

func validateVRFTargetValue(context, target string) error {
	if !regexp.MustCompile(`^target:\d+:\d+$`).MatchString(target) {
		return fmt.Errorf("%s: invalid vrf-target %q", context, target)
	}
	return nil
}

func (c *RouterConfig) validateProtocols() error {
	if c.Protocols == nil {
		return nil
	}
	if bfd := c.Protocols.BFD; bfd != nil {
		if err := c.validateBFD(bfd); err != nil {
			return err
		}
	}
	if bgp := c.Protocols.BGP; bgp != nil {
		if err := c.validateBGP(bgp); err != nil {
			return err
		}
	}
	if evpn := c.Protocols.EVPN; evpn != nil {
		if err := c.validateEVPN(evpn); err != nil {
			return err
		}
	}
	if ospf := c.Protocols.OSPF; ospf != nil {
		if err := c.validateOSPF("ospf", ospf); err != nil {
			return err
		}
	}
	if ospf3 := c.Protocols.OSPF3; ospf3 != nil {
		if err := c.validateOSPF("ospf3", ospf3); err != nil {
			return err
		}
	}
	if mpls := c.Protocols.MPLS; mpls != nil {
		for _, ifName := range mpls.Interfaces {
			if err := c.validateInterfaceReference("mpls", ifName); err != nil {
				return err
			}
		}
	}
	if vrrp := c.Protocols.VRRP; vrrp != nil {
		for name, group := range vrrp.Groups {
			if group == nil {
				return fmt.Errorf("vrrp group %s is nil", name)
			}
			id, err := strconv.Atoi(name)
			if err != nil || id < 1 || id > 255 {
				return fmt.Errorf("vrrp group %s: id must be numeric 1-255", name)
			}
			if group.Interface != "" {
				if err := c.validateInterfaceReference(fmt.Sprintf("vrrp group %s", name), group.Interface); err != nil {
					return err
				}
			}
			if group.VirtualAddress != "" && net.ParseIP(group.VirtualAddress) == nil {
				return fmt.Errorf("vrrp group %s: invalid virtual-address %q", name, group.VirtualAddress)
			}
			if group.Priority < 0 || group.Priority > 254 {
				return fmt.Errorf("vrrp group %s: priority must be 1-254 when configured, got %d", name, group.Priority)
			}
		}
	}
	return nil
}

func (c *RouterConfig) validateEVPN(evpn *EVPNConfig) error {
	for id, vni := range evpn.VNIs {
		if err := c.validateEVPNVNI(id, vni); err != nil {
			return err
		}
	}
	return nil
}

func (c *RouterConfig) validateEVPNVNI(id int, vni *EVPNVNI) error {
	context := fmt.Sprintf("evpn vni %d", id)
	if vni == nil {
		return fmt.Errorf("%s is nil", context)
	}
	if id < 1 || id > 16777215 {
		return fmt.Errorf("%s: VNI must be 1-16777215", context)
	}
	if vni.VNI != 0 && vni.VNI != id {
		return fmt.Errorf("%s: VNI value %d does not match map key", context, vni.VNI)
	}
	if vni.Type != "l2" && vni.Type != "l3" {
		return fmt.Errorf("%s: type must be l2 or l3", context)
	}
	switch vni.Type {
	case "l2":
		if strings.TrimSpace(vni.BridgeDomain) == "" {
			return fmt.Errorf("%s: bridge-domain is required for L2 VNI", context)
		}
		if vni.RoutingInstance != "" {
			return fmt.Errorf("%s: routing-instance is only valid for L3 VNI", context)
		}
	case "l3":
		if strings.TrimSpace(vni.RoutingInstance) == "" {
			return fmt.Errorf("%s: routing-instance is required for L3 VNI", context)
		}
		if vni.BridgeDomain != "" {
			return fmt.Errorf("%s: bridge-domain is only valid for L2 VNI", context)
		}
		if vni.VLANID != 0 {
			return fmt.Errorf("%s: vlan-id is only valid for L2 VNI", context)
		}
		if _, ok := c.RoutingInstances[vni.RoutingInstance]; !ok {
			return fmt.Errorf("%s: routing-instance %q is not configured", context, vni.RoutingInstance)
		}
	}
	if vni.VLANID != 0 && (vni.VLANID < 1 || vni.VLANID > 4094) {
		return fmt.Errorf("%s: vlan-id must be 1-4094, got %d", context, vni.VLANID)
	}
	if vni.RouteDistinguisher != "" && !regexp.MustCompile(`^\d+:\d+$`).MatchString(vni.RouteDistinguisher) {
		return fmt.Errorf("%s: invalid route-distinguisher %q", context, vni.RouteDistinguisher)
	}
	if vni.VRFTarget != "" {
		if err := validateVRFTargetValue(fmt.Sprintf("%s vrf-target", context), vni.VRFTarget); err != nil {
			return err
		}
	}
	for _, target := range vni.VRFTargetImport {
		if err := validateVRFTargetValue(fmt.Sprintf("%s vrf-target import", context), target); err != nil {
			return err
		}
	}
	for _, target := range vni.VRFTargetExport {
		if err := validateVRFTargetValue(fmt.Sprintf("%s vrf-target export", context), target); err != nil {
			return err
		}
	}
	if vni.SourceInterface != "" {
		if err := c.validateInterfaceReference(context, vni.SourceInterface); err != nil {
			return err
		}
	}
	if vni.SourceAddress != "" && net.ParseIP(vni.SourceAddress) == nil {
		return fmt.Errorf("%s: invalid source-address %q", context, vni.SourceAddress)
	}
	if vni.MulticastGroup != "" {
		groupIP := net.ParseIP(vni.MulticastGroup)
		if groupIP == nil || !groupIP.IsMulticast() {
			return fmt.Errorf("%s: invalid multicast-group %q", context, vni.MulticastGroup)
		}
	}
	if vni.RemoteVTEP != "" {
		remoteIP := net.ParseIP(vni.RemoteVTEP)
		if remoteIP == nil || remoteIP.IsMulticast() {
			return fmt.Errorf("%s: invalid remote-vtep %q", context, vni.RemoteVTEP)
		}
	}
	if vni.MulticastGroup != "" && vni.RemoteVTEP != "" {
		return fmt.Errorf("%s: multicast-group and remote-vtep are mutually exclusive", context)
	}
	return nil
}

func (c *RouterConfig) validateBFD(bfd *BFDConfig) error {
	for name, profile := range bfd.Profiles {
		if profile == nil {
			return fmt.Errorf("bfd profile %s is nil", name)
		}
		if err := validateModelBFDTimers(fmt.Sprintf("bfd profile %s", name), profile.DetectMultiplier, profile.ReceiveInterval, profile.TransmitInterval); err != nil {
			return err
		}
	}
	for address, peer := range bfd.Peers {
		if peer == nil {
			return fmt.Errorf("bfd peer %s is nil", address)
		}
		if net.ParseIP(address) == nil {
			return fmt.Errorf("bfd peer %s: invalid peer address", address)
		}
		if peer.LocalAddress != "" && net.ParseIP(peer.LocalAddress) == nil {
			return fmt.Errorf("bfd peer %s: invalid local-address %q", address, peer.LocalAddress)
		}
		if peer.Interface != "" {
			if err := c.validateInterfaceReference(fmt.Sprintf("bfd peer %s", address), peer.Interface); err != nil {
				return err
			}
		}
		if peer.VRF != "" && peer.VRF != "default" {
			if c.RoutingInstances == nil || c.RoutingInstances[peer.VRF] == nil {
				return fmt.Errorf("bfd peer %s: routing-instance %q is not configured", address, peer.VRF)
			}
		}
		if peer.Profile != "" && bfd.Profiles[peer.Profile] == nil {
			return fmt.Errorf("bfd peer %s: profile %q is not configured", address, peer.Profile)
		}
		if peer.Multihop && peer.EchoMode {
			return fmt.Errorf("bfd peer %s: echo-mode is not supported with multihop", address)
		}
		if peer.Multihop && peer.Profile != "" && bfd.Profiles[peer.Profile] != nil && bfd.Profiles[peer.Profile].EchoMode {
			return fmt.Errorf("bfd peer %s: echo-mode profile %q is not supported with multihop", address, peer.Profile)
		}
		if err := validateModelBFDTimers(fmt.Sprintf("bfd peer %s", address), peer.DetectMultiplier, peer.ReceiveInterval, peer.TransmitInterval); err != nil {
			return err
		}
	}
	return nil
}

func validateModelBFDTimers(context string, detectMultiplier, receiveInterval, transmitInterval int) error {
	if detectMultiplier < 0 || detectMultiplier > 255 || detectMultiplier == 1 {
		return fmt.Errorf("%s: detect-multiplier must be omitted or 2-255, got %d", context, detectMultiplier)
	}
	if receiveInterval < 0 || receiveInterval > 60000 || (receiveInterval > 0 && receiveInterval < 10) {
		return fmt.Errorf("%s: receive-interval must be omitted or 10-60000, got %d", context, receiveInterval)
	}
	if transmitInterval < 0 || transmitInterval > 60000 || (transmitInterval > 0 && transmitInterval < 10) {
		return fmt.Errorf("%s: transmit-interval must be omitted or 10-60000, got %d", context, transmitInterval)
	}
	return nil
}

func (c *RouterConfig) validateOSPF(protocol string, ospf *OSPFConfig) error {
	if ospf.RouterID != "" {
		if net.ParseIP(ospf.RouterID) == nil {
			return fmt.Errorf("%s: invalid router-id %q", protocol, ospf.RouterID)
		}
	}
	for areaName, area := range ospf.Areas {
		if area == nil {
			return fmt.Errorf("%s area %s is nil", protocol, areaName)
		}
		for ifName := range area.Interfaces {
			if err := c.validateInterfaceReference(fmt.Sprintf("%s area %s", protocol, areaName), ifName); err != nil {
				return err
			}
			if area.Interfaces[ifName] != nil && area.Interfaces[ifName].BFDProfile != "" {
				if err := c.validateBFDProfileReference(fmt.Sprintf("%s area %s interface %s", protocol, areaName, ifName), area.Interfaces[ifName].BFDProfile); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *RouterConfig) validateBGP(bgp *BGPConfig) error {
	// BGP requires AS number from routing-options
	if c.Routing == nil || c.Routing.AutonomousSystem == 0 {
		return fmt.Errorf("bgp: routing-options autonomous-system is required")
	}
	for groupName, group := range bgp.Groups {
		if group == nil {
			return fmt.Errorf("bgp group %s is nil", groupName)
		}
		if group.Type != "" && group.Type != "internal" && group.Type != "external" {
			return fmt.Errorf("bgp group %s: type must be 'internal' or 'external', got %q",
				groupName, group.Type)
		}
		for ip, neighbor := range group.Neighbors {
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("bgp group %s: invalid neighbor IP %q", groupName, ip)
			}
			if neighbor == nil {
				return fmt.Errorf("bgp group %s neighbor %s is nil", groupName, ip)
			}
			if neighbor.PeerAS == 0 {
				return fmt.Errorf("bgp group %s neighbor %s: peer-as is required", groupName, ip)
			}
			if neighbor.BFDProfile != "" {
				if err := c.validateBFDProfileReference(fmt.Sprintf("bgp group %s neighbor %s", groupName, ip), neighbor.BFDProfile); err != nil {
					return err
				}
			}
		}
		if group.Import != "" {
			if err := c.validatePolicyStatementReference(fmt.Sprintf("bgp group %s import", groupName), group.Import); err != nil {
				return err
			}
		}
		if group.Export != "" {
			if err := c.validatePolicyStatementReference(fmt.Sprintf("bgp group %s export", groupName), group.Export); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *RouterConfig) validateBFDProfileReference(context, profileName string) error {
	if strings.TrimSpace(profileName) == "" {
		return fmt.Errorf("%s: empty BFD profile reference", context)
	}
	if c.Protocols == nil || c.Protocols.BFD == nil || c.Protocols.BFD.Profiles == nil {
		return fmt.Errorf("%s: BFD profile %q not found in protocols bfd", context, profileName)
	}
	if c.Protocols.BFD.Profiles[profileName] == nil {
		return fmt.Errorf("%s: BFD profile %q not found in protocols bfd", context, profileName)
	}
	return nil
}

func (c *RouterConfig) validatePolicy() error {
	if c.Policy == nil {
		return nil
	}
	for name, pl := range c.Policy.PrefixLists {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("policy-options prefix-list name is empty")
		}
		if pl == nil {
			return fmt.Errorf("policy-options prefix-list %s is nil", name)
		}
		for _, prefix := range pl.Prefixes {
			if _, _, err := net.ParseCIDR(prefix); err != nil {
				return fmt.Errorf("prefix-list %s: invalid prefix %q: %w", name, prefix, err)
			}
		}
	}
	for name, statement := range c.Policy.PolicyStatements {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("policy-options policy-statement name is empty")
		}
		if statement == nil {
			return fmt.Errorf("policy-options policy-statement %s is nil", name)
		}
		if err := c.validatePolicyStatement(name, statement); err != nil {
			return err
		}
	}
	return nil
}

func (c *RouterConfig) validatePolicyStatement(name string, statement *PolicyStatement) error {
	for _, term := range statement.Terms {
		if term == nil {
			return fmt.Errorf("policy-statement %s: nil term", name)
		}
		if strings.TrimSpace(term.Name) == "" {
			return fmt.Errorf("policy-statement %s: empty term name", name)
		}
		if term.From != nil {
			for _, listName := range term.From.PrefixLists {
				if strings.TrimSpace(listName) == "" {
					return fmt.Errorf("policy-statement %s term %s: empty prefix-list reference", name, term.Name)
				}
				if c.Policy.PrefixLists == nil || c.Policy.PrefixLists[listName] == nil {
					return fmt.Errorf("policy-statement %s term %s: prefix-list %q not found in policy-options", name, term.Name, listName)
				}
			}
			if term.From.Protocol != "" && !isValidRoutePolicyProtocol(term.From.Protocol) {
				return fmt.Errorf("policy-statement %s term %s: invalid protocol %q", name, term.Name, term.From.Protocol)
			}
			if term.From.Neighbor != "" && net.ParseIP(term.From.Neighbor) == nil {
				return fmt.Errorf("policy-statement %s term %s: invalid neighbor %q", name, term.Name, term.From.Neighbor)
			}
			if term.From.ASPath != "" {
				if _, err := regexp.Compile(term.From.ASPath); err != nil {
					return fmt.Errorf("policy-statement %s term %s: invalid as-path %q: %w", name, term.Name, term.From.ASPath, err)
				}
			}
		}
		if term.Then != nil && term.Then.Community != "" && !isValidPolicyCommunity(term.Then.Community) {
			return fmt.Errorf("policy-statement %s term %s: invalid community %q", name, term.Name, term.Then.Community)
		}
	}
	return nil
}

func isValidRoutePolicyProtocol(protocol string) bool {
	switch protocol {
	case "bgp", "ospf", "ospf3", "static", "connected", "direct", "kernel", "rip":
		return true
	default:
		return false
	}
}

func isValidPolicyCommunity(community string) bool {
	switch community {
	case "no-export", "no-advertise", "local-AS", "no-peer":
		return true
	default:
		parts := strings.Split(community, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return false
		}
		asn, err := strconv.ParseUint(parts[0], 10, 32)
		if err != nil || asn > 65535 {
			return false
		}
		value, err := strconv.ParseUint(parts[1], 10, 32)
		return err == nil && value <= 65535
	}
}

func (c *RouterConfig) validateClassOfService() error {
	if c.ClassOfService == nil {
		return nil
	}
	for name, fc := range c.ClassOfService.ForwardingClasses {
		if fc == nil {
			return fmt.Errorf("class-of-service forwarding-class %s is nil", name)
		}
		if fc.Queue < 0 || fc.Queue > 7 {
			return fmt.Errorf("class-of-service forwarding-class %s: queue must be 0-7, got %d", name, fc.Queue)
		}
	}
	for name, iface := range c.ClassOfService.Interfaces {
		if err := c.validateInterfaceReference("class-of-service", name); err != nil {
			return err
		}
		if iface == nil {
			return fmt.Errorf("class-of-service interface %s is nil", name)
		}
		if iface.OutputTrafficControlProfile != "" {
			if _, ok := c.ClassOfService.TrafficControlProfiles[iface.OutputTrafficControlProfile]; !ok {
				return fmt.Errorf("class-of-service interface %s: output traffic-control-profile %q not found", name, iface.OutputTrafficControlProfile)
			}
		}
	}
	return nil
}

func (c *RouterConfig) validateInterfaceReference(context, ifName string) error {
	if !junosIfacePattern.MatchString(ifName) {
		return fmt.Errorf("%s: invalid interface name %q", context, ifName)
	}
	if _, ok := c.Interfaces[ifName]; !ok {
		return fmt.Errorf("%s: interface %q is not configured", context, ifName)
	}
	return nil
}

func (c *RouterConfig) validatePolicyStatementReference(context, policyName string) error {
	if strings.TrimSpace(policyName) == "" {
		return fmt.Errorf("%s: empty policy-statement reference", context)
	}
	if c.Policy == nil {
		return fmt.Errorf("%s: policy-statement %q not found in policy-options", context, policyName)
	}
	if _, ok := c.Policy.PolicyStatements[policyName]; !ok {
		return fmt.Errorf("%s: policy-statement %q not found in policy-options", context, policyName)
	}
	return nil
}

func (c *RouterConfig) validateSecurity() error {
	if c.Security == nil || c.Security.NETCONF == nil || c.Security.NETCONF.SSH == nil {
		return nil
	}
	ssh := c.Security.NETCONF.SSH
	if ssh.ListenAddress != "" && ssh.ListenAddress != "localhost" && net.ParseIP(ssh.ListenAddress) == nil {
		return fmt.Errorf("security netconf ssh: invalid listen-address %q", ssh.ListenAddress)
	}
	port := ssh.Port
	if port < 0 || port > 65535 {
		return fmt.Errorf("security netconf ssh port must be 0-65535, got %d", port)
	}
	return nil
}

// ResolveRouterID returns the effective router-id for a given protocol,
// applying the Junos-style fallback: protocol-specific → global routing-options.
func (c *RouterConfig) ResolveRouterID(protocol string) string {
	switch strings.ToLower(protocol) {
	case "ospf":
		if c.Protocols != nil && c.Protocols.OSPF != nil && c.Protocols.OSPF.RouterID != "" {
			return c.Protocols.OSPF.RouterID
		}
	case "ospf3":
		if c.Protocols != nil && c.Protocols.OSPF3 != nil && c.Protocols.OSPF3.RouterID != "" {
			return c.Protocols.OSPF3.RouterID
		}
	}
	if c.Routing != nil && c.Routing.RouterID != "" {
		return c.Routing.RouterID
	}
	return ""
}
