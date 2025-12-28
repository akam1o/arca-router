package main

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/akam1o/arca-router/pkg/vpp"
)

// mockVPPClient is a mock implementation of vpp.Client for testing
type mockVPPClient struct {
	interfaces []*vpp.Interface
	lcpPairs   []*vpp.LCPInterface
	connectErr error
	listErr    error
	lcpErr     error
	closed     bool
}

func (m *mockVPPClient) Connect(ctx context.Context) error {
	return m.connectErr
}

func (m *mockVPPClient) Close() error {
	m.closed = true
	return nil
}

func (m *mockVPPClient) CreateInterface(ctx context.Context, req *vpp.CreateInterfaceRequest) (*vpp.Interface, error) {
	return nil, nil
}

func (m *mockVPPClient) SetInterfaceUp(ctx context.Context, ifIndex uint32) error {
	return nil
}

func (m *mockVPPClient) SetInterfaceDown(ctx context.Context, ifIndex uint32) error {
	return nil
}

func (m *mockVPPClient) SetInterfaceAddress(ctx context.Context, ifIndex uint32, address *net.IPNet) error {
	return nil
}

func (m *mockVPPClient) DeleteInterfaceAddress(ctx context.Context, ifIndex uint32, address *net.IPNet) error {
	return nil
}

func (m *mockVPPClient) GetInterface(ctx context.Context, ifIndex uint32) (*vpp.Interface, error) {
	for _, iface := range m.interfaces {
		if iface.SwIfIndex == ifIndex {
			return iface, nil
		}
	}
	return nil, nil
}

func (m *mockVPPClient) ListInterfaces(ctx context.Context) ([]*vpp.Interface, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.interfaces, nil
}

func (m *mockVPPClient) CreateLCPInterface(ctx context.Context, ifIndex uint32, linuxIfName string) error {
	return nil
}

func (m *mockVPPClient) DeleteLCPInterface(ctx context.Context, ifIndex uint32) error {
	return nil
}

func (m *mockVPPClient) GetLCPInterface(ctx context.Context, ifIndex uint32) (*vpp.LCPInterface, error) {
	for _, lcp := range m.lcpPairs {
		if lcp.VPPSwIfIndex == ifIndex {
			return lcp, nil
		}
	}
	return nil, nil
}

func (m *mockVPPClient) ListLCPInterfaces(ctx context.Context) ([]*vpp.LCPInterface, error) {
	if m.lcpErr != nil {
		return nil, m.lcpErr
	}
	return m.lcpPairs, nil
}

func (m *mockVPPClient) GetVersion(ctx context.Context) (string, error) {
	return "24.10.0", nil
}

func TestShowInterfaceTable(t *testing.T) {
	_, ipnet1, _ := net.ParseCIDR("192.168.1.1/24")
	_, ipnet2, _ := net.ParseCIDR("2001:db8::1/64")

	interfaces := []*vpp.Interface{
		{
			SwIfIndex: 1,
			Name:      "ge-0/0/0",
			AdminUp:   true,
			LinkUp:    true,
			MAC:       net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
			Addresses: []*net.IPNet{ipnet1},
		},
		{
			SwIfIndex: 2,
			Name:      "xe-0/1/0",
			AdminUp:   false,
			LinkUp:    false,
			MAC:       net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x06},
			Addresses: []*net.IPNet{ipnet2},
		},
	}

	lcpMap := make(map[uint32]*vpp.LCPInterface)
	lcpMap[1] = &vpp.LCPInterface{
		VPPSwIfIndex: 1,
		LinuxIfName:  "ge0-0-0",
		JunosName:    "ge-0/0/0",
	}

	exitCode := showInterfaceTable(interfaces, lcpMap)
	if exitCode != ExitSuccess {
		t.Errorf("showInterfaceTable() exit code = %d, want %d", exitCode, ExitSuccess)
	}
}

func TestShowInterfaceTable_Empty(t *testing.T) {
	interfaces := []*vpp.Interface{}
	lcpMap := make(map[uint32]*vpp.LCPInterface)

	exitCode := showInterfaceTable(interfaces, lcpMap)
	if exitCode != ExitSuccess {
		t.Errorf("showInterfaceTable() with empty interfaces exit code = %d, want %d", exitCode, ExitSuccess)
	}
}

func TestShowInterfaceTable_NoLCP(t *testing.T) {
	interfaces := []*vpp.Interface{
		{
			SwIfIndex: 1,
			Name:      "ge-0/0/0",
			AdminUp:   true,
			LinkUp:    true,
			MAC:       net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
			Addresses: []*net.IPNet{},
		},
	}

	lcpMap := make(map[uint32]*vpp.LCPInterface)

	exitCode := showInterfaceTable(interfaces, lcpMap)
	if exitCode != ExitSuccess {
		t.Errorf("showInterfaceTable() without LCP exit code = %d, want %d", exitCode, ExitSuccess)
	}
}

func TestShowInterfaceDetail(t *testing.T) {
	_, ipnet1, _ := net.ParseCIDR("192.168.1.1/24")

	interfaces := []*vpp.Interface{
		{
			SwIfIndex: 1,
			Name:      "ge-0/0/0",
			AdminUp:   true,
			LinkUp:    true,
			MAC:       net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
			Addresses: []*net.IPNet{ipnet1},
		},
	}

	lcpMap := make(map[uint32]*vpp.LCPInterface)
	lcpMap[1] = &vpp.LCPInterface{
		VPPSwIfIndex: 1,
		LinuxIfName:  "ge0-0-0",
		JunosName:    "ge-0/0/0",
		HostIfType:   "tap",
		Netns:        "default",
	}

	exitCode := showInterfaceDetail(interfaces, lcpMap, "ge-0/0/0")
	if exitCode != ExitSuccess {
		t.Errorf("showInterfaceDetail() exit code = %d, want %d", exitCode, ExitSuccess)
	}
}

func TestShowInterfaceDetail_NotFound(t *testing.T) {
	interfaces := []*vpp.Interface{
		{
			SwIfIndex: 1,
			Name:      "ge-0/0/0",
			AdminUp:   true,
			LinkUp:    true,
			MAC:       net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
			Addresses: []*net.IPNet{},
		},
	}

	lcpMap := make(map[uint32]*vpp.LCPInterface)

	exitCode := showInterfaceDetail(interfaces, lcpMap, "xe-0/1/0")
	if exitCode != ExitOperationError {
		t.Errorf("showInterfaceDetail() for non-existent interface exit code = %d, want %d", exitCode, ExitOperationError)
	}
}

func TestShowInterfaceDetail_NoLCP(t *testing.T) {
	interfaces := []*vpp.Interface{
		{
			SwIfIndex: 1,
			Name:      "ge-0/0/0",
			AdminUp:   true,
			LinkUp:    true,
			MAC:       net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
			Addresses: []*net.IPNet{},
		},
	}

	lcpMap := make(map[uint32]*vpp.LCPInterface)

	exitCode := showInterfaceDetail(interfaces, lcpMap, "ge-0/0/0")
	if exitCode != ExitSuccess {
		t.Errorf("showInterfaceDetail() without LCP exit code = %d, want %d", exitCode, ExitSuccess)
	}
}

func TestIfState(t *testing.T) {
	tests := []struct {
		name string
		up   bool
		want string
	}{
		{"state up", true, "up"},
		{"state down", false, "down"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ifState(tt.up)
			if got != tt.want {
				t.Errorf("ifState(%v) = %q, want %q", tt.up, got, tt.want)
			}
		})
	}
}

func TestCmdShowInterfaces_WithMockClient(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("192.168.1.1/24")

	mockClient := &mockVPPClient{
		interfaces: []*vpp.Interface{
			{
				SwIfIndex: 1,
				Name:      "ge-0/0/0",
				AdminUp:   true,
				LinkUp:    true,
				MAC:       net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
				Addresses: []*net.IPNet{ipnet},
			},
		},
		lcpPairs: []*vpp.LCPInterface{
			{
				VPPSwIfIndex: 1,
				LinuxIfName:  "ge0-0-0",
				JunosName:    "ge-0/0/0",
			},
		},
	}

	// Inject mock client
	oldFactory := defaultVPPClientFactory
	defer func() { defaultVPPClientFactory = oldFactory }()

	defaultVPPClientFactory = func(f *flags) (vpp.Client, error) {
		return mockClient, nil
	}

	f := &flags{
		vppSocket:  "/run/vpp/api.sock",
		configPath: "/etc/arca-router/arca-router.conf",
		debug:      false,
	}

	exitCode := cmdShowInterfaces(context.Background(), []string{}, f)
	if exitCode != ExitSuccess {
		t.Errorf("cmdShowInterfaces() with mock client exit code = %d, want %d", exitCode, ExitSuccess)
	}

	if !mockClient.closed {
		t.Error("cmdShowInterfaces() did not close VPP client")
	}
}

func TestCmdShowInterfaces_ConnectError(t *testing.T) {
	mockClient := &mockVPPClient{
		connectErr: errors.New("connection refused"),
	}

	// Inject mock client
	oldFactory := defaultVPPClientFactory
	defer func() { defaultVPPClientFactory = oldFactory }()

	defaultVPPClientFactory = func(f *flags) (vpp.Client, error) {
		return mockClient, nil
	}

	f := &flags{
		vppSocket:  "/run/vpp/api.sock",
		configPath: "/etc/arca-router/arca-router.conf",
		debug:      false,
	}

	exitCode := cmdShowInterfaces(context.Background(), []string{}, f)
	if exitCode != ExitOperationError {
		t.Errorf("cmdShowInterfaces() with connect error exit code = %d, want %d", exitCode, ExitOperationError)
	}
}

func TestCmdShowInterfaces_ListError(t *testing.T) {
	mockClient := &mockVPPClient{
		listErr: errors.New("failed to list interfaces"),
	}

	// Inject mock client
	oldFactory := defaultVPPClientFactory
	defer func() { defaultVPPClientFactory = oldFactory }()

	defaultVPPClientFactory = func(f *flags) (vpp.Client, error) {
		return mockClient, nil
	}

	f := &flags{
		vppSocket:  "/run/vpp/api.sock",
		configPath: "/etc/arca-router/arca-router.conf",
		debug:      false,
	}

	exitCode := cmdShowInterfaces(context.Background(), []string{}, f)
	if exitCode != ExitOperationError {
		t.Errorf("cmdShowInterfaces() with list error exit code = %d, want %d", exitCode, ExitOperationError)
	}
}

func TestCmdShowInterfaces_LCPError(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("192.168.1.1/24")

	mockClient := &mockVPPClient{
		interfaces: []*vpp.Interface{
			{
				SwIfIndex: 1,
				Name:      "ge-0/0/0",
				AdminUp:   true,
				LinkUp:    true,
				MAC:       net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
				Addresses: []*net.IPNet{ipnet},
			},
		},
		lcpErr: errors.New("LCP plugin not loaded"),
	}

	// Inject mock client
	oldFactory := defaultVPPClientFactory
	defer func() { defaultVPPClientFactory = oldFactory }()

	defaultVPPClientFactory = func(f *flags) (vpp.Client, error) {
		return mockClient, nil
	}

	f := &flags{
		vppSocket:  "/run/vpp/api.sock",
		configPath: "/etc/arca-router/arca-router.conf",
		debug:      false,
	}

	// LCP error should be non-fatal, command should still succeed
	exitCode := cmdShowInterfaces(context.Background(), []string{}, f)
	if exitCode != ExitSuccess {
		t.Errorf("cmdShowInterfaces() with LCP error exit code = %d, want %d (LCP error should be non-fatal)", exitCode, ExitSuccess)
	}
}
