package frr

import (
	"context"
	"strings"
	"testing"
)

func TestParseVRRPStatusJSONAcceptsFRRStyleLabels(t *testing.T) {
	status, err := ParseVRRPStatusJSON([]byte(`[
		{
			"Virtual Router ID": 10,
			"Interface": "ge0-0-0",
			"Status (v4)": "Master",
			"Status (v6)": "Backup"
		}
	]`))
	if err != nil {
		t.Fatalf("ParseVRRPStatusJSON() error = %v", err)
	}
	if len(status.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(status.Groups))
	}
	group := status.Groups[0]
	if group.Interface != "ge0-0-0" || group.VRID != 10 || group.IPv4State != "Master" || group.IPv6State != "Backup" {
		t.Fatalf("group = %#v, want parsed FRR labels", group)
	}
}

func TestParseVRRPStatusJSONAcceptsNestedState(t *testing.T) {
	status, err := ParseVRRPStatusJSON([]byte(`{
		"routers": [
			{
				"interface": "ge0-0-0",
				"vrid": "10",
				"v4": {"state": "Backup"}
			}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseVRRPStatusJSON() error = %v", err)
	}
	if len(status.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(status.Groups))
	}
	group := status.Groups[0]
	if group.Interface != "ge0-0-0" || group.VRID != 10 || group.IPv4State != "Backup" {
		t.Fatalf("group = %#v, want nested state", group)
	}
}

func TestVtyshVRRPStatusReaderRunsShowVRRPJSON(t *testing.T) {
	reader := NewVtyshVRRPStatusReaderWithRunner(func(ctx context.Context, command string) ([]byte, error) {
		if command != "show vrrp json" {
			t.Fatalf("command = %q, want show vrrp json", command)
		}
		return []byte(`[{"interface":"ge0-0-0","vrid":10,"state":"Master"}]`), nil
	})
	status, err := reader.ReadVRRPStatus(context.Background())
	if err != nil {
		t.Fatalf("ReadVRRPStatus() error = %v", err)
	}
	if len(status.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(status.Groups))
	}
}

func TestVtyshVRRPStatusReaderWrapsParseError(t *testing.T) {
	reader := NewVtyshVRRPStatusReaderWithRunner(func(ctx context.Context, command string) ([]byte, error) {
		return []byte(`not-json`), nil
	})
	_, err := reader.ReadVRRPStatus(context.Background())
	if err == nil || !strings.Contains(err.Error(), "parse FRR VRRP status") {
		t.Fatalf("ReadVRRPStatus() error = %v, want parse context", err)
	}
}
