package netconf

import (
	"fmt"
	"strings"

	"github.com/akam1o/arca-router/pkg/config"
)

// escapeValue escapes values for safe use in set commands
func escapeValue(s string) string {
	// Quote values containing spaces or special characters
	if strings.ContainsAny(s, " \t\"'") {
		// Escape internal quotes
		s = strings.ReplaceAll(s, "\"", "\\\"")
		return "\"" + s + "\""
	}
	return s
}

// ConfigToText converts config.Config to text format (set commands)
// This implements Phase 2 Step 4: Datastore Bridge Layer
func ConfigToText(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", nil
	}

	var buf strings.Builder

	// System configuration
	if cfg.System != nil && cfg.System.HostName != "" {
		buf.WriteString(fmt.Sprintf("set system host-name %s\n", escapeValue(cfg.System.HostName)))
	}

	// Interfaces configuration
	for ifaceName, iface := range cfg.Interfaces {
		if iface.Description != "" {
			buf.WriteString(fmt.Sprintf("set interfaces %s description %s\n", ifaceName, escapeValue(iface.Description)))
		}

		for unitNum, unit := range iface.Units {
			for familyName, family := range unit.Family {
				for _, addr := range family.Addresses {
					buf.WriteString(fmt.Sprintf("set interfaces %s unit %d family %s address %s\n",
						ifaceName, unitNum, familyName, addr))
				}
			}
		}
	}

	// Routing options
	if cfg.RoutingOptions != nil {
		if cfg.RoutingOptions.RouterID != "" {
			buf.WriteString(fmt.Sprintf("set routing-options router-id %s\n", cfg.RoutingOptions.RouterID))
		}
		if cfg.RoutingOptions.AutonomousSystem != 0 {
			buf.WriteString(fmt.Sprintf("set routing-options autonomous-system %d\n", cfg.RoutingOptions.AutonomousSystem))
		}
		for _, route := range cfg.RoutingOptions.StaticRoutes {
			if route.Distance > 0 {
				buf.WriteString(fmt.Sprintf("set routing-options static route %s next-hop %s distance %d\n",
					route.Prefix, route.NextHop, route.Distance))
			} else {
				buf.WriteString(fmt.Sprintf("set routing-options static route %s next-hop %s\n",
					route.Prefix, route.NextHop))
			}
		}
	}

	// Protocols - BGP
	if cfg.Protocols != nil && cfg.Protocols.BGP != nil {
		for groupName, group := range cfg.Protocols.BGP.Groups {
			if group.Type != "" {
				buf.WriteString(fmt.Sprintf("set protocols bgp group %s type %s\n", groupName, group.Type))
			}
			if group.Import != "" {
				buf.WriteString(fmt.Sprintf("set protocols bgp group %s import %s\n", groupName, group.Import))
			}
			if group.Export != "" {
				buf.WriteString(fmt.Sprintf("set protocols bgp group %s export %s\n", groupName, group.Export))
			}
			for _, neighbor := range group.Neighbors {
				buf.WriteString(fmt.Sprintf("set protocols bgp group %s neighbor %s peer-as %d\n",
					groupName, neighbor.IP, neighbor.PeerAS))
				if neighbor.Description != "" {
					buf.WriteString(fmt.Sprintf("set protocols bgp group %s neighbor %s description %s\n",
						groupName, neighbor.IP, escapeValue(neighbor.Description)))
				}
				if neighbor.LocalAddress != "" {
					buf.WriteString(fmt.Sprintf("set protocols bgp group %s neighbor %s local-address %s\n",
						groupName, neighbor.IP, neighbor.LocalAddress))
				}
			}
		}
	}

	// Protocols - OSPF
	if cfg.Protocols != nil && cfg.Protocols.OSPF != nil {
		if cfg.Protocols.OSPF.RouterID != "" {
			buf.WriteString(fmt.Sprintf("set protocols ospf router-id %s\n", cfg.Protocols.OSPF.RouterID))
		}
		for areaName, area := range cfg.Protocols.OSPF.Areas {
			buf.WriteString(fmt.Sprintf("set protocols ospf area %s area-id %s\n", areaName, area.AreaID))
			for _, ospfIface := range area.Interfaces {
				buf.WriteString(fmt.Sprintf("set protocols ospf area %s interface %s\n", areaName, ospfIface.Name))
				if ospfIface.Passive {
					buf.WriteString(fmt.Sprintf("set protocols ospf area %s interface %s passive\n",
						areaName, ospfIface.Name))
				}
				if ospfIface.Metric > 0 {
					buf.WriteString(fmt.Sprintf("set protocols ospf area %s interface %s metric %d\n",
						areaName, ospfIface.Name, ospfIface.Metric))
				}
				if ospfIface.Priority > 0 {
					buf.WriteString(fmt.Sprintf("set protocols ospf area %s interface %s priority %d\n",
						areaName, ospfIface.Name, ospfIface.Priority))
				}
			}
		}
	}

	return buf.String(), nil
}

// TextToConfig converts text format (set commands) to config.Config
// This implements Phase 2 Step 4: Datastore Bridge Layer
func TextToConfig(text string) (*config.Config, error) {
	if text == "" {
		return config.NewConfig(), nil
	}

	// Use existing parser
	reader := strings.NewReader(text)
	parser := config.NewParser(reader)
	cfg, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse config text: %w", err)
	}

	return cfg, nil
}
