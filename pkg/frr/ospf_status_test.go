package frr

import (
	"context"
	"strings"
	"testing"
)

func TestParseOSPFNeighborJSONAcceptsMapOutput(t *testing.T) {
	status, err := ParseOSPFNeighborJSON([]byte(`{
		"neighbors": {
			"10.0.0.2": [
				{
					"ifaceAddress": "192.0.2.2",
					"ifaceName": "ge0-0-0",
					"nbrState": "Full/DROther",
					"priority": 1,
					"deadTime": "00:00:31",
					"upTimeInMsec": 65000
				}
			]
		}
	}`))
	if err != nil {
		t.Fatalf("ParseOSPFNeighborJSON() error = %v", err)
	}
	if len(status.Neighbors) != 1 {
		t.Fatalf("neighbors = %d, want 1", len(status.Neighbors))
	}
	got := status.Neighbors[0]
	if got.RouterID != "10.0.0.2" || got.Address != "192.0.2.2" || got.Interface != "ge0-0-0" ||
		got.State != "Full" || got.Role != "DROther" || got.Priority != 1 ||
		got.DeadTimeSecs != 31 || got.UptimeSecs != 65 {
		t.Fatalf("neighbor = %#v, want parsed OSPFv2 state", got)
	}
}

func TestParseOSPFNeighborJSONAcceptsArrayOutput(t *testing.T) {
	status, err := ParseOSPFNeighborJSON([]byte(`{
		"neighbors": [
			{
				"neighborId": "10.0.0.3",
				"linkLocalAddress": "fe80::1",
				"interfaceName": "ge0-0-1",
				"state": "Full",
				"role": "Backup",
				"deadTime": "35.000s",
				"duration": "00:01:05"
			}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseOSPFNeighborJSON() error = %v", err)
	}
	if len(status.Neighbors) != 1 {
		t.Fatalf("neighbors = %d, want 1", len(status.Neighbors))
	}
	got := status.Neighbors[0]
	if got.RouterID != "10.0.0.3" || got.Address != "fe80::1" || got.Interface != "ge0-0-1" ||
		got.State != "Full" || got.Role != "Backup" || got.DeadTimeSecs != 35 || got.UptimeSecs != 65 {
		t.Fatalf("neighbor = %#v, want parsed OSPFv3 state", got)
	}
}

func TestVtyshOSPFNeighborStatusReaderUsesFamilyCommands(t *testing.T) {
	var commands []string
	reader := NewVtyshOSPFNeighborStatusReaderWithRunner(func(ctx context.Context, command string) ([]byte, error) {
		commands = append(commands, command)
		return []byte(`{"neighbors":[]}`), nil
	})
	if _, err := reader.ReadOSPFNeighborStatus(context.Background(), false); err != nil {
		t.Fatalf("ReadOSPFNeighborStatus(ipv4) error = %v", err)
	}
	if _, err := reader.ReadOSPFNeighborStatus(context.Background(), true); err != nil {
		t.Fatalf("ReadOSPFNeighborStatus(ipv6) error = %v", err)
	}
	want := "show ip ospf neighbor json\nshow ipv6 ospf6 neighbor json"
	if strings.Join(commands, "\n") != want {
		t.Fatalf("commands = %q, want %q", strings.Join(commands, "\n"), want)
	}
}
