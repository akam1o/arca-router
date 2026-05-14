package frr

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// BFDStatusReader reads FRR's operational BFD state.
type BFDStatusReader interface {
	ReadBFDStatus(ctx context.Context) (*BFDStatus, error)
}

// BFDStatus is the parsed output of FRR's BFD status commands.
type BFDStatus struct {
	Peers []BFDPeerStatus
}

// BFDPeerStatus describes one BFD peer session.
type BFDPeerStatus struct {
	Peer                string
	LocalAddress        string
	Interface           string
	VRF                 string
	Status              string
	Uptime              string
	Diagnostic          string
	RemoteDiagnostic    string
	PeerType            string
	Multihop            bool
	DetectMultiplier    int
	ReceiveInterval     int
	TransmitInterval    int
	ControlPacketInput  int
	ControlPacketOutput int
	EchoPacketInput     int
	EchoPacketOutput    int
	SessionUpEvents     int
	SessionDownEvents   int
	ZebraNotifications  int
	RxFailPackets       int
}

// VtyshBFDStatusReader reads BFD state through vtysh.
type VtyshBFDStatusReader struct {
	run VtyshRunner
}

// NewVtyshBFDStatusReader creates a vtysh-backed BFD status reader.
func NewVtyshBFDStatusReader() *VtyshBFDStatusReader {
	return &VtyshBFDStatusReader{run: runVtyshMgmtCommand}
}

// NewVtyshBFDStatusReaderWithRunner creates a reader with a test runner.
func NewVtyshBFDStatusReaderWithRunner(run VtyshRunner) *VtyshBFDStatusReader {
	return &VtyshBFDStatusReader{run: run}
}

// ReadBFDStatus executes FRR's JSON BFD show commands and parses the result.
func (r *VtyshBFDStatusReader) ReadBFDStatus(ctx context.Context) (*BFDStatus, error) {
	if r.run == nil {
		r.run = runVtyshMgmtCommand
	}
	output, err := r.run(ctx, "show bfd peers json")
	if err != nil {
		return nil, NewApplyError("read FRR BFD status", err)
	}
	status, err := ParseBFDStatusJSON(output)
	if err != nil {
		return nil, NewApplyError("parse FRR BFD status", err)
	}
	for i := range status.Peers {
		counterOutput, err := r.run(ctx, bfdPeerCounterCommand(status.Peers[i]))
		if err != nil {
			continue
		}
		counters, err := ParseBFDCountersJSON(counterOutput)
		if err != nil {
			continue
		}
		mergeBFDPeerCounters(&status.Peers[i], counters)
	}
	return status, nil
}

// ParseBFDStatusJSON parses FRR's show bfd peers json output.
func ParseBFDStatusJSON(data []byte) (*BFDStatus, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	var peers []BFDPeerStatus
	for _, object := range collectBFDObjects(root) {
		peer := parseBFDPeerObject(object)
		if peer.Peer == "" {
			continue
		}
		peers = append(peers, peer)
	}
	return &BFDStatus{Peers: peers}, nil
}

// ParseBFDCountersJSON parses FRR's show bfd peer ... counters json output.
func ParseBFDCountersJSON(data []byte) (*BFDPeerStatus, error) {
	status, err := ParseBFDStatusJSON(data)
	if err != nil {
		return nil, err
	}
	if len(status.Peers) == 0 {
		return &BFDPeerStatus{}, nil
	}
	return &status.Peers[0], nil
}

func collectBFDObjects(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		var objects []map[string]any
		for _, item := range typed {
			objects = append(objects, collectBFDObjects(item)...)
		}
		return objects
	case map[string]any:
		if looksLikeBFDObject(typed) {
			return []map[string]any{typed}
		}
		var objects []map[string]any
		for key, item := range typed {
			if child, ok := item.(map[string]any); ok && looksLikeBFDPeerKey(key) &&
				lookupNormalized(child, "peer", "peeraddress", "peeraddr") == nil {
				child = cloneJSONObject(child)
				child["peer"] = key
				objects = append(objects, collectBFDObjects(child)...)
				continue
			}
			objects = append(objects, collectBFDObjects(item)...)
		}
		return objects
	default:
		return nil
	}
}

func looksLikeBFDObject(object map[string]any) bool {
	return lookupNormalized(object, "peer", "peeraddress", "peeraddr", "remoteaddress", "remote", "dst", "destination") != nil
}

func looksLikeBFDPeerKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	return strings.Contains(key, ".") || strings.Contains(key, ":")
}

func cloneJSONObject(object map[string]any) map[string]any {
	clone := make(map[string]any, len(object)+1)
	for key, value := range object {
		clone[key] = value
	}
	return clone
}

func parseBFDPeerObject(object map[string]any) BFDPeerStatus {
	return BFDPeerStatus{
		Peer:                stringFromNormalized(object, "peer", "peeraddress", "peeraddr", "remoteaddress", "remote", "dst", "destination"),
		LocalAddress:        stringFromNormalized(object, "localaddress", "localaddr", "local", "source", "src"),
		Interface:           stringFromNormalized(object, "interface", "ifname", "interfacename"),
		VRF:                 stringFromNormalized(object, "vrf", "vrfname"),
		Status:              stateFromNormalized(object, "status", "state", "sessionstate"),
		Uptime:              scalarStringFromNormalized(object, "uptime", "uptimeformatted", "upduration"),
		Diagnostic:          stringFromNormalized(object, "diagnostic", "diagnostics", "localdiagnostic", "localdiagnostics"),
		RemoteDiagnostic:    stringFromNormalized(object, "remotediagnostic", "remotediagnostics"),
		PeerType:            stringFromNormalized(object, "peertype", "type"),
		Multihop:            boolFromNormalized(object, "multihop", "multi-hop"),
		DetectMultiplier:    intFromNormalized(object, "detectmultiplier", "detectmult", "detectionmultiplier"),
		ReceiveInterval:     intFromNormalized(object, "receiveinterval", "rxinterval", "localreceiveinterval"),
		TransmitInterval:    intFromNormalized(object, "transmitinterval", "transmissioninterval", "txinterval", "localtransmitinterval"),
		ControlPacketInput:  intFromNormalized(object, "controlpacketinput", "controlpacketsinput"),
		ControlPacketOutput: intFromNormalized(object, "controlpacketoutput", "controlpacketsoutput"),
		EchoPacketInput:     intFromNormalized(object, "echopacketinput", "echopacketsinput"),
		EchoPacketOutput:    intFromNormalized(object, "echopacketoutput", "echopacketsoutput"),
		SessionUpEvents:     intFromNormalized(object, "sessionup", "sessionupevents"),
		SessionDownEvents:   intFromNormalized(object, "sessiondown", "sessiondownevents", "downcount", "sessiondowncount"),
		ZebraNotifications:  intFromNormalized(object, "zebranotifications"),
		RxFailPackets:       intFromNormalized(object, "rxfailpacket", "rxfailpackets", "receivefailures", "rxfailures"),
	}
}

func scalarStringFromNormalized(object map[string]any, names ...string) string {
	value := lookupNormalized(object, names...)
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func boolFromNormalized(object map[string]any, names ...string) bool {
	value := lookupNormalized(object, names...)
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}

func mergeBFDPeerCounters(peer *BFDPeerStatus, counters *BFDPeerStatus) {
	if peer == nil || counters == nil {
		return
	}
	peer.ControlPacketInput = counters.ControlPacketInput
	peer.ControlPacketOutput = counters.ControlPacketOutput
	peer.EchoPacketInput = counters.EchoPacketInput
	peer.EchoPacketOutput = counters.EchoPacketOutput
	peer.SessionUpEvents = counters.SessionUpEvents
	peer.SessionDownEvents = counters.SessionDownEvents
	peer.ZebraNotifications = counters.ZebraNotifications
	peer.RxFailPackets = counters.RxFailPackets
}

func bfdPeerCounterCommand(peer BFDPeerStatus) string {
	var b strings.Builder
	b.WriteString("show bfd")
	if peer.VRF != "" {
		b.WriteString(" vrf ")
		b.WriteString(peer.VRF)
	}
	b.WriteString(" peer ")
	b.WriteString(peer.Peer)
	if peer.Multihop {
		b.WriteString(" multihop")
	}
	if peer.LocalAddress != "" {
		b.WriteString(" local-address ")
		b.WriteString(peer.LocalAddress)
	}
	if peer.Interface != "" {
		b.WriteString(" interface ")
		b.WriteString(peer.Interface)
	}
	b.WriteString(" counters json")
	return b.String()
}
