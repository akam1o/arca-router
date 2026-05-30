package main

import (
	"fmt"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
	configcli "github.com/akam1o/arca-router/pkg/cli"
	"github.com/chzyer/readline"
)

func routeTextOptions(args []string) (protocol, addressFamily string, err error) {
	addressFamily = routeAddressFamilyIPv4
	if len(args) > 0 && isRouteAddressFamily(args[0]) {
		addressFamily = args[0]
		args = args[1:]
	}
	if len(args) == 0 {
		return "", addressFamily, nil
	}
	if args[0] != "protocol" {
		return "", "", fmt.Errorf("'show route' accepts '[inet|inet6] protocol <proto>' or no arguments")
	}
	if len(args) < 2 {
		return "", "", fmt.Errorf("'protocol' requires a protocol name")
	}
	if len(args) > 2 {
		return "", "", fmt.Errorf("'show route protocol' does not accept extra arguments")
	}
	protocol = args[1]
	if !validRouteProtocol(protocol, addressFamily) {
		return "", "", fmt.Errorf("invalid protocol '%s' for %s. Valid: %s", protocol, addressFamily, validRouteProtocolList(addressFamily))
	}
	return protocol, addressFamily, nil
}

func routeStateOptions(args []string) (prefix, protocol string, err error) {
	for len(args) > 0 {
		switch args[0] {
		case "prefix":
			if prefix != "" {
				return "", "", fmt.Errorf("'show routes' accepts prefix only once")
			}
			if len(args) < 2 {
				return "", "", fmt.Errorf("'show routes prefix' requires a CIDR prefix")
			}
			prefix = args[1]
			args = args[2:]
		case "protocol":
			if protocol != "" {
				return "", "", fmt.Errorf("'show routes' accepts protocol only once")
			}
			if len(args) < 2 {
				return "", "", fmt.Errorf("'show routes protocol' requires a protocol name")
			}
			protocol = args[1]
			if !validRouteStateProtocol(protocol) {
				return "", "", fmt.Errorf("invalid protocol '%s'. Valid: %s", protocol, validRouteStateProtocolList())
			}
			args = args[2:]
		default:
			return "", "", fmt.Errorf("'show routes' accepts '[prefix <cidr>] [protocol <proto>]'")
		}
	}
	return prefix, protocol, nil
}

func bfdTextOptions(args []string) (peerAddress string, brief bool, counters bool, err error) {
	if len(args) == 0 {
		return "", false, false, nil
	}
	switch args[0] {
	case "brief":
		if len(args) > 1 {
			return "", false, false, fmt.Errorf("'show bfd brief' does not accept extra arguments")
		}
		return "", true, false, nil
	case "counters":
		if len(args) > 1 {
			return "", false, false, fmt.Errorf("'show bfd counters' does not accept extra arguments")
		}
		return "", false, true, nil
	case "peer":
		if len(args) < 2 {
			return "", false, false, fmt.Errorf("'show bfd peer' requires an IP address")
		}
		if len(args) > 3 {
			return "", false, false, fmt.Errorf("'show bfd peer' accepts only an optional counters argument")
		}
		if len(args) == 3 && args[2] != "counters" {
			return "", false, false, fmt.Errorf("'show bfd peer' accepts only an optional counters argument")
		}
		return args[1], false, len(args) == 3, nil
	case "status":
		return "", false, false, fmt.Errorf("'show bfd status' is handled as structured operational state")
	default:
		return "", false, false, fmt.Errorf("'show bfd' accepts status, brief, counters, peer <ip>, or no arguments")
	}
}

func bfdStatusRequested(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "status" {
		return false, nil
	}
	if len(args) > 1 {
		return false, fmt.Errorf("'show bfd status' does not accept extra arguments")
	}
	return true, nil
}

func routingInstancesNameFilter(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) > 1 {
		return "", fmt.Errorf("'show routing-instances' accepts at most one instance name")
	}
	return args[0], nil
}

func filterRoutingInstances(instances []grpcclient.RoutingInstanceInfo, name string) []grpcclient.RoutingInstanceInfo {
	if name == "" {
		return instances
	}
	filtered := make([]grpcclient.RoutingInstanceInfo, 0, 1)
	for _, instance := range instances {
		if instance.Name == name {
			filtered = append(filtered, instance)
		}
	}
	return filtered
}

const (
	routeAddressFamilyIPv4 = "inet"
	routeAddressFamilyIPv6 = "inet6"
)

var validIPv4RouteProtocols = map[string]bool{
	"bgp":       true,
	"ospf":      true,
	"static":    true,
	"connected": true,
	"kernel":    true,
}

var validIPv6RouteProtocols = map[string]bool{
	"bgp":       true,
	"ospf3":     true,
	"ospf6":     true,
	"static":    true,
	"connected": true,
	"kernel":    true,
}

func isRouteAddressFamily(value string) bool {
	return value == routeAddressFamilyIPv4 || value == routeAddressFamilyIPv6
}

func validRouteProtocol(protocol, addressFamily string) bool {
	if addressFamily == routeAddressFamilyIPv6 {
		return validIPv6RouteProtocols[protocol]
	}
	return validIPv4RouteProtocols[protocol]
}

func validRouteProtocolList(addressFamily string) string {
	if addressFamily == routeAddressFamilyIPv6 {
		return "bgp, ospf3, ospf6, static, connected, kernel"
	}
	return "bgp, ospf, static, connected, kernel"
}

func validRouteStateProtocol(protocol string) bool {
	return validIPv4RouteProtocols[protocol] || validIPv6RouteProtocols[protocol]
}

func validRouteStateProtocolList() string {
	return "bgp, ospf, ospf3, ospf6, static, connected, kernel"
}

// --- Utilities ---

func hasPipeOutsideQuotes(line string) bool {
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case '|':
			if !inQuote {
				return true
			}
		}
	}
	return false
}

func tokenize(line string) []string {
	tokens, err := configcli.TokenizeCommand(line)
	if err != nil {
		return nil
	}
	return tokens
}

func filterInput(r rune) (rune, bool) {
	if r == readline.CharCtrlZ {
		return r, false
	}
	return r, true
}

func createCompleter() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("help"),
		readline.PcItem("configure"),
		readline.PcItem("exit"),
		readline.PcItem("quit"),
		readline.PcItem("show",
			readline.PcItem("configuration",
				readline.PcItem("rollback"),
			),
			readline.PcItem("compatibility"),
			readline.PcItem("interfaces"),
			readline.PcItem("bgp",
				readline.PcItem("summary"),
				readline.PcItem("neighbor"),
			),
			readline.PcItem("ospf",
				readline.PcItem("neighbor"),
			),
			readline.PcItem("vrrp"),
			readline.PcItem("lcp"),
			readline.PcItem("ha"),
			readline.PcItem("class-of-service"),
			readline.PcItem("evpn"),
			readline.PcItem("telemetry",
				readline.PcItem("path"),
				readline.PcItem("interval"),
				readline.PcItem("count"),
				readline.PcItem("once"),
			),
			readline.PcItem("route",
				readline.PcItem("protocol"),
			),
			readline.PcItem("compare"),
			readline.PcItem("history"),
		),
		readline.PcItem("set",
			readline.PcItem("system",
				readline.PcItem("host-name"),
			),
			readline.PcItem("interfaces"),
			readline.PcItem("routing-options",
				readline.PcItem("autonomous-system"),
				readline.PcItem("router-id"),
				readline.PcItem("static",
					readline.PcItem("route"),
				),
			),
			readline.PcItem("protocols",
				readline.PcItem("bgp",
					readline.PcItem("group"),
				),
				readline.PcItem("ospf",
					readline.PcItem("router-id"),
					readline.PcItem("area"),
				),
			),
		),
		readline.PcItem("delete",
			readline.PcItem("system"),
			readline.PcItem("interfaces"),
			readline.PcItem("routing-options"),
			readline.PcItem("protocols"),
		),
		readline.PcItem("commit",
			readline.PcItem("check"),
			readline.PcItem("and-quit"),
			readline.PcItem("comment"),
		),
		readline.PcItem("check",
			readline.PcItem("upgrade",
				readline.PcItem("backup"),
			),
		),
		readline.PcItem("backup",
			readline.PcItem("configuration",
				readline.PcItem("rollback"),
			),
		),
		readline.PcItem("restore",
			readline.PcItem("configuration",
				readline.PcItem("rollback"),
			),
		),
		readline.PcItem("rollback"),
		readline.PcItem("discard-changes"),
		readline.PcItem("compare"),
		readline.PcItem("edit",
			readline.PcItem("interfaces"),
			readline.PcItem("protocols"),
			readline.PcItem("routing-options"),
		),
		readline.PcItem("up"),
		readline.PcItem("top"),
	)
}
