package frr

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// VRRPStatusReader reads FRR's operational VRRP state.
type VRRPStatusReader interface {
	ReadVRRPStatus(ctx context.Context) (*VRRPStatus, error)
}

// VRRPStatus is the parsed output of FRR's show vrrp json command.
type VRRPStatus struct {
	Groups []VRRPRouterStatus
}

// VRRPRouterStatus describes one VRRP router instance.
type VRRPRouterStatus struct {
	Interface string
	VRID      int
	State     string
	IPv4State string
	IPv6State string
}

// VtyshVRRPStatusReader reads VRRP state through vtysh.
type VtyshVRRPStatusReader struct {
	run VtyshRunner
}

// NewVtyshVRRPStatusReader creates a vtysh-backed VRRP status reader.
func NewVtyshVRRPStatusReader() *VtyshVRRPStatusReader {
	return &VtyshVRRPStatusReader{run: runVtyshMgmtCommand}
}

// NewVtyshVRRPStatusReaderWithRunner creates a reader with a test runner.
func NewVtyshVRRPStatusReaderWithRunner(run VtyshRunner) *VtyshVRRPStatusReader {
	return &VtyshVRRPStatusReader{run: run}
}

// ReadVRRPStatus executes FRR's JSON VRRP show command and parses the result.
func (r *VtyshVRRPStatusReader) ReadVRRPStatus(ctx context.Context) (*VRRPStatus, error) {
	if r.run == nil {
		r.run = runVtyshMgmtCommand
	}
	output, err := r.run(ctx, "show vrrp json")
	if err != nil {
		return nil, NewApplyError("read FRR VRRP status", err)
	}
	status, err := ParseVRRPStatusJSON(output)
	if err != nil {
		return nil, NewApplyError("parse FRR VRRP status", err)
	}
	return status, nil
}

// ParseVRRPStatusJSON parses FRR's show vrrp json output. FRR has changed JSON
// key spelling across releases, so this parser accepts both machine-style keys
// and label-like keys matching the text output.
func ParseVRRPStatusJSON(data []byte) (*VRRPStatus, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	var groups []VRRPRouterStatus
	for _, object := range collectVRRPObjects(root) {
		group := parseVRRPObject(object)
		if group.Interface == "" || group.VRID == 0 {
			continue
		}
		groups = append(groups, group)
	}
	return &VRRPStatus{Groups: groups}, nil
}

func collectVRRPObjects(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		var objects []map[string]any
		for _, item := range typed {
			objects = append(objects, collectVRRPObjects(item)...)
		}
		return objects
	case map[string]any:
		if looksLikeVRRPObject(typed) {
			return []map[string]any{typed}
		}
		var objects []map[string]any
		for _, item := range typed {
			objects = append(objects, collectVRRPObjects(item)...)
		}
		return objects
	default:
		return nil
	}
}

func looksLikeVRRPObject(object map[string]any) bool {
	return lookupNormalized(object, "interface", "ifname") != nil &&
		lookupNormalized(object, "virtualrouterid", "vrid", "id") != nil
}

func parseVRRPObject(object map[string]any) VRRPRouterStatus {
	group := VRRPRouterStatus{
		Interface: stringFromNormalized(object, "interface", "ifname"),
		VRID:      intFromNormalized(object, "virtualrouterid", "vrid", "id"),
		State:     stateFromNormalized(object, "state", "status"),
		IPv4State: stateFromNormalized(object,
			"statusv4", "v4status", "ipv4status", "statusipv4",
			"statev4", "v4state", "ipv4state", "stateipv4",
		),
		IPv6State: stateFromNormalized(object,
			"statusv6", "v6status", "ipv6status", "statusipv6",
			"statev6", "v6state", "ipv6state", "stateipv6",
		),
	}
	if group.IPv4State == "" {
		group.IPv4State = nestedState(object, "v4", "ipv4")
	}
	if group.IPv6State == "" {
		group.IPv6State = nestedState(object, "v6", "ipv6")
	}
	return group
}

func nestedState(object map[string]any, names ...string) string {
	value := lookupNormalized(object, names...)
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return stateFromNormalized(typed, "state", "status")
	default:
		return ""
	}
}

func stateFromNormalized(object map[string]any, names ...string) string {
	return strings.TrimSpace(stringFromNormalized(object, names...))
}

func stringFromNormalized(object map[string]any, names ...string) string {
	value := lookupNormalized(object, names...)
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func intFromNormalized(object map[string]any, names ...string) int {
	value := lookupNormalized(object, names...)
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func lookupNormalized(object map[string]any, names ...string) any {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[normalizeJSONKey(name)] = struct{}{}
	}
	for key, value := range object {
		if _, ok := wanted[normalizeJSONKey(key)]; ok {
			return value
		}
	}
	return nil
}

func normalizeJSONKey(key string) string {
	key = strings.ToLower(key)
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", "(", "", ")", "")
	return replacer.Replace(key)
}
