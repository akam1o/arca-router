package frr

import (
	"context"
	"testing"
)

func TestParseBGPSummaryJSONAcceptsFRRPeerMap(t *testing.T) {
	status, err := ParseBGPSummaryJSON([]byte(`{
		"ipv4Unicast": {
			"peers": {
				"192.0.2.2": {
					"remoteAs": 65001,
					"state": "Established",
					"peerUptime": "01:02:03",
					"pfxRcd": 12,
					"pfxSnt": 8
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("ParseBGPSummaryJSON() error = %v", err)
	}
	if len(status.Neighbors) != 1 {
		t.Fatalf("neighbors = %d, want 1", len(status.Neighbors))
	}
	neighbor := status.Neighbors[0]
	if neighbor.PeerAddress != "192.0.2.2" || neighbor.PeerAS != 65001 ||
		neighbor.State != "Established" || neighbor.UptimeSecs != 3723 ||
		neighbor.PrefixReceived != 12 || neighbor.PrefixSent != 8 {
		t.Fatalf("neighbor = %#v, want parsed BGP summary", neighbor)
	}
}

func TestParseBGPSummaryJSONMergesAddressFamilies(t *testing.T) {
	status, err := ParseBGPSummaryJSON([]byte(`{
		"ipv4Unicast": {
			"peers": {
				"2001:db8::2": {
					"remoteAs": 65002,
					"state": "Established",
					"peerUptimeMsec": 300000,
					"pfxRcd": 5,
					"pfxSnt": 7
				}
			}
		},
		"ipv6Unicast": {
			"peers": {
				"2001:db8::2": {
					"remoteAs": 65002,
					"state": "Established",
					"peerUptime": "1d02h03m04s",
					"pfxRcd": 11,
					"pfxSnt": 13
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("ParseBGPSummaryJSON() error = %v", err)
	}
	if len(status.Neighbors) != 1 {
		t.Fatalf("neighbors = %d, want merged peer", len(status.Neighbors))
	}
	neighbor := status.Neighbors[0]
	if neighbor.PeerAddress != "2001:db8::2" || neighbor.PeerAS != 65002 ||
		neighbor.UptimeSecs != 93784 || neighbor.PrefixReceived != 16 || neighbor.PrefixSent != 20 {
		t.Fatalf("neighbor = %#v, want merged AF counters and max uptime", neighbor)
	}
}

func TestParseBGPSummaryJSONAcceptsNeighborArray(t *testing.T) {
	status, err := ParseBGPSummaryJSON([]byte(`{
		"neighbors": [
			{
				"peerAddress": "198.51.100.2",
				"peerAs": 65003,
				"peerState": "Active",
				"uptimeSecs": 42,
				"prefixReceived": 0,
				"prefixSent": 1
			}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseBGPSummaryJSON() error = %v", err)
	}
	if len(status.Neighbors) != 1 {
		t.Fatalf("neighbors = %d, want 1", len(status.Neighbors))
	}
	neighbor := status.Neighbors[0]
	if neighbor.PeerAddress != "198.51.100.2" || neighbor.State != "Active" ||
		neighbor.UptimeSecs != 42 || neighbor.PrefixSent != 1 {
		t.Fatalf("neighbor = %#v, want parsed array neighbor", neighbor)
	}
}

func TestParseBGPSummaryJSONRejectsInvalidJSON(t *testing.T) {
	if _, err := ParseBGPSummaryJSON([]byte(`not-json`)); err == nil {
		t.Fatal("ParseBGPSummaryJSON(invalid) error = nil, want error")
	}
}

func TestVtyshBGPSummaryStatusReaderRunsShowBGPSummaryJSON(t *testing.T) {
	var commands []string
	reader := NewVtyshBGPSummaryStatusReaderWithRunner(func(ctx context.Context, command string) ([]byte, error) {
		commands = append(commands, command)
		if command != "show bgp summary json" {
			t.Fatalf("unexpected command %q", command)
		}
		return []byte(`{
			"ipv4Unicast": {
				"peers": {
					"192.0.2.2": {
						"remoteAs": 65001,
						"state": "Established",
						"peerUptime": "01:00:00",
						"pfxRcd": 4,
						"pfxSnt": 5
					}
				}
			}
		}`), nil
	})

	status, err := reader.ReadBGPSummaryStatus(context.Background())
	if err != nil {
		t.Fatalf("ReadBGPSummaryStatus() error = %v", err)
	}
	if len(commands) != 1 || commands[0] != "show bgp summary json" {
		t.Fatalf("commands = %#v, want BGP summary JSON", commands)
	}
	if len(status.Neighbors) != 1 {
		t.Fatalf("neighbors = %d, want 1", len(status.Neighbors))
	}
	if got := status.Neighbors[0]; got.PeerAddress != "192.0.2.2" || got.PeerAS != 65001 ||
		got.State != "Established" || got.UptimeSecs != 3600 || got.PrefixReceived != 4 || got.PrefixSent != 5 {
		t.Fatalf("neighbor = %#v, want parsed BGP summary", got)
	}
}
