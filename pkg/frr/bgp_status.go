package frr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// BGPSummaryStatusReader reads FRR's operational BGP summary state.
type BGPSummaryStatusReader interface {
	ReadBGPSummaryStatus(ctx context.Context) (*BGPSummaryStatus, error)
}

// BGPSummaryStatus is the parsed output of FRR's BGP summary JSON command.
type BGPSummaryStatus struct {
	Neighbors []BGPNeighborStatus
}

// BGPNeighborStatus describes one BGP peer from FRR operational state.
type BGPNeighborStatus struct {
	PeerAddress    string
	PeerAS         uint32
	State          string
	UptimeSecs     uint64
	PrefixReceived uint32
	PrefixSent     uint32
}

// VtyshBGPSummaryStatusReader reads BGP summary state through vtysh.
type VtyshBGPSummaryStatusReader struct {
	run VtyshRunner
}

// NewVtyshBGPSummaryStatusReader creates a vtysh-backed BGP summary status reader.
func NewVtyshBGPSummaryStatusReader() *VtyshBGPSummaryStatusReader {
	return &VtyshBGPSummaryStatusReader{run: runVtyshMgmtCommand}
}

// NewVtyshBGPSummaryStatusReaderWithRunner creates a reader with a test runner.
func NewVtyshBGPSummaryStatusReaderWithRunner(run VtyshRunner) *VtyshBGPSummaryStatusReader {
	return &VtyshBGPSummaryStatusReader{run: run}
}

// ReadBGPSummaryStatus executes FRR's JSON BGP summary command and parses the result.
func (r *VtyshBGPSummaryStatusReader) ReadBGPSummaryStatus(ctx context.Context) (*BGPSummaryStatus, error) {
	if r.run == nil {
		r.run = runVtyshMgmtCommand
	}
	output, err := r.run(ctx, "show bgp summary json")
	if err != nil {
		return nil, NewApplyError("read FRR BGP summary status", err)
	}
	status, err := ParseBGPSummaryJSON(output)
	if err != nil {
		return nil, NewApplyError("parse FRR BGP summary status", err)
	}
	return status, nil
}

// ParseBGPSummaryJSON parses FRR's show bgp summary json output.
func ParseBGPSummaryJSON(data []byte) (*BGPSummaryStatus, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	neighbors := mergeBGPNeighbors(collectBGPNeighborStatuses(root))
	sort.Slice(neighbors, func(i, j int) bool {
		return neighbors[i].PeerAddress < neighbors[j].PeerAddress
	})
	return &BGPSummaryStatus{Neighbors: neighbors}, nil
}

func collectBGPNeighborStatuses(value any) []BGPNeighborStatus {
	switch typed := value.(type) {
	case []any:
		var neighbors []BGPNeighborStatus
		for _, item := range typed {
			neighbors = append(neighbors, collectBGPNeighborStatuses(item)...)
		}
		return neighbors
	case map[string]any:
		if neighbor, ok := parseBGPNeighborObject(typed); ok {
			return []BGPNeighborStatus{neighbor}
		}
		var neighbors []BGPNeighborStatus
		for key, item := range typed {
			if child, ok := item.(map[string]any); ok && looksLikeBGPNeighborKey(key) &&
				lookupNormalized(child, "peer", "neighbor", "peeraddress", "address") == nil {
				child = cloneJSONObject(child)
				child["peer"] = key
				neighbors = append(neighbors, collectBGPNeighborStatuses(child)...)
				continue
			}
			neighbors = append(neighbors, collectBGPNeighborStatuses(item)...)
		}
		return neighbors
	default:
		return nil
	}
}

func parseBGPNeighborObject(object map[string]any) (BGPNeighborStatus, bool) {
	peer := stringFromNormalized(object, "peer", "neighbor", "peeraddress", "address")
	if peer == "" || !looksLikeBGPNeighborObject(object) {
		return BGPNeighborStatus{}, false
	}
	return BGPNeighborStatus{
		PeerAddress:    peer,
		PeerAS:         uint32FromNormalized(object, "remoteas", "peeras", "as"),
		State:          bgpStateFromObject(object),
		UptimeSecs:     bgpUptimeSecsFromObject(object),
		PrefixReceived: uint32FromNormalized(object, "pfxrcd", "prefixreceived", "prefixesreceived", "receivedprefixes", "acceptedprefixes"),
		PrefixSent:     uint32FromNormalized(object, "pfxsnt", "prefixsent", "prefixessent", "sentprefixes"),
	}, true
}

func looksLikeBGPNeighborObject(object map[string]any) bool {
	return lookupNormalized(object,
		"remoteas", "peeras", "as",
		"state", "peerstate", "connectionstate", "bgpstate", "statepfxrcd",
		"pfxrcd", "prefixreceived", "prefixesreceived",
		"pfxsnt", "prefixsent", "prefixessent",
		"peeruptime", "uptime", "peeruptimemsec", "uptimemsec",
	) != nil
}

func looksLikeBGPNeighborKey(key string) bool {
	_, err := netip.ParseAddr(strings.TrimSpace(key))
	return err == nil
}

func bgpStateFromObject(object map[string]any) string {
	state := stateFromNormalized(object, "state", "peerstate", "connectionstate", "bgpstate", "statepfxrcd")
	if state != "" {
		return state
	}
	if uint32FromNormalized(object, "pfxrcd", "prefixreceived", "prefixesreceived", "receivedprefixes", "acceptedprefixes") > 0 {
		return "Established"
	}
	return ""
}

func bgpUptimeSecsFromObject(object map[string]any) uint64 {
	for _, name := range []string{"peeruptimemsec", "uptimemsec", "uptimemilliseconds"} {
		if value := uint64FromNormalized(object, name); value > 0 {
			return value / 1000
		}
	}
	if value := uint64FromNormalized(object, "uptimesecs", "uptimeseconds"); value > 0 {
		return value
	}
	return parseBGPUptimeSecs(scalarStringFromNormalized(object, "peeruptime", "uptime", "updown"))
}

func uint64FromNormalized(object map[string]any, names ...string) uint64 {
	value := lookupNormalized(object, names...)
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0
		}
		return uint64(typed)
	case int:
		if typed < 0 {
			return 0
		}
		return uint64(typed)
	case string:
		parsed, _ := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func parseBGPUptimeSecs(value string) uint64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "never" {
		return 0
	}
	if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
		return parsed
	}
	if strings.Contains(value, ":") {
		return parseColonDuration(value)
	}
	return parseUnitDuration(value)
}

func parseColonDuration(value string) uint64 {
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 4 {
		return 0
	}
	var total uint64
	multipliers := []uint64{1, 60, 3600, 86400}
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			return 0
		}
		parsed, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return 0
		}
		total += parsed * multipliers[len(parts)-1-i]
	}
	return total
}

func parseUnitDuration(value string) uint64 {
	var total uint64
	var number strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			number.WriteRune(r)
			continue
		}
		if number.Len() == 0 {
			continue
		}
		parsed, err := strconv.ParseUint(number.String(), 10, 64)
		if err != nil {
			return 0
		}
		switch r {
		case 'd', 'D':
			total += parsed * 86400
		case 'h', 'H':
			total += parsed * 3600
		case 'm', 'M':
			total += parsed * 60
		case 's', 'S':
			total += parsed
		default:
			return 0
		}
		number.Reset()
	}
	if number.Len() != 0 {
		return 0
	}
	return total
}

func mergeBGPNeighbors(neighbors []BGPNeighborStatus) []BGPNeighborStatus {
	byPeer := make(map[string]BGPNeighborStatus)
	for _, neighbor := range neighbors {
		if neighbor.PeerAddress == "" {
			continue
		}
		key := strings.ToLower(neighbor.PeerAddress)
		existing, ok := byPeer[key]
		if !ok {
			byPeer[key] = neighbor
			continue
		}
		if existing.PeerAS == 0 {
			existing.PeerAS = neighbor.PeerAS
		}
		if existing.State == "" || (existing.State != "Established" && neighbor.State == "Established") {
			existing.State = neighbor.State
		}
		if neighbor.UptimeSecs > existing.UptimeSecs {
			existing.UptimeSecs = neighbor.UptimeSecs
		}
		existing.PrefixReceived += neighbor.PrefixReceived
		existing.PrefixSent += neighbor.PrefixSent
		byPeer[key] = existing
	}
	result := make([]BGPNeighborStatus, 0, len(byPeer))
	for _, neighbor := range byPeer {
		result = append(result, neighbor)
	}
	return result
}
