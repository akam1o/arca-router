package frr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVRRPSystemInterfaces(t *testing.T) {
	interfaces, err := vrrpSystemInterfaces(&VRRPConfig{Groups: []VRRPGroup{
		{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254"},
		{ID: 20, Interface: "ge0-0-0", VirtualAddress: "2001:db8::1"},
	}})
	if err != nil {
		t.Fatalf("vrrpSystemInterfaces() error = %v", err)
	}
	if len(interfaces) != 2 {
		t.Fatalf("vrrpSystemInterfaces() length = %d, want 2", len(interfaces))
	}
	if got, want := interfaces[0].Name, vrrpMacvlanName(vrrpIPv4Family, "ge0-0-0", 10); got != want {
		t.Fatalf("IPv4 Name = %q, want %q", got, want)
	}
	if len(interfaces[0].Name) > 15 {
		t.Fatalf("IPv4 Name length = %d, want <= 15", len(interfaces[0].Name))
	}
	if got, want := interfaces[0].MAC, "00:00:5e:00:01:0a"; got != want {
		t.Fatalf("IPv4 MAC = %q, want %q", got, want)
	}
	if got, want := interfaces[0].Address, "192.0.2.254/32"; got != want {
		t.Fatalf("IPv4 Address = %q, want %q", got, want)
	}
	if got, want := interfaces[1].MAC, "00:00:5e:00:02:14"; got != want {
		t.Fatalf("IPv6 MAC = %q, want %q", got, want)
	}
	if got, want := interfaces[1].Address, "2001:db8::1/128"; got != want {
		t.Fatalf("IPv6 Address = %q, want %q", got, want)
	}
}

func TestIPVRRPSystemPreparerPrepare(t *testing.T) {
	var commands []string
	linkMissing := errors.New("link missing")
	preparer := NewIPVRRPSystemPreparer(func(ctx context.Context, args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		commands = append(commands, command)
		switch command {
		case "link show":
			return []byte("9: arv4-100-dead@ge0-0-0: <BROADCAST,MULTICAST>\n"), nil
		case "link show dev " + vrrpMacvlanName(vrrpIPv4Family, "ge0-0-0", 10):
			return nil, linkMissing
		default:
			return nil, nil
		}
	})

	err := preparer.Prepare(context.Background(), &Config{
		VRRP: &VRRPConfig{Groups: []VRRPGroup{
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254"},
		}},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	name := vrrpMacvlanName(vrrpIPv4Family, "ge0-0-0", 10)
	for _, want := range []string{
		"link delete arv4-100-dead",
		"link add " + name + " link ge0-0-0 addrgenmode random type macvlan mode bridge",
		"link set dev " + name + " address 00:00:5e:00:01:0a",
		"addr replace 192.0.2.254/32 dev " + name,
		"link set dev " + name + " up",
	} {
		if !containsCommand(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, strings.Join(commands, "\n"))
		}
	}
}

func TestIPVRRPSystemPreparerSkipsEmptyInitialConfig(t *testing.T) {
	called := false
	preparer := NewIPVRRPSystemPreparer(func(ctx context.Context, args ...string) ([]byte, error) {
		called = true
		return nil, nil
	})

	if err := preparer.Prepare(context.Background(), &Config{}); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if called {
		t.Fatal("Prepare() ran ip command for empty initial config")
	}
}

func TestIPVRRPSystemPreparerCleansKnownInterfacesOnRemoval(t *testing.T) {
	name := vrrpMacvlanName(vrrpIPv4Family, "ge0-0-0", 10)
	var commands []string
	preparer := NewIPVRRPSystemPreparer(func(ctx context.Context, args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		commands = append(commands, command)
		switch command {
		case "link show":
			return []byte("9: " + name + "@ge0-0-0: <BROADCAST,MULTICAST>\n"), nil
		case "link show dev " + name:
			return nil, errors.New("link missing")
		default:
			return nil, nil
		}
	})

	err := preparer.Prepare(context.Background(), &Config{
		VRRP: &VRRPConfig{Groups: []VRRPGroup{
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254"},
		}},
	})
	if err != nil {
		t.Fatalf("initial Prepare() error = %v", err)
	}
	commands = nil
	if err := preparer.Prepare(context.Background(), &Config{}); err != nil {
		t.Fatalf("removal Prepare() error = %v", err)
	}
	if !containsCommand(commands, "link delete "+name) {
		t.Fatalf("commands missing cleanup for %q:\n%s", name, strings.Join(commands, "\n"))
	}
}

func TestIPVRRPSystemPreparerPersistsCleanupAcrossRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "vrrp-interfaces.json")
	name := vrrpMacvlanName(vrrpIPv4Family, "ge0-0-0", 10)
	var commands []string
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		commands = append(commands, command)
		switch command {
		case "link show":
			return []byte("9: " + name + "@ge0-0-0: <BROADCAST,MULTICAST>\n"), nil
		case "link show dev " + name:
			return nil, errors.New("link missing")
		default:
			return nil, nil
		}
	}

	preparer := NewIPVRRPSystemPreparerWithState(run, statePath)
	err := preparer.Prepare(context.Background(), &Config{
		VRRP: &VRRPConfig{Groups: []VRRPGroup{
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254"},
		}},
	})
	if err != nil {
		t.Fatalf("initial Prepare() error = %v", err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", statePath, err)
	}
	if !strings.Contains(string(data), name) {
		t.Fatalf("state file missing %q:\n%s", name, data)
	}

	commands = nil
	restarted := NewIPVRRPSystemPreparerWithState(run, statePath)
	if err := restarted.Prepare(context.Background(), &Config{}); err != nil {
		t.Fatalf("restarted Prepare() error = %v", err)
	}
	if !containsCommand(commands, "link delete "+name) {
		t.Fatalf("commands missing cleanup for %q after restart:\n%s", name, strings.Join(commands, "\n"))
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file still exists after cleanup: %v", err)
	}
}

func TestArcaVRRPInterfaceNames(t *testing.T) {
	got := arcaVRRPInterfaceNames(strings.Join([]string{
		"5: lo: <LOOPBACK>",
		"6: arv4-010-abcd@ge0-0-0: <BROADCAST,MULTICAST>",
		"7: arv6-020-1234@ge0-0-1: <BROADCAST,MULTICAST>",
		"8: vrrp4-2-1@eth0: <BROADCAST,MULTICAST>",
		"9: arv4-bad-name@eth0: <BROADCAST,MULTICAST>",
	}, "\n"))
	want := []string{"arv4-010-abcd", "arv6-020-1234"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("arcaVRRPInterfaceNames() = %#v, want %#v", got, want)
	}
}

func TestTransactionalApplierPreparesVRRPSystem(t *testing.T) {
	client := &recordingMgmtClient{}
	preparer := &recordingVRRPPreparer{}
	applier := NewTransactionalApplierWithPreparer(client, preparer)
	cfg := &Config{VRRP: &VRRPConfig{Groups: []VRRPGroup{
		{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254"},
	}}}

	if err := applier.ApplyConfig(context.Background(), "", cfg); err != nil {
		t.Fatalf("ApplyConfig() error = %v", err)
	}
	if preparer.calls != 1 {
		t.Fatalf("preparer calls = %d, want 1", preparer.calls)
	}
	if len(client.ops) == 0 {
		t.Fatal("mgmt client did not receive operations")
	}
}

func TestTransactionalApplierValidatesBeforeVRRPSystemPreparation(t *testing.T) {
	client := &recordingMgmtClient{}
	preparer := &recordingVRRPPreparer{}
	applier := NewTransactionalApplierWithPreparer(client, preparer)
	cfg := &Config{
		VRRP: &VRRPConfig{Groups: []VRRPGroup{
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254"},
		}},
		BGP: &BGPConfig{Neighbors: []BGPNeighbor{
			{IP: "192.0.2.2", RemoteAS: 65001},
		}},
	}

	err := applier.ApplyConfig(context.Background(), "", cfg)
	if err == nil || !strings.Contains(err.Error(), "BGP ASN is required") {
		t.Fatalf("ApplyConfig() error = %v, want BGP validation error", err)
	}
	if preparer.calls != 0 {
		t.Fatalf("preparer calls = %d, want 0", preparer.calls)
	}
	if len(client.ops) != 0 {
		t.Fatalf("mgmt client ops = %d, want 0", len(client.ops))
	}
}

func TestFileApplierValidatesVRRPBeforeSystemPreparation(t *testing.T) {
	preparer := &recordingVRRPPreparer{}
	applier := NewFileApplierWithPreparer(NewReloader(), preparer)
	cfg := &Config{VRRP: &VRRPConfig{Groups: []VRRPGroup{
		{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254", Priority: 300},
	}}}

	err := applier.ApplyConfig(context.Background(), "", cfg)
	if err == nil || !strings.Contains(err.Error(), "priority must be 1-254") {
		t.Fatalf("ApplyConfig() error = %v, want VRRP priority validation error", err)
	}
	if preparer.calls != 0 {
		t.Fatalf("preparer calls = %d, want 0", preparer.calls)
	}
}

func containsCommand(commands []string, want string) bool {
	for _, command := range commands {
		if command == want {
			return true
		}
	}
	return false
}

type recordingMgmtClient struct {
	ops []MgmtOperation
}

func (c *recordingMgmtClient) Apply(ctx context.Context, ops []MgmtOperation) error {
	c.ops = append([]MgmtOperation(nil), ops...)
	return nil
}

type recordingVRRPPreparer struct {
	calls int
}

func (p *recordingVRRPPreparer) Prepare(ctx context.Context, cfg *Config) error {
	p.calls++
	return nil
}
