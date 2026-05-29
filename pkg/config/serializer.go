package config

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const redactedSecretValue = "<redacted>"

// ContainsRedactedSecretValue reports whether serialized config text contains
// the reserved marker used in place of credential material.
func ContainsRedactedSecretValue(text string) bool {
	return strings.Contains(text, redactedSecretValue)
}

type serializeOptions struct {
	RedactSecrets bool
}

// EscapeValue escapes a scalar value for safe use in set-command text.
func EscapeValue(s string) string {
	if s == "" || needsQuotedValue(s) {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\t", "\\t")
		s = strings.ReplaceAll(s, "\"", "\\\"")
		return `"` + s + `"`
	}
	return s
}

func needsQuotedValue(s string) bool {
	for _, ch := range s {
		if unicode.IsSpace(ch) || ch == '"' || ch == '\'' || ch == '\\' || !isWordChar(ch) {
			return true
		}
	}
	return false
}

// ToSetCommands serializes Config into deterministic Junos-style set commands.
// It panics if sensitive value protection fails; callers that can return errors
// should use ToSetCommandsWithError.
func ToSetCommands(cfg *Config) string {
	text, err := ToSetCommandsWithError(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to serialize config: %v", err))
	}
	return text
}

// ToSetCommandsWithError serializes Config into deterministic Junos-style set
// commands and reports sensitive value protection failures.
func ToSetCommandsWithError(cfg *Config) (string, error) {
	return toSetCommandsWithError(cfg, serializeOptions{})
}

// ToSetCommandsRedactedWithError serializes Config into deterministic set
// commands with credential material replaced by a non-round-trippable marker.
func ToSetCommandsRedactedWithError(cfg *Config) (string, error) {
	return toSetCommandsWithError(cfg, serializeOptions{RedactSecrets: true})
}

func toSetCommandsWithError(cfg *Config, opts serializeOptions) (string, error) {
	if cfg == nil {
		return "", nil
	}

	var b strings.Builder

	if cfg.System != nil && cfg.System.HostName != "" {
		writeLine(&b, "set system host-name %s", EscapeValue(cfg.System.HostName))
	}
	writeSystemServices(&b, cfg.System, opts)

	writeChassis(&b, cfg.Chassis)
	writeInterfaces(&b, cfg.Interfaces)
	writeRoutingOptions(&b, cfg.RoutingOptions)
	writeRoutingInstances(&b, cfg.RoutingInstances)
	writeProtocols(&b, cfg.Protocols)
	writePolicyOptions(&b, cfg.PolicyOptions)
	writeClassOfService(&b, cfg.ClassOfService)
	if err := writeSecurity(&b, cfg.Security, opts); err != nil {
		return "", err
	}

	return b.String(), nil
}

func writeSystemServices(b *strings.Builder, system *SystemConfig, opts serializeOptions) {
	if system == nil || system.Services == nil {
		return
	}
	if web := system.Services.WebUI; web != nil {
		if web.Enabled {
			writeLine(b, "set system services web-ui enabled true")
		}
		if web.ListenAddress != "" {
			writeLine(b, "set system services web-ui listen-address %s", EscapeValue(web.ListenAddress))
		}
		if web.Port != 0 {
			writeLine(b, "set system services web-ui port %d", web.Port)
		}
	}
	if prometheus := system.Services.Prometheus; prometheus != nil {
		if prometheus.Enabled {
			writeLine(b, "set system services prometheus enabled true")
		}
		if prometheus.ListenAddress != "" {
			writeLine(b, "set system services prometheus listen-address %s", EscapeValue(prometheus.ListenAddress))
		}
		if prometheus.Port != 0 {
			writeLine(b, "set system services prometheus port %d", prometheus.Port)
		}
	}
	if snmp := system.Services.SNMP; snmp != nil {
		if snmp.Enabled {
			writeLine(b, "set system services snmp enabled true")
		}
		if snmp.ListenAddress != "" {
			writeLine(b, "set system services snmp listen-address %s", EscapeValue(snmp.ListenAddress))
		}
		if snmp.Port != 0 {
			writeLine(b, "set system services snmp port %d", snmp.Port)
		}
		if snmp.Community != "" {
			community := snmp.Community
			if opts.RedactSecrets {
				community = redactedSecretValue
			}
			writeLine(b, "set system services snmp community %s", EscapeValue(community))
		}
	}
}

func writeChassis(b *strings.Builder, chassis *ChassisConfig) {
	if chassis == nil || chassis.Cluster == nil {
		return
	}
	cluster := chassis.Cluster
	if cluster.Enabled {
		writeLine(b, "set chassis cluster enabled true")
	}
	for _, name := range sortedKeys(cluster.Nodes) {
		node := cluster.Nodes[name]
		if node == nil {
			continue
		}
		if node.Address != "" {
			writeLine(b, "set chassis cluster node %s address %s", name, node.Address)
		}
		if node.Priority != 0 {
			writeLine(b, "set chassis cluster node %s priority %d", name, node.Priority)
		}
	}
	if cluster.Sync != nil && cluster.Sync.Etcd != nil {
		endpoints := append([]string(nil), cluster.Sync.Etcd.Endpoints...)
		sort.Strings(endpoints)
		for _, endpoint := range endpoints {
			writeLine(b, "set chassis cluster sync etcd endpoint %s", EscapeValue(endpoint))
		}
	}
}

func writeLine(b *strings.Builder, format string, args ...interface{}) {
	_, _ = fmt.Fprintf(b, format+"\n", args...)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedInts[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func writeInterfaces(b *strings.Builder, interfaces map[string]*Interface) {
	for _, name := range sortedKeys(interfaces) {
		iface := interfaces[name]
		if iface == nil {
			continue
		}
		if iface.Description != "" {
			writeLine(b, "set interfaces %s description %s", name, EscapeValue(iface.Description))
		}
		for _, unitNum := range sortedInts(iface.Units) {
			unit := iface.Units[unitNum]
			if unit == nil {
				continue
			}
			for _, familyName := range sortedKeys(unit.Family) {
				family := unit.Family[familyName]
				if family == nil {
					continue
				}
				addresses := append([]string(nil), family.Addresses...)
				sort.Strings(addresses)
				for _, addr := range addresses {
					writeLine(b, "set interfaces %s unit %d family %s address %s",
						name, unitNum, familyName, addr)
				}
			}
		}
	}
}

func writeRoutingOptions(b *strings.Builder, ro *RoutingOptions) {
	if ro == nil {
		return
	}
	if ro.RouterID != "" {
		writeLine(b, "set routing-options router-id %s", ro.RouterID)
	}
	if ro.AutonomousSystem != 0 {
		writeLine(b, "set routing-options autonomous-system %d", ro.AutonomousSystem)
	}

	routes := append([]*StaticRoute(nil), ro.StaticRoutes...)
	sort.Slice(routes, func(i, j int) bool {
		if routes[i] == nil || routes[j] == nil {
			return routes[j] != nil
		}
		if routes[i].Prefix != routes[j].Prefix {
			return routes[i].Prefix < routes[j].Prefix
		}
		if routes[i].NextHop != routes[j].NextHop {
			return routes[i].NextHop < routes[j].NextHop
		}
		return routes[i].Distance < routes[j].Distance
	})
	for _, route := range routes {
		if route == nil {
			continue
		}
		line := fmt.Sprintf("set routing-options static route %s next-hop %s", route.Prefix, route.NextHop)
		if route.Distance > 0 {
			line += fmt.Sprintf(" distance %d", route.Distance)
		}
		if route.BFD || route.BFDProfile != "" || route.BFDSource != "" || route.BFDMultihop {
			line += " bfd"
			if route.BFDMultihop {
				line += " multi-hop"
			}
			if route.BFDSource != "" {
				line += fmt.Sprintf(" source %s", route.BFDSource)
			}
			if route.BFDProfile != "" {
				line += fmt.Sprintf(" profile %s", EscapeValue(route.BFDProfile))
			}
		}
		writeLine(b, "%s", line)
	}
}

func writeRoutingInstances(b *strings.Builder, instances map[string]*RoutingInstance) {
	for _, name := range sortedKeys(instances) {
		instance := instances[name]
		if instance == nil {
			continue
		}
		if instance.InstanceType != "" {
			writeLine(b, "set routing-instances %s instance-type %s", name, instance.InstanceType)
		}
		if instance.RouteDistinguisher != "" {
			writeLine(b, "set routing-instances %s route-distinguisher %s", name, instance.RouteDistinguisher)
		}
		if instance.VRFTarget != "" {
			writeLine(b, "set routing-instances %s vrf-target %s", name, instance.VRFTarget)
		}
		for _, target := range instance.VRFTargetImport {
			writeLine(b, "set routing-instances %s vrf-target import %s", name, target)
		}
		for _, target := range instance.VRFTargetExport {
			writeLine(b, "set routing-instances %s vrf-target export %s", name, target)
		}
		for _, policy := range instance.VRFImport {
			writeLine(b, "set routing-instances %s vrf-import %s", name, EscapeValue(policy))
		}
		for _, policy := range instance.VRFExport {
			writeLine(b, "set routing-instances %s vrf-export %s", name, EscapeValue(policy))
		}
		interfaces := append([]string(nil), instance.Interfaces...)
		sort.Strings(interfaces)
		for _, iface := range interfaces {
			writeLine(b, "set routing-instances %s interface %s", name, iface)
		}
	}
}

func writeProtocols(b *strings.Builder, pc *ProtocolConfig) {
	if pc == nil {
		return
	}
	writeBFD(b, pc.BFD)
	writeBGP(b, pc.BGP)
	writeEVPN(b, pc.EVPN)
	writeOSPF(b, "ospf", pc.OSPF)
	writeOSPF(b, "ospf3", pc.OSPF3)
	writeMPLS(b, pc.MPLS)
	writeVRRP(b, pc.VRRP)
}

func writeBFD(b *strings.Builder, bfd *BFDConfig) {
	if bfd == nil {
		return
	}
	for _, name := range sortedKeys(bfd.Profiles) {
		profile := bfd.Profiles[name]
		if profile == nil {
			continue
		}
		if profile.ReceiveInterval != 0 {
			writeLine(b, "set protocols bfd profile %s receive-interval %d", EscapeValue(name), profile.ReceiveInterval)
		}
		if profile.TransmitInterval != 0 {
			writeLine(b, "set protocols bfd profile %s transmit-interval %d", EscapeValue(name), profile.TransmitInterval)
		}
		if profile.DetectMultiplier != 0 {
			writeLine(b, "set protocols bfd profile %s detect-multiplier %d", EscapeValue(name), profile.DetectMultiplier)
		}
		if profile.EchoMode {
			writeLine(b, "set protocols bfd profile %s echo-mode", EscapeValue(name))
		}
		if profile.PassiveMode {
			writeLine(b, "set protocols bfd profile %s passive-mode", EscapeValue(name))
		}
	}
	for _, address := range sortedKeys(bfd.Peers) {
		peer := bfd.Peers[address]
		if peer == nil {
			continue
		}
		base := fmt.Sprintf("set protocols bfd peer %s", EscapeValue(address))
		if peer.LocalAddress != "" {
			writeLine(b, "%s local-address %s", base, EscapeValue(peer.LocalAddress))
		}
		if peer.Interface != "" {
			writeLine(b, "%s interface %s", base, peer.Interface)
		}
		if peer.VRF != "" {
			writeLine(b, "%s vrf %s", base, EscapeValue(peer.VRF))
		}
		if peer.Multihop {
			writeLine(b, "%s multihop", base)
		}
		if peer.Profile != "" {
			writeLine(b, "%s profile %s", base, EscapeValue(peer.Profile))
		}
		if peer.ReceiveInterval != 0 {
			writeLine(b, "%s receive-interval %d", base, peer.ReceiveInterval)
		}
		if peer.TransmitInterval != 0 {
			writeLine(b, "%s transmit-interval %d", base, peer.TransmitInterval)
		}
		if peer.DetectMultiplier != 0 {
			writeLine(b, "%s detect-multiplier %d", base, peer.DetectMultiplier)
		}
		if peer.EchoMode {
			writeLine(b, "%s echo-mode", base)
		}
		if peer.PassiveMode {
			writeLine(b, "%s passive-mode", base)
		}
		if peer.Shutdown {
			writeLine(b, "%s shutdown", base)
		}
	}
}

func writeEVPN(b *strings.Builder, evpn *EVPNConfig) {
	if evpn == nil {
		return
	}
	for _, vni := range sortedInts(evpn.VNIs) {
		entry := evpn.VNIs[vni]
		if entry == nil {
			continue
		}
		base := fmt.Sprintf("set protocols evpn vni %d", vni)
		if entry.Type != "" {
			writeLine(b, "%s type %s", base, entry.Type)
		}
		if entry.BridgeDomain != "" {
			writeLine(b, "%s bridge-domain %s", base, EscapeValue(entry.BridgeDomain))
		}
		if entry.VLANID != 0 {
			writeLine(b, "%s vlan-id %d", base, entry.VLANID)
		}
		if entry.RoutingInstance != "" {
			writeLine(b, "%s routing-instance %s", base, EscapeValue(entry.RoutingInstance))
		}
		if entry.RouteDistinguisher != "" {
			writeLine(b, "%s route-distinguisher %s", base, entry.RouteDistinguisher)
		}
		if entry.VRFTarget != "" {
			writeLine(b, "%s vrf-target %s", base, entry.VRFTarget)
		}
		for _, target := range sortedStrings(entry.VRFTargetImport) {
			writeLine(b, "%s vrf-target import %s", base, target)
		}
		for _, target := range sortedStrings(entry.VRFTargetExport) {
			writeLine(b, "%s vrf-target export %s", base, target)
		}
		if entry.SourceInterface != "" {
			writeLine(b, "%s source-interface %s", base, entry.SourceInterface)
		}
		if entry.SourceAddress != "" {
			writeLine(b, "%s source-address %s", base, entry.SourceAddress)
		}
		if entry.MulticastGroup != "" {
			writeLine(b, "%s multicast-group %s", base, entry.MulticastGroup)
		}
		if entry.RemoteVTEP != "" {
			writeLine(b, "%s remote-vtep %s", base, entry.RemoteVTEP)
		}
	}
}

func sortedStrings(values []string) []string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return sorted
}

func writeMPLS(b *strings.Builder, mpls *MPLSConfig) {
	if mpls == nil {
		return
	}
	interfaces := append([]string(nil), mpls.Interfaces...)
	sort.Strings(interfaces)
	for _, iface := range interfaces {
		writeLine(b, "set protocols mpls interface %s", iface)
	}
}

func writeVRRP(b *strings.Builder, vrrp *VRRPConfig) {
	if vrrp == nil {
		return
	}
	for _, groupName := range sortedKeys(vrrp.Groups) {
		group := vrrp.Groups[groupName]
		if group == nil {
			continue
		}
		if group.Interface != "" {
			writeLine(b, "set protocols vrrp group %s interface %s", groupName, group.Interface)
		}
		if group.VirtualAddress != "" {
			writeLine(b, "set protocols vrrp group %s virtual-address %s", groupName, group.VirtualAddress)
		}
		if group.Priority != 0 {
			writeLine(b, "set protocols vrrp group %s priority %d", groupName, group.Priority)
		}
		if group.Preempt {
			writeLine(b, "set protocols vrrp group %s preempt", groupName)
		}
	}
}

func writeBGP(b *strings.Builder, bgp *BGPConfig) {
	if bgp == nil {
		return
	}
	for _, groupName := range sortedKeys(bgp.Groups) {
		group := bgp.Groups[groupName]
		if group == nil {
			continue
		}
		if group.Type != "" {
			writeLine(b, "set protocols bgp group %s type %s", groupName, group.Type)
		}
		if group.Import != "" {
			writeLine(b, "set protocols bgp group %s import %s", groupName, group.Import)
		}
		if group.Export != "" {
			writeLine(b, "set protocols bgp group %s export %s", groupName, group.Export)
		}
		for _, neighborIP := range sortedKeys(group.Neighbors) {
			neighbor := group.Neighbors[neighborIP]
			if neighbor == nil {
				continue
			}
			if neighbor.PeerAS != 0 {
				writeLine(b, "set protocols bgp group %s neighbor %s peer-as %d",
					groupName, neighborIP, neighbor.PeerAS)
			}
			if neighbor.Description != "" {
				writeLine(b, "set protocols bgp group %s neighbor %s description %s",
					groupName, neighborIP, EscapeValue(neighbor.Description))
			}
			if neighbor.LocalAddress != "" {
				writeLine(b, "set protocols bgp group %s neighbor %s local-address %s",
					groupName, neighborIP, neighbor.LocalAddress)
			}
			if neighbor.BFDProfile != "" {
				writeLine(b, "set protocols bgp group %s neighbor %s bfd profile %s",
					groupName, neighborIP, EscapeValue(neighbor.BFDProfile))
			} else if neighbor.BFD {
				writeLine(b, "set protocols bgp group %s neighbor %s bfd",
					groupName, neighborIP)
			}
		}
	}
}

func writeOSPF(b *strings.Builder, protocol string, ospf *OSPFConfig) {
	if ospf == nil {
		return
	}
	if ospf.RouterID != "" {
		writeLine(b, "set protocols %s router-id %s", protocol, ospf.RouterID)
	}
	for _, areaName := range sortedKeys(ospf.Areas) {
		area := ospf.Areas[areaName]
		if area == nil {
			continue
		}
		for _, ifaceName := range sortedKeys(area.Interfaces) {
			ospfIface := area.Interfaces[ifaceName]
			if ospfIface == nil {
				continue
			}
			base := fmt.Sprintf("set protocols %s area %s interface %s", protocol, areaName, ifaceName)
			wrote := false
			if ospfIface.Passive {
				writeLine(b, "%s passive", base)
				wrote = true
			}
			if ospfIface.Metric > 0 {
				writeLine(b, "%s metric %d", base, ospfIface.Metric)
				wrote = true
			}
			if ospfIface.PrioritySet || ospfIface.Priority > 0 {
				writeLine(b, "%s priority %d", base, ospfIface.Priority)
				wrote = true
			}
			if ospfIface.BFDProfile != "" {
				writeLine(b, "%s bfd profile %s", base, EscapeValue(ospfIface.BFDProfile))
				wrote = true
			} else if ospfIface.BFD {
				writeLine(b, "%s bfd", base)
				wrote = true
			}
			if !wrote {
				writeLine(b, "%s", base)
			}
		}
	}
}

func writePolicyOptions(b *strings.Builder, po *PolicyOptions) {
	if po == nil {
		return
	}
	for _, listName := range sortedKeys(po.PrefixLists) {
		list := po.PrefixLists[listName]
		if list == nil {
			continue
		}
		prefixes := append([]string(nil), list.Prefixes...)
		sort.Strings(prefixes)
		for _, prefix := range prefixes {
			writeLine(b, "set policy-options prefix-list %s %s", listName, prefix)
		}
	}
	for _, policyName := range sortedKeys(po.PolicyStatements) {
		policy := po.PolicyStatements[policyName]
		if policy == nil {
			continue
		}
		for _, term := range policy.Terms {
			if term == nil || term.Name == "" {
				continue
			}
			writePolicyTerm(b, policyName, term)
		}
	}
}

func writePolicyTerm(b *strings.Builder, policyName string, term *PolicyTerm) {
	base := fmt.Sprintf("set policy-options policy-statement %s term %s", policyName, term.Name)
	if term.From != nil {
		prefixLists := append([]string(nil), term.From.PrefixLists...)
		sort.Strings(prefixLists)
		for _, listName := range prefixLists {
			writeLine(b, "%s from prefix-list %s", base, listName)
		}
		if term.From.Protocol != "" {
			writeLine(b, "%s from protocol %s", base, term.From.Protocol)
		}
		if term.From.Neighbor != "" {
			writeLine(b, "%s from neighbor %s", base, term.From.Neighbor)
		}
		if term.From.ASPath != "" {
			writeLine(b, "%s from as-path %s", base, EscapeValue(term.From.ASPath))
		}
	}
	if term.Then != nil {
		if term.Then.Accept != nil {
			if *term.Then.Accept {
				writeLine(b, "%s then accept", base)
			} else {
				writeLine(b, "%s then reject", base)
			}
		}
		if term.Then.LocalPreference != nil {
			writeLine(b, "%s then local-preference %d", base, *term.Then.LocalPreference)
		}
		if term.Then.Community != "" {
			writeLine(b, "%s then community %s", base, EscapeValue(term.Then.Community))
		}
	}
}

func writeClassOfService(b *strings.Builder, cos *ClassOfServiceConfig) {
	if cos == nil {
		return
	}
	for _, name := range sortedKeys(cos.ForwardingClasses) {
		fc := cos.ForwardingClasses[name]
		if fc == nil {
			continue
		}
		writeLine(b, "set class-of-service forwarding-class %s queue %d", name, fc.Queue)
	}
	for _, name := range sortedKeys(cos.TrafficControlProfiles) {
		profile := cos.TrafficControlProfiles[name]
		if profile == nil {
			continue
		}
		if profile.ShapingRate != 0 {
			writeLine(b, "set class-of-service traffic-control-profile %s shaping-rate %d", name, profile.ShapingRate)
		}
		if profile.SchedulerMap != "" {
			writeLine(b, "set class-of-service traffic-control-profile %s scheduler-map %s", name, profile.SchedulerMap)
		}
	}
	for _, name := range sortedKeys(cos.Interfaces) {
		iface := cos.Interfaces[name]
		if iface == nil || iface.OutputTrafficControlProfile == "" {
			continue
		}
		writeLine(b, "set class-of-service interfaces %s output-traffic-control-profile %s",
			name, iface.OutputTrafficControlProfile)
	}
}

func writeSecurity(b *strings.Builder, sec *SecurityConfig, opts serializeOptions) error {
	if sec == nil {
		return nil
	}
	if sec.NETCONF != nil && sec.NETCONF.SSH != nil {
		ssh := sec.NETCONF.SSH
		if ssh.Enabled {
			writeLine(b, "set security netconf ssh enabled true")
		}
		if ssh.ListenAddress != "" {
			writeLine(b, "set security netconf ssh listen-address %s", EscapeValue(ssh.ListenAddress))
		}
		if ssh.Port != 0 {
			writeLine(b, "set security netconf ssh port %d", ssh.Port)
		}
	}
	for _, username := range sortedKeys(sec.Users) {
		user := sec.Users[username]
		if user == nil {
			continue
		}
		if user.Password != "" {
			password := redactedSecretValue
			if !opts.RedactSecrets {
				var err error
				password, err = NormalizePasswordForStorage(user.Password)
				if err != nil {
					return fmt.Errorf("protect password for user %s: %w", username, err)
				}
			}
			writeLine(b, "set security users user %s password %s", username, EscapeValue(password))
		}
		if user.Role != "" {
			writeLine(b, "set security users user %s role %s", username, user.Role)
		}
		if user.SSHKey != "" {
			writeLine(b, "set security users user %s ssh-key %s", username, EscapeValue(user.SSHKey))
		}
	}
	if sec.RateLimit != nil {
		if sec.RateLimit.PerIP != 0 {
			writeLine(b, "set security rate-limit per-ip %d", sec.RateLimit.PerIP)
		}
		if sec.RateLimit.PerUser != 0 {
			writeLine(b, "set security rate-limit per-user %d", sec.RateLimit.PerUser)
		}
	}
	return nil
}
