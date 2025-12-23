package vpp

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/akam1o/arca-router/pkg/errors"
)

// MockClient is a mock implementation of the VPP Client interface for testing
type MockClient struct {
	mu         sync.RWMutex
	connected  bool
	interfaces map[uint32]*Interface
	nextIfIdx  uint32

	// Hooks for testing error scenarios
	ConnectError                error
	CreateInterfaceError        error
	SetInterfaceUpError         error
	SetInterfaceDownError       error
	SetInterfaceAddressError    error
	DeleteInterfaceAddressError error
	GetInterfaceError           error
	ListInterfacesError         error
}

// NewMockClient creates a new mock VPP client
func NewMockClient() *MockClient {
	return &MockClient{
		interfaces: make(map[uint32]*Interface),
		nextIfIdx:  1, // Start from 1 (0 is reserved for local0)
	}
}

// deepCopyInterface creates a deep copy of an Interface
func deepCopyInterface(iface *Interface) *Interface {
	if iface == nil {
		return nil
	}

	copy := &Interface{
		SwIfIndex: iface.SwIfIndex,
		Name:      iface.Name,
		AdminUp:   iface.AdminUp,
		LinkUp:    iface.LinkUp,
	}

	// Deep copy MAC address
	if len(iface.MAC) > 0 {
		copy.MAC = make(net.HardwareAddr, len(iface.MAC))
		copyBytes(copy.MAC, iface.MAC)
	}

	// Deep copy addresses
	if len(iface.Addresses) > 0 {
		copy.Addresses = make([]*net.IPNet, len(iface.Addresses))
		for i, addr := range iface.Addresses {
			copy.Addresses[i] = deepCopyIPNet(addr)
		}
	}

	return copy
}

// deepCopyIPNet creates a deep copy of a net.IPNet
func deepCopyIPNet(ipnet *net.IPNet) *net.IPNet {
	if ipnet == nil {
		return nil
	}

	copy := &net.IPNet{
		IP:   make(net.IP, len(ipnet.IP)),
		Mask: make(net.IPMask, len(ipnet.Mask)),
	}

	copyBytes(copy.IP, ipnet.IP)
	copyBytes(copy.Mask, ipnet.Mask)

	return copy
}

// copyBytes copies bytes from src to dst
func copyBytes(dst, src []byte) {
	for i := range src {
		dst[i] = src[i]
	}
}

// ipNetEqual compares two net.IPNet values for equality
func ipNetEqual(a, b *net.IPNet) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Compare IP addresses
	if !a.IP.Equal(b.IP) {
		return false
	}

	// Compare masks byte-wise
	if len(a.Mask) != len(b.Mask) {
		return false
	}
	for i := range a.Mask {
		if a.Mask[i] != b.Mask[i] {
			return false
		}
	}

	return true
}

// Connect establishes a mock connection to VPP
func (m *MockClient) Connect(ctx context.Context) error {
	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	if m.ConnectError != nil {
		return m.ConnectError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Already connected to VPP",
			"VPP connection already established",
			"Close the existing connection before reconnecting",
		)
	}

	m.connected = true
	return nil
}

// Close closes the mock VPP connection
func (m *MockClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before closing",
		)
	}

	m.connected = false
	return nil
}

// CreateInterface creates a mock VPP interface
func (m *MockClient) CreateInterface(ctx context.Context, req *CreateInterfaceRequest) (*Interface, error) {
	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate request is not nil
	if req == nil {
		return nil, errors.New(
			errors.ErrCodeVPPOperation,
			"CreateInterfaceRequest is nil",
			"Request parameter must not be nil",
			"Provide a valid CreateInterfaceRequest",
		)
	}

	if m.CreateInterfaceError != nil {
		return nil, m.CreateInterfaceError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return nil, errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before creating interfaces",
		)
	}

	// Validate request
	if req.Type == "" {
		return nil, errors.New(
			errors.ErrCodeVPPOperation,
			"Interface type is required",
			"Interface type must be specified",
			"Specify a valid interface type (avf, rdma, tap)",
		)
	}

	// Validate interface type
	validTypes := map[InterfaceType]bool{
		InterfaceTypeAVF:  true,
		InterfaceTypeRDMA: true,
		InterfaceTypeTap:  true,
	}
	if !validTypes[req.Type] {
		return nil, errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Invalid interface type: %s", req.Type),
			"Interface type must be one of: avf, rdma, tap",
			"Use a valid interface type",
		)
	}

	// Create interface
	iface := &Interface{
		SwIfIndex: m.nextIfIdx,
		Name:      fmt.Sprintf("%s%d", req.Type, m.nextIfIdx),
		AdminUp:   false,
		LinkUp:    false,
		MAC:       net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, byte(m.nextIfIdx)},
		Addresses: []*net.IPNet{},
	}

	// Store a copy to prevent external mutation
	m.interfaces[m.nextIfIdx] = deepCopyInterface(iface)
	m.nextIfIdx++

	// Return a copy to prevent external mutation
	return deepCopyInterface(iface), nil
}

// SetInterfaceUp sets a mock interface to admin up state
func (m *MockClient) SetInterfaceUp(ctx context.Context, ifIndex uint32) error {
	if m.SetInterfaceUpError != nil {
		return m.SetInterfaceUpError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before setting interface state",
		)
	}

	iface, ok := m.interfaces[ifIndex]
	if !ok {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Interface with index %d not found", ifIndex),
			"Interface does not exist",
			"Create the interface before setting its state",
		)
	}

	iface.AdminUp = true
	iface.LinkUp = true // In mock, link is always up when admin is up
	return nil
}

// SetInterfaceDown sets a mock interface to admin down state
func (m *MockClient) SetInterfaceDown(ctx context.Context, ifIndex uint32) error {
	if m.SetInterfaceDownError != nil {
		return m.SetInterfaceDownError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before setting interface state",
		)
	}

	iface, ok := m.interfaces[ifIndex]
	if !ok {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Interface with index %d not found", ifIndex),
			"Interface does not exist",
			"Create the interface before setting its state",
		)
	}

	iface.AdminUp = false
	iface.LinkUp = false
	return nil
}

// SetInterfaceAddress adds an IP address to a mock interface
func (m *MockClient) SetInterfaceAddress(ctx context.Context, ifIndex uint32, addr *net.IPNet) error {
	if m.SetInterfaceAddressError != nil {
		return m.SetInterfaceAddressError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before setting interface address",
		)
	}

	iface, ok := m.interfaces[ifIndex]
	if !ok {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Interface with index %d not found", ifIndex),
			"Interface does not exist",
			"Create the interface before setting its address",
		)
	}

	if addr == nil {
		return errors.New(
			errors.ErrCodeVPPOperation,
			"Address is nil",
			"IP address must not be nil",
			"Provide a valid IP address",
		)
	}

	// Check if address already exists
	for _, existing := range iface.Addresses {
		if ipNetEqual(existing, addr) {
			return errors.New(
				errors.ErrCodeVPPOperation,
				fmt.Sprintf("Address %s already exists on interface %d", addr.String(), ifIndex),
				"Address already configured",
				"Remove the existing address before adding a new one",
			)
		}
	}

	// Store a deep copy to prevent external mutation
	iface.Addresses = append(iface.Addresses, deepCopyIPNet(addr))
	return nil
}

// DeleteInterfaceAddress removes an IP address from a mock interface
func (m *MockClient) DeleteInterfaceAddress(ctx context.Context, ifIndex uint32, addr *net.IPNet) error {
	if m.DeleteInterfaceAddressError != nil {
		return m.DeleteInterfaceAddressError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before deleting interface address",
		)
	}

	iface, ok := m.interfaces[ifIndex]
	if !ok {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Interface with index %d not found", ifIndex),
			"Interface does not exist",
			"Interface must exist to delete its address",
		)
	}

	if addr == nil {
		return errors.New(
			errors.ErrCodeVPPOperation,
			"Address is nil",
			"IP address must not be nil",
			"Provide a valid IP address",
		)
	}

	// Find and remove address
	for i, existing := range iface.Addresses {
		if ipNetEqual(existing, addr) {
			iface.Addresses = append(iface.Addresses[:i], iface.Addresses[i+1:]...)
			return nil
		}
	}

	return errors.New(
		errors.ErrCodeVPPOperation,
		fmt.Sprintf("Address %s not found on interface %d", addr.String(), ifIndex),
		"Address not configured",
		"Address must be configured before it can be deleted",
	)
}

// GetInterface retrieves mock interface information by index
func (m *MockClient) GetInterface(ctx context.Context, ifIndex uint32) (*Interface, error) {
	if m.GetInterfaceError != nil {
		return nil, m.GetInterfaceError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before getting interface information",
		)
	}

	iface, ok := m.interfaces[ifIndex]
	if !ok {
		return nil, errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Interface with index %d not found", ifIndex),
			"Interface does not exist",
			"Interface must exist to retrieve its information",
		)
	}

	// Return a deep copy to prevent external modification
	return deepCopyInterface(iface), nil
}

// ListInterfaces lists all mock VPP interfaces
func (m *MockClient) ListInterfaces(ctx context.Context) ([]*Interface, error) {
	if m.ListInterfacesError != nil {
		return nil, m.ListInterfacesError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before listing interfaces",
		)
	}

	interfaces := make([]*Interface, 0, len(m.interfaces))
	for _, iface := range m.interfaces {
		// Return deep copies to prevent external modification
		interfaces = append(interfaces, deepCopyInterface(iface))
	}

	return interfaces, nil
}

// Reset resets the mock client to its initial state (for testing)
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connected = false
	m.interfaces = make(map[uint32]*Interface)
	m.nextIfIdx = 1

	m.ConnectError = nil
	m.CreateInterfaceError = nil
	m.SetInterfaceUpError = nil
	m.SetInterfaceDownError = nil
	m.SetInterfaceAddressError = nil
	m.DeleteInterfaceAddressError = nil
	m.GetInterfaceError = nil
	m.ListInterfacesError = nil
}
