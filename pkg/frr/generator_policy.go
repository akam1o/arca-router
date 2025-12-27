package frr

import (
	"fmt"
	"net"
	"strings"

	"github.com/akam1o/arca-router/pkg/config"
)

// convertPolicyOptions converts policy-options from arca-router config to FRR format.
// Returns prefix-lists and route-maps.
func convertPolicyOptions(cfg *config.Config) ([]PrefixList, []RouteMap, error) {
	if cfg == nil || cfg.PolicyOptions == nil {
		return nil, nil, nil
	}

	// Convert prefix-lists
	prefixLists, err := convertPrefixLists(cfg.PolicyOptions.PrefixLists)
	if err != nil {
		return nil, nil, err
	}

	// Convert policy-statements to route-maps
	routeMaps, err := convertPolicyStatements(cfg.PolicyOptions.PolicyStatements)
	if err != nil {
		return nil, nil, err
	}

	return prefixLists, routeMaps, nil
}

// convertPrefixLists converts prefix-lists from config to FRR format.
func convertPrefixLists(prefixListsMap map[string]*config.PrefixList) ([]PrefixList, error) {
	if len(prefixListsMap) == 0 {
		return nil, nil
	}

	var frrPrefixLists []PrefixList

	for name, pl := range prefixListsMap {
		if pl == nil {
			continue
		}

		frrPL := PrefixList{
			Name:    name,
			IsIPv6:  false,
			Entries: make([]PrefixListEntry, 0, len(pl.Prefixes)),
		}

		// Check if any prefix is IPv6
		hasIPv6 := false
		for _, prefix := range pl.Prefixes {
			if isIPv6Prefix(prefix) {
				hasIPv6 = true
				break
			}
		}
		frrPL.IsIPv6 = hasIPv6

		// Convert each prefix to an entry
		for i, prefix := range pl.Prefixes {
			entry := PrefixListEntry{
				Seq:    (i + 1) * 10, // Sequence numbers: 10, 20, 30, ...
				Action: "permit",     // Default to permit
				Prefix: prefix,
			}
			frrPL.Entries = append(frrPL.Entries, entry)
		}

		frrPrefixLists = append(frrPrefixLists, frrPL)
	}

	return frrPrefixLists, nil
}

// convertPolicyStatements converts policy-statements to FRR route-maps.
func convertPolicyStatements(policyStatementsMap map[string]*config.PolicyStatement) ([]RouteMap, error) {
	if len(policyStatementsMap) == 0 {
		return nil, nil
	}

	var frrRouteMaps []RouteMap

	for name, ps := range policyStatementsMap {
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
					entry.MatchPrefixLists = term.From.PrefixLists
				}
				if term.From.Protocol != "" {
					entry.MatchProtocol = term.From.Protocol
				}
				if term.From.Neighbor != "" {
					entry.MatchNeighbor = term.From.Neighbor
				}
				if term.From.ASPath != "" {
					entry.MatchASPath = term.From.ASPath
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

	return frrRouteMaps, nil
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
func GenerateRouteMapConfig(routeMaps []RouteMap) (string, error) {
	if len(routeMaps) == 0 {
		return "", nil
	}

	var b strings.Builder

	for _, rm := range routeMaps {
		for _, entry := range rm.Entries {
			b.WriteString(fmt.Sprintf("route-map %s %s %d\n", rm.Name, entry.Action, entry.Seq))

			// Match conditions
			if len(entry.MatchPrefixLists) > 0 {
				for _, plName := range entry.MatchPrefixLists {
					b.WriteString(fmt.Sprintf(" match ip address prefix-list %s\n", plName))
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

// isIPv6Prefix checks if a prefix is IPv6.
func isIPv6Prefix(prefix string) bool {
	// Parse CIDR
	ip, _, err := net.ParseCIDR(prefix)
	if err != nil {
		return false
	}
	return ip.To4() == nil
}
