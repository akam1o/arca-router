package frr

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestGenerateVRRPConfig(t *testing.T) {
	got, err := GenerateVRRPConfig(&VRRPConfig{
		Groups: []VRRPGroup{
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254", Priority: 110, Preempt: true},
			{ID: 20, Interface: "ge0-0-0", VirtualAddress: "2001:db8::1"},
		},
	})
	if err != nil {
		t.Fatalf("GenerateVRRPConfig() error = %v", err)
	}
	for _, want := range []string{
		"interface ge0-0-0",
		" vrrp 10 version 3",
		" vrrp 10 priority 110",
		" vrrp 10 preempt",
		" vrrp 10 ip 192.0.2.254",
		" vrrp 20 ipv6 2001:db8::1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("VRRP config missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateVRRPConfigRejectsDuplicateGroups(t *testing.T) {
	_, err := GenerateVRRPConfig(&VRRPConfig{
		Groups: []VRRPGroup{
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254"},
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.253"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("GenerateVRRPConfig() error = %v, want duplicate group error", err)
	}
}

func TestGenerateFRRConfigConvertsVRRP(t *testing.T) {
	frrCfg, err := GenerateFRRConfig(&config.Config{
		Interfaces: map[string]*config.Interface{
			"ge-0/0/0": {Units: map[int]*config.Unit{}},
		},
		Protocols: &config.ProtocolConfig{
			VRRP: &config.VRRPConfig{Groups: map[string]*config.VRRPGroup{
				"10": {
					Name:           "10",
					Interface:      "ge-0/0/0",
					VirtualAddress: "192.0.2.254",
					Priority:       110,
					Preempt:        true,
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("GenerateFRRConfig() error = %v", err)
	}
	if frrCfg.VRRP == nil || len(frrCfg.VRRP.Groups) != 1 {
		t.Fatalf("VRRP config = %#v, want one group", frrCfg.VRRP)
	}
	if got := frrCfg.VRRP.Groups[0].Interface; got != "ge0-0-0" {
		t.Fatalf("VRRP interface = %q, want ge0-0-0", got)
	}
}

func TestGenerateFRRConfigRejectsInvalidVRRPGroupID(t *testing.T) {
	_, err := GenerateFRRConfig(&config.Config{
		Interfaces: map[string]*config.Interface{
			"ge-0/0/0": {Units: map[int]*config.Unit{}},
		},
		Protocols: &config.ProtocolConfig{
			VRRP: &config.VRRPConfig{Groups: map[string]*config.VRRPGroup{
				"bad": {Interface: "ge-0/0/0", VirtualAddress: "192.0.2.254"},
			}},
		},
	})
	if err == nil {
		t.Fatal("GenerateFRRConfig() error = nil, want invalid group id error")
	}
}
