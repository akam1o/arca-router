package frr

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/akam1o/arca-router/pkg/config"
)

// convertPolicyOptions converts policy-options from arca-router config to FRR format.
// Returns prefix-lists, route-maps, and AS-path access-lists.
func convertPolicyOptions(cfg *config.Config) ([]PrefixList, []RouteMap, []ASPathAccessList, error) {
	if cfg == nil || cfg.PolicyOptions == nil {
		return nil, nil, nil, nil
	}

	// Convert prefix-lists (and get IPv6 split mapping)
	prefixLists, ipv6Mapping, err := convertPrefixLists(cfg.PolicyOptions.PrefixLists)
	if err != nil {
		return nil, nil, nil, err
	}

	// Convert policy-statements to route-maps and AS-path access-lists
	// Pass IPv6 mapping so route-maps can reference both IPv4 and IPv6 variants
	routeMaps, asPathLists, err := convertPolicyStatementsWithMapping(cfg.PolicyOptions.PolicyStatements, ipv6Mapping)
	if err != nil {
		return nil, nil, nil, err
	}

	return prefixLists, routeMaps, asPathLists, nil
}

// convertPrefixLists converts prefix-lists from config to FRR format.
// If a prefix-list contains both IPv4 and IPv6 prefixes, it will be split into
// two separate prefix-lists: <name> for IPv4 and <name>-v6 for IPv6.
// Returns prefix-lists and a map of original names to their IPv6 variants (if split).
func convertPrefixLists(prefixListsMap map[string]*config.PrefixList) ([]PrefixList, map[string]string, error) {
	if len(prefixListsMap) == 0 {
		return nil, nil, nil
	}

	// Sort map keys for deterministic output
	names := make([]string, 0, len(prefixListsMap))
	for name := range prefixListsMap {
		names = append(names, name)
	}
	sort.Strings(names)

	var frrPrefixLists []PrefixList
	ipv6Mapping := make(map[string]string) // original name -> IPv6 variant name

	for _, name := range names {
		pl := prefixListsMap[name]
		if pl == nil {
			continue
		}

		// Separate IPv4 and IPv6 prefixes
		var ipv4Prefixes []string
		var ipv6Prefixes []string

		for _, prefix := range pl.Prefixes {
			if isIPv6Prefix(prefix) {
				ipv6Prefixes = append(ipv6Prefixes, prefix)
			} else {
				ipv4Prefixes = append(ipv4Prefixes, prefix)
			}
		}

		// Create IPv4 prefix-list if there are IPv4 prefixes
		if len(ipv4Prefixes) > 0 {
			frrPL := PrefixList{
				Name:    name,
				IsIPv6:  false,
				Entries: make([]PrefixListEntry, 0, len(ipv4Prefixes)),
			}

			for i, prefix := range ipv4Prefixes {
				entry := PrefixListEntry{
					Seq:    (i + 1) * 10, // Sequence numbers: 10, 20, 30, ...
					Action: "permit",     // Default to permit
					Prefix: prefix,
				}
				frrPL.Entries = append(frrPL.Entries, entry)
			}

			frrPrefixLists = append(frrPrefixLists, frrPL)
		}

		// Create IPv6 prefix-list if there are IPv6 prefixes
		if len(ipv6Prefixes) > 0 {
			// Use "-v6" suffix for IPv6 variant when mixed
			ipv6Name := name
			if len(ipv4Prefixes) > 0 {
				ipv6Name = name + "-v6"
				ipv6Mapping[name] = ipv6Name
			}

			frrPL := PrefixList{
				Name:    ipv6Name,
				IsIPv6:  true,
				Entries: make([]PrefixListEntry, 0, len(ipv6Prefixes)),
			}

			for i, prefix := range ipv6Prefixes {
				entry := PrefixListEntry{
					Seq:    (i + 1) * 10, // Sequence numbers: 10, 20, 30, ...
					Action: "permit",     // Default to permit
					Prefix: prefix,
				}
				frrPL.Entries = append(frrPL.Entries, entry)
			}

			frrPrefixLists = append(frrPrefixLists, frrPL)
		}
	}

	return frrPrefixLists, ipv6Mapping, nil
}

// convertPolicyStatements converts policy-statements to FRR route-maps and AS-path access-lists.
func convertPolicyStatements(policyStatementsMap map[string]*config.PolicyStatement) ([]RouteMap, []ASPathAccessList, error) {
	return convertPolicyStatementsWithMapping(policyStatementsMap, nil)
}

// convertPolicyStatementsWithMapping converts policy-statements with IPv6 prefix-list mapping.
func convertPolicyStatementsWithMapping(policyStatementsMap map[string]*config.PolicyStatement, ipv6Mapping map[string]string) ([]RouteMap, []ASPathAccessList, error) {
	if len(policyStatementsMap) == 0 {
		return nil, nil, nil
	}

	// Sort map keys for deterministic output
	names := make([]string, 0, len(policyStatementsMap))
	for name := range policyStatementsMap {
		names = append(names, name)
	}
	sort.Strings(names)

	var frrRouteMaps []RouteMap
	var asPathLists []ASPathAccessList
	asPathCounter := 1 // Counter for generating AS-path list names

	for _, name := range names {
		ps := policyStatementsMap[name]
		if ps == nil {
			continue
		}

		frrRM := RouteMap{
			Name:    name,
			Entries: make([]RouteMapEntry, 0, len(ps.Terms)),
		}

		// Convert each term to a route-map entry
		for i, term := range ps.Terms {
			if term == nil {
				continue
			}

			entry := RouteMapEntry{
				Seq: (i + 1) * 10, // Sequence numbers: 10, 20, 30, ...
			}

			// Determine action (permit or deny based on accept/reject)
			if term.Then != nil && term.Then.Accept != nil {
				if *term.Then.Accept {
					entry.Action = "permit"
				} else {
					entry.Action = "deny"
				}
			} else {
				// Default to permit if no explicit action
				entry.Action = "permit"
			}

			// Convert match conditions
			if term.From != nil {
				if len(term.From.PrefixLists) > 0 {
					// Expand prefix-list references to include IPv6 variants if they exist
					expandedLists := make([]string, 0, len(term.From.PrefixLists)*2)
					for _, plName := range term.From.PrefixLists {
						expandedLists = append(expandedLists, plName)
						// If this prefix-list was split, also reference the IPv6 variant
						if ipv6Variant, exists := ipv6Mapping[plName]; exists {
							expandedLists = append(expandedLists, ipv6Variant)
						}
					}
					entry.MatchPrefixLists = expandedLists
				}
				if term.From.Protocol != "" {
					entry.MatchProtocol = term.From.Protocol
				}
				if term.From.Neighbor != "" {
					entry.MatchNeighbor = term.From.Neighbor
				}
				if term.From.ASPath != "" {
					// Generate AS-path access-list from regex
					asPathListName := fmt.Sprintf("AS-PATH-%d", asPathCounter)
					asPathCounter++

					asPathList := ASPathAccessList{
						Name: asPathListName,
						Entries: []ASPathAccessListEntry{
							{
								Seq:    10,
								Action: "permit",
								Regex:  term.From.ASPath,
							},
						},
					}
					asPathLists = append(asPathLists, asPathList)

					// Reference the generated AS-path list in route-map
					entry.MatchASPath = asPathListName
				}
			}

			// Convert actions
			if term.Then != nil {
				if term.Then.LocalPreference != nil {
					entry.SetLocalPreference = term.Then.LocalPreference
				}
				if term.Then.Community != "" {
					entry.SetCommunity = term.Then.Community
				}
			}

			frrRM.Entries = append(frrRM.Entries, entry)
		}

		frrRouteMaps = append(frrRouteMaps, frrRM)
	}

	return frrRouteMaps, asPathLists, nil
}

// GeneratePrefixListConfig generates FRR prefix-list configuration.
func GeneratePrefixListConfig(prefixLists []PrefixList) (string, error) {
	if len(prefixLists) == 0 {
		return "", nil
	}

	var b strings.Builder

	for _, pl := range prefixLists {
		prefix := "ip"
		if pl.IsIPv6 {
			prefix = "ipv6"
		}

		for _, entry := range pl.Entries {
			b.WriteString(fmt.Sprintf("%s prefix-list %s seq %d %s %s\n",
				prefix, pl.Name, entry.Seq, entry.Action, entry.Prefix))
		}
		b.WriteString("!\n")
	}

	return b.String(), nil
}

// GenerateRouteMapConfig generates FRR route-map configuration.
// Takes prefix-lists parameter to determine IPv4 vs IPv6 for match statements.
func GenerateRouteMapConfig(routeMaps []RouteMap, prefixLists []PrefixList) (string, error) {
	if len(routeMaps) == 0 {
		return "", nil
	}

	// Build map of prefix-list names to IPv6 flag
	plMap := make(map[string]bool)
	for _, pl := range prefixLists {
		plMap[pl.Name] = pl.IsIPv6
	}

	var b strings.Builder

	for _, rm := range routeMaps {
		for _, entry := range rm.Entries {
			b.WriteString(fmt.Sprintf("route-map %s %s %d\n", rm.Name, entry.Action, entry.Seq))

			// Match conditions
			if len(entry.MatchPrefixLists) > 0 {
				for _, plName := range entry.MatchPrefixLists {
					// Determine if this is IPv4 or IPv6 prefix-list
					ipVersion := "ip"
					if isIPv6, found := plMap[plName]; found && isIPv6 {
						ipVersion = "ipv6"
					}
					b.WriteString(fmt.Sprintf(" match %s address prefix-list %s\n", ipVersion, plName))
				}
			}

			if entry.MatchProtocol != "" {
				b.WriteString(fmt.Sprintf(" match source-protocol %s\n", entry.MatchProtocol))
			}

			if entry.MatchNeighbor != "" {
				b.WriteString(fmt.Sprintf(" match peer %s\n", entry.MatchNeighbor))
			}

			if entry.MatchASPath != "" {
				b.WriteString(fmt.Sprintf(" match as-path %s\n", entry.MatchASPath))
			}

			// Set actions
			if entry.SetLocalPreference != nil {
				b.WriteString(fmt.Sprintf(" set local-preference %d\n", *entry.SetLocalPreference))
			}

			if entry.SetCommunity != "" {
				b.WriteString(fmt.Sprintf(" set community %s\n", entry.SetCommunity))
			}

			b.WriteString("!\n")
		}
	}

	return b.String(), nil
}

// GenerateASPathAccessListConfig generates FRR AS-path access-list configuration.
func GenerateASPathAccessListConfig(asPathLists []ASPathAccessList) (string, error) {
	if len(asPathLists) == 0 {
		return "", nil
	}

	var b strings.Builder

	for _, apl := range asPathLists {
		for _, entry := range apl.Entries {
			b.WriteString(fmt.Sprintf("bgp as-path access-list %s seq %d %s %s\n",
				apl.Name, entry.Seq, entry.Action, entry.Regex))
		}
		b.WriteString("!\n")
	}

	return b.String(), nil
}

// isIPv6Prefix checks if a prefix is IPv6.
func isIPv6Prefix(prefix string) bool {
	// Parse CIDR
	ip, _, err := net.ParseCIDR(prefix)
	if err != nil {
		return false
	}
	return ip.To4() == nil
}
