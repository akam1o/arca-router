package frr

import (
	"context"
	"strings"
	"testing"
)

func TestParseBFDStatusJSONAcceptsFRRStyleLabels(t *testing.T) {
	status, err := ParseBFDStatusJSON([]byte(`[
		{
			"Peer": "192.0.2.2",
			"Local Address": "192.0.2.1",
			"Interface": "ge0-0-0",
			"VRF": "default",
			"Status": "up",
			"Diagnostics": "ok",
			"Remote diagnostics": "ok",
			"Session down events": 2,
			"Rx fail packet": 1
		}
	]`))
	if err != nil {
		t.Fatalf("ParseBFDStatusJSON() error = %v", err)
	}
	if len(status.Peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(status.Peers))
	}
	peer := status.Peers[0]
	if peer.Peer != "192.0.2.2" || peer.LocalAddress != "192.0.2.1" ||
		peer.Interface != "ge0-0-0" || peer.VRF != "default" || peer.Status != "up" ||
		peer.SessionDownEvents != 2 || peer.RxFailPackets != 1 {
		t.Fatalf("peer = %#v, want parsed FRR labels", peer)
	}
}

func TestParseBFDStatusJSONAcceptsPeerKeyedMap(t *testing.T) {
	status, err := ParseBFDStatusJSON([]byte(`{
		"peers": {
			"2001:db8::2": {
				"status": "down",
				"local-address": "2001:db8::1",
				"session-down": "3"
			}
		}
	}`))
	if err != nil {
		t.Fatalf("ParseBFDStatusJSON() error = %v", err)
	}
	if len(status.Peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(status.Peers))
	}
	peer := status.Peers[0]
	if peer.Peer != "2001:db8::2" || peer.Status != "down" || peer.SessionDownEvents != 3 {
		t.Fatalf("peer = %#v, want peer-keyed map", peer)
	}
}

func TestParseBFDCountersJSON(t *testing.T) {
	counters, err := ParseBFDCountersJSON([]byte(`{
		"multihop": false,
		"peer": "192.0.2.2",
		"control-packet-input": 348,
		"control-packet-output": 685,
		"echo-packet-input": 10,
		"echo-packet-output": 11,
		"session-up": 1,
		"session-down": 2,
		"zebra-notifications": 4,
		"rx-fail-packet": 3
	}`))
	if err != nil {
		t.Fatalf("ParseBFDCountersJSON() error = %v", err)
	}
	if counters.Peer != "192.0.2.2" || counters.ControlPacketInput != 348 ||
		counters.SessionUpEvents != 1 || counters.SessionDownEvents != 2 ||
		counters.RxFailPackets != 3 {
		t.Fatalf("counters = %#v, want parsed counters", counters)
	}
}

func TestVtyshBFDStatusReaderRunsShowBFDJSONAndCounters(t *testing.T) {
	var commands []string
	reader := NewVtyshBFDStatusReaderWithRunner(func(ctx context.Context, command string) ([]byte, error) {
		commands = append(commands, command)
		switch command {
		case "show bfd peers json":
			return []byte(`[{"peer":"192.0.2.2","status":"up","local-address":"192.0.2.1","interface":"ge0-0-0"}]`), nil
		case "show bfd peer 192.0.2.2 local-address 192.0.2.1 interface ge0-0-0 counters json":
			return []byte(`{"peer":"192.0.2.2","session-down":2,"rx-fail-packet":1}`), nil
		default:
			t.Fatalf("unexpected command %q", command)
			return nil, nil
		}
	})
	status, err := reader.ReadBFDStatus(context.Background())
	if err != nil {
		t.Fatalf("ReadBFDStatus() error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %#v, want status and counters commands", commands)
	}
	if len(status.Peers) != 1 || status.Peers[0].SessionDownEvents != 2 || status.Peers[0].RxFailPackets != 1 {
		t.Fatalf("BFD peers = %#v, want merged counters", status.Peers)
	}
}

func TestVtyshBFDStatusReaderWrapsParseError(t *testing.T) {
	reader := NewVtyshBFDStatusReaderWithRunner(func(ctx context.Context, command string) ([]byte, error) {
		return []byte(`not-json`), nil
	})
	_, err := reader.ReadBFDStatus(context.Background())
	if err == nil || !strings.Contains(err.Error(), "parse FRR BFD status") {
		t.Fatalf("ReadBFDStatus() error = %v, want parse context", err)
	}
}
