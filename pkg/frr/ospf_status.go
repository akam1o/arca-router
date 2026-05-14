package frr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OSPFNeighborStatusReader reads FRR's operational OSPF neighbor state.
type OSPFNeighborStatusReader interface {
	ReadOSPFNeighborStatus(ctx context.Context, ipv6 bool) (*OSPFNeighborStatus, error)
}

// OSPFNeighborStatus is the parsed output of FRR's OSPF neighbor JSON command.
type OSPFNeighborStatus struct {
	Neighbors []OSPFNeighbor
}

// OSPFNeighbor describes one OSPFv2 or OSPFv3 adjacency.
type OSPFNeighbor struct {
	RouterID     string
	Address      string
	Interface    string
	State        string
	Role         string
	Priority     uint32
	DeadTimeSecs uint64
	UptimeSecs   uint64
}

// VtyshOSPFNeighborStatusReader reads OSPF neighbor state through vtysh.
type VtyshOSPFNeighborStatusReader struct {
	run VtyshRunner
}

// NewVtyshOSPFNeighborStatusReader creates a vtysh-backed OSPF neighbor reader.
func NewVtyshOSPFNeighborStatusReader() *VtyshOSPFNeighborStatusReader {
	return &VtyshOSPFNeighborStatusReader{run: runVtyshMgmtCommand}
}

// NewVtyshOSPFNeighborStatusReaderWithRunner creates a reader with a test runner.
func NewVtyshOSPFNeighborStatusReaderWithRunner(run VtyshRunner) *VtyshOSPFNeighborStatusReader {
	return &VtyshOSPFNeighborStatusReader{run: run}
}

// ReadOSPFNeighborStatus executes FRR's JSON OSPF neighbor command and parses the result.
func (r *VtyshOSPFNeighborStatusReader) ReadOSPFNeighborStatus(ctx context.Context, ipv6 bool) (*OSPFNeighborStatus, error) {
	if r.run == nil {
		r.run = runVtyshMgmtCommand
	}
	command := "show ip ospf neighbor json"
	if ipv6 {
		command = "show ipv6 ospf6 neighbor json"
	}
	output, err := r.run(ctx, command)
	if err != nil {
		return nil, NewApplyError("read FRR OSPF neighbor status", err)
	}
	status, err := ParseOSPFNeighborJSON(output)
	if err != nil {
		return nil, NewApplyError("parse FRR OSPF neighbor status", err)
	}
	return status, nil
}

// ParseOSPFNeighborJSON parses FRR's show ip ospf / show ipv6 ospf6 neighbor json output.
func ParseOSPFNeighborJSON(data []byte) (*OSPFNeighborStatus, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	neighbors := mergeOSPFNeighbors(collectOSPFNeighbors("", root))
	sort.Slice(neighbors, func(i, j int) bool {
		return ospfNeighborSortKey(neighbors[i]) < ospfNeighborSortKey(neighbors[j])
	})
	return &OSPFNeighborStatus{Neighbors: neighbors}, nil
}

func collectOSPFNeighbors(routerID string, value any) []OSPFNeighbor {
	switch typed := value.(type) {
	case []any:
		var neighbors []OSPFNeighbor
		for _, item := range typed {
			neighbors = append(neighbors, collectOSPFNeighbors(routerID, item)...)
		}
		return neighbors
	case map[string]any:
		if neighbor, ok := parseOSPFNeighborObject(routerID, typed); ok {
			return []OSPFNeighbor{neighbor}
		}
		var neighbors []OSPFNeighbor
		for key, item := range typed {
			nextRouterID := routerID
			if looksLikeOSPFRouterID(key) {
				nextRouterID = strings.TrimSpace(key)
			}
			neighbors = append(neighbors, collectOSPFNeighbors(nextRouterID, item)...)
		}
		return neighbors
	default:
		return nil
	}
}

func parseOSPFNeighborObject(fallbackRouterID string, object map[string]any) (OSPFNeighbor, bool) {
	routerID := firstNonEmpty(
		stringFromNormalized(object, "routerid", "neighborid", "neighborrid", "nbrid", "id"),
		fallbackRouterID,
	)
	if routerID == "" || !looksLikeOSPFNeighborObject(object) {
		return OSPFNeighbor{}, false
	}
	state, role := ospfStateAndRole(object)
	return OSPFNeighbor{
		RouterID:     routerID,
		Address:      stringFromNormalized(object, "address", "neighboraddress", "neighborip", "ifaceaddress", "interfaceaddress", "linklocaladdress"),
		Interface:    stringFromNormalized(object, "interface", "ifname", "ifacename", "interfacename"),
		State:        state,
		Role:         role,
		Priority:     uint32FromNormalized(object, "priority", "nbrpriority", "neighborpriority"),
		DeadTimeSecs: ospfSecondsFromNormalized(object, []string{"deadtimemsec", "deadtimeinmsec", "deadmilliseconds"}, []string{"deadtime", "dead", "timer"}),
		UptimeSecs:   ospfSecondsFromNormalized(object, []string{"uptimemsec", "uptimeinmsec", "durationmsec"}, []string{"uptime", "duration", "updown"}),
	}, true
}

func looksLikeOSPFNeighborObject(object map[string]any) bool {
	return lookupNormalized(object,
		"routerid", "neighborid", "neighborrid", "nbrid",
		"state", "nbrstate", "neighborstate", "ospfstate",
		"address", "neighboraddress", "ifaceaddress", "linklocaladdress",
		"interface", "ifname", "ifacename", "interfacename",
		"deadtimemsec", "deadtime", "uptimemsec", "uptime", "duration",
	) != nil
}

func looksLikeOSPFRouterID(value string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	return err == nil && addr.Is4()
}

func ospfStateAndRole(object map[string]any) (string, string) {
	state := stateFromNormalized(object, "state", "nbrstate", "neighborstate", "ospfstate")
	role := stateFromNormalized(object, "role", "neighborrole")
	if before, after, ok := strings.Cut(state, "/"); ok {
		state = strings.TrimSpace(before)
		if role == "" {
			role = strings.TrimSpace(after)
		}
	}
	return state, role
}

func ospfSecondsFromNormalized(object map[string]any, millisecondNames, stringNames []string) uint64 {
	if value := uint64FromNormalized(object, millisecondNames...); value > 0 {
		return value / 1000
	}
	return parseOSPFDurationSecs(scalarStringFromNormalized(object, stringNames...))
}

func parseOSPFDurationSecs(value string) uint64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0
	}
	if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
		return parsed
	}
	if strings.Contains(value, ":") {
		return parseColonDuration(value)
	}
	if duration, err := time.ParseDuration(strings.ToLower(value)); err == nil && duration > 0 {
		return uint64(duration / time.Second)
	}
	return parseUnitDuration(value)
}

func mergeOSPFNeighbors(neighbors []OSPFNeighbor) []OSPFNeighbor {
	byNeighbor := make(map[string]OSPFNeighbor)
	for _, neighbor := range neighbors {
		if neighbor.RouterID == "" {
			continue
		}
		key := ospfNeighborSortKey(neighbor)
		existing, ok := byNeighbor[key]
		if !ok {
			byNeighbor[key] = neighbor
			continue
		}
		if existing.Address == "" {
			existing.Address = neighbor.Address
		}
		if existing.State == "" {
			existing.State = neighbor.State
		}
		if existing.Role == "" {
			existing.Role = neighbor.Role
		}
		if existing.Priority == 0 {
			existing.Priority = neighbor.Priority
		}
		if neighbor.DeadTimeSecs > existing.DeadTimeSecs {
			existing.DeadTimeSecs = neighbor.DeadTimeSecs
		}
		if neighbor.UptimeSecs > existing.UptimeSecs {
			existing.UptimeSecs = neighbor.UptimeSecs
		}
		byNeighbor[key] = existing
	}
	result := make([]OSPFNeighbor, 0, len(byNeighbor))
	for _, neighbor := range byNeighbor {
		result = append(result, neighbor)
	}
	return result
}

func ospfNeighborSortKey(neighbor OSPFNeighbor) string {
	return neighbor.RouterID + "\x00" + neighbor.Interface + "\x00" + neighbor.Address
}
