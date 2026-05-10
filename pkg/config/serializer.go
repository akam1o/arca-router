package config

import (
	"fmt"
	"sort"
	"strings"
)

// EscapeValue escapes a scalar value for safe use in set-command text.
func EscapeValue(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\"'\\") {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\t", "\\t")
		s = strings.ReplaceAll(s, "\"", "\\\"")
		return `"` + s + `"`
	}
	return s
}

// ToSetCommands serializes Config into deterministic Junos-style set commands.
func ToSetCommands(cfg *Config) string {
	if cfg == nil {
		return ""
	}

	var b strings.Builder

	if cfg.System != nil && cfg.System.HostName != "" {
		writeLine(&b, "set system host-name %s", EscapeValue(cfg.System.HostName))
	}

	writeInterfaces(&b, cfg.Interfaces)
	writeRoutingOptions(&b, cfg.RoutingOptions)
	writeProtocols(&b, cfg.Protocols)
	writePolicyOptions(&b, cfg.PolicyOptions)
	writeSecurity(&b, cfg.Security)

	return b.String()
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
		if route.Distance > 0 {
			writeLine(b, "set routing-options static route %s next-hop %s distance %d",
				route.Prefix, route.NextHop, route.Distance)
		} else {
			writeLine(b, "set routing-options static route %s next-hop %s",
				route.Prefix, route.NextHop)
		}
	}
}

func writeProtocols(b *strings.Builder, pc *ProtocolConfig) {
	if pc == nil {
		return
	}
	writeBGP(b, pc.BGP)
	writeOSPF(b, pc.OSPF)
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
		}
	}
}

func writeOSPF(b *strings.Builder, ospf *OSPFConfig) {
	if ospf == nil {
		return
	}
	if ospf.RouterID != "" {
		writeLine(b, "set protocols ospf router-id %s", ospf.RouterID)
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
			line := fmt.Sprintf("set protocols ospf area %s interface %s", areaName, ifaceName)
			if ospfIface.Passive {
				line += " passive"
			}
			if ospfIface.Metric > 0 {
				line += fmt.Sprintf(" metric %d", ospfIface.Metric)
			}
			if ospfIface.Priority > 0 {
				line += fmt.Sprintf(" priority %d", ospfIface.Priority)
			}
			writeLine(b, "%s", line)
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

func writeSecurity(b *strings.Builder, sec *SecurityConfig) {
	if sec == nil {
		return
	}
	if sec.NETCONF != nil && sec.NETCONF.SSH != nil && sec.NETCONF.SSH.Port != 0 {
		writeLine(b, "set security netconf ssh port %d", sec.NETCONF.SSH.Port)
	}
	for _, username := range sortedKeys(sec.Users) {
		user := sec.Users[username]
		if user == nil {
			continue
		}
		if user.Password != "" {
			writeLine(b, "set security users user %s password %s", username, EscapeValue(user.Password))
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
}
