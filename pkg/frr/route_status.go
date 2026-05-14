package frr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// RouteStatusReader reads FRR's operational route state.
type RouteStatusReader interface {
	ReadRouteStatus(ctx context.Context) (*RouteStatus, error)
}

// RouteStatus is the parsed output of FRR's route JSON commands.
type RouteStatus struct {
	Routes []RouteStatusEntry
}

// RouteStatusEntry describes one route entry or nexthop path.
type RouteStatusEntry struct {
	Prefix    string
	NextHop   string
	Protocol  string
	Metric    uint32
	Interface string
	Active    bool
}

// VtyshRouteStatusReader reads IPv4 and IPv6 route state through vtysh.
type VtyshRouteStatusReader struct {
	run VtyshRunner
}

// NewVtyshRouteStatusReader creates a vtysh-backed route status reader.
func NewVtyshRouteStatusReader() *VtyshRouteStatusReader {
	return &VtyshRouteStatusReader{run: runVtyshMgmtCommand}
}

// NewVtyshRouteStatusReaderWithRunner creates a reader with a test runner.
func NewVtyshRouteStatusReaderWithRunner(run VtyshRunner) *VtyshRouteStatusReader {
	return &VtyshRouteStatusReader{run: run}
}

// ReadRouteStatus executes FRR's JSON route show commands and parses the result.
func (r *VtyshRouteStatusReader) ReadRouteStatus(ctx context.Context) (*RouteStatus, error) {
	if r.run == nil {
		r.run = runVtyshMgmtCommand
	}
	var routes []RouteStatusEntry
	for _, command := range []string{"show ip route json", "show ipv6 route json"} {
		output, err := r.run(ctx, command)
		if err != nil {
			return nil, NewApplyError("read FRR route status", err)
		}
		status, err := ParseRouteStatusJSON(output)
		if err != nil {
			return nil, NewApplyError("parse FRR route status", err)
		}
		routes = append(routes, status.Routes...)
	}
	sort.Slice(routes, func(i, j int) bool {
		return routeSortKey(routes[i]) < routeSortKey(routes[j])
	})
	return &RouteStatus{Routes: routes}, nil
}

// ParseRouteStatusJSON parses FRR's show ip/ipv6 route json output.
func ParseRouteStatusJSON(data []byte) (*RouteStatus, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	routes := collectRouteEntries("", root)
	sort.Slice(routes, func(i, j int) bool {
		return routeSortKey(routes[i]) < routeSortKey(routes[j])
	})
	return &RouteStatus{Routes: routes}, nil
}

func collectRouteEntries(prefix string, value any) []RouteStatusEntry {
	switch typed := value.(type) {
	case []any:
		var routes []RouteStatusEntry
		for _, item := range typed {
			routes = append(routes, collectRouteEntries(prefix, item)...)
		}
		return routes
	case map[string]any:
		if entry, ok := parseRouteObject(prefix, typed); ok {
			return entry
		}
		var routes []RouteStatusEntry
		for key, item := range typed {
			nextPrefix := prefix
			if looksLikeRoutePrefix(key) {
				nextPrefix = strings.TrimSpace(key)
			}
			routes = append(routes, collectRouteEntries(nextPrefix, item)...)
		}
		return routes
	default:
		return nil
	}
}

func parseRouteObject(prefix string, object map[string]any) ([]RouteStatusEntry, bool) {
	routePrefix := firstNonEmpty(prefix, stringFromNormalized(object, "prefix", "network", "destination", "dest"))
	if routePrefix == "" || !looksLikeRouteObject(object) {
		return nil, false
	}
	protocol := stringFromNormalized(object, "protocol", "routeprotocol", "routetype", "type")
	metric := uint32FromNormalized(object, "metric", "cost")
	routeActive := routeObjectActive(object)

	nexthops := routeNexthops(object)
	if len(nexthops) == 0 {
		return []RouteStatusEntry{{
			Prefix:    routePrefix,
			NextHop:   stringFromNormalized(object, "nexthop", "next-hop", "gateway", "via", "ip"),
			Protocol:  protocol,
			Metric:    metric,
			Interface: stringFromNormalized(object, "interface", "ifname", "interfacename", "interfaceName"),
			Active:    routeActive,
		}}, true
	}

	routes := make([]RouteStatusEntry, 0, len(nexthops))
	for _, nexthop := range nexthops {
		active := routeActive
		if lookupNormalized(nexthop, "active", "selected", "fib", "installed") != nil {
			active = boolFromNormalized(nexthop, "active", "selected", "fib", "installed")
		}
		routes = append(routes, RouteStatusEntry{
			Prefix:    routePrefix,
			NextHop:   stringFromNormalized(nexthop, "ip", "gateway", "via", "nexthop", "next-hop", "address"),
			Protocol:  protocol,
			Metric:    metric,
			Interface: stringFromNormalized(nexthop, "interface", "ifname", "interfacename", "interfaceName"),
			Active:    active,
		})
	}
	return routes, true
}

func looksLikeRouteObject(object map[string]any) bool {
	return lookupNormalized(object,
		"protocol", "routeprotocol", "routetype", "type",
		"nexthops", "nexthop", "nextHops", "paths",
		"metric", "cost",
		"selected", "destselected", "active", "installed", "fib",
		"interface", "ifname", "interfacename", "interfaceName",
	) != nil
}

func routeNexthops(object map[string]any) []map[string]any {
	value := lookupNormalized(object, "nexthops", "nexthop", "nextHops", "paths")
	switch typed := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				out = append(out, object)
			}
		}
		return out
	case []map[string]any:
		return typed
	case map[string]any:
		return []map[string]any{typed}
	default:
		return nil
	}
}

func routeObjectActive(object map[string]any) bool {
	for _, names := range [][]string{
		{"selected", "destselected", "route-selected"},
		{"active", "installed", "fib"},
	} {
		if lookupNormalized(object, names...) != nil {
			return boolFromNormalized(object, names...)
		}
	}
	return false
}

func looksLikeRoutePrefix(value string) bool {
	_, err := netip.ParsePrefix(strings.TrimSpace(value))
	return err == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uint32FromNormalized(object map[string]any, names ...string) uint32 {
	value := intFromNormalized(object, names...)
	if value < 0 {
		return 0
	}
	return uint32(value)
}

func routeSortKey(route RouteStatusEntry) string {
	return route.Prefix + "\x00" + route.Protocol + "\x00" + route.NextHop + "\x00" + route.Interface
}
