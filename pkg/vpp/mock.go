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
	mu             sync.RWMutex
	connected      bool
	interfaces     map[uint32]*Interface
	lcpInterfaces  map[uint32]*LCPInterface
	mplsInterfaces map[uint32]bool
	ipTables       map[ipTableKey]IPTable
	interfaceTable map[interfaceTableKey]uint32
	qosProfiles    map[uint32]QoSProfile
	nextIfIdx      uint32

	// Hooks for testing error scenarios
	ConnectError                error
	CreateInterfaceError        error
	SetInterfaceUpError         error
	SetInterfaceDownError       error
	SetInterfaceAddressError    error
	DeleteInterfaceAddressError error
	SetMPLSInterfaceError       error
	AddIPTableError             error
	DeleteIPTableError          error
	SetInterfaceTableError      error
	SetQoSProfileError          error
	ClearQoSProfileError        error
	GetInterfaceError           error
	ListInterfacesError         error
	CreateLCPInterfaceError     error
	DeleteLCPInterfaceError     error
	GetLCPInterfaceError        error
	ListLCPInterfacesError      error
}

// NewMockClient creates a new mock VPP client
func NewMockClient() *MockClient {
	return &MockClient{
		interfaces:     make(map[uint32]*Interface),
		lcpInterfaces:  make(map[uint32]*LCPInterface),
		mplsInterfaces: make(map[uint32]bool),
		ipTables:       make(map[ipTableKey]IPTable),
		interfaceTable: make(map[interfaceTableKey]uint32),
		qosProfiles:    make(map[uint32]QoSProfile),
		nextIfIdx:      1, // Start from 1 (0 is reserved for local0)
	}
}

type ipTableKey struct {
	id     uint32
	isIPv6 bool
}

type interfaceTableKey struct {
	ifIndex uint32
	isIPv6  bool
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
	copy(dst, src)
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

// SetMPLSInterface enables or disables MPLS forwarding on a mock interface.
func (m *MockClient) SetMPLSInterface(ctx context.Context, ifIndex uint32, enabled bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.SetMPLSInterfaceError != nil {
		return m.SetMPLSInterfaceError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before setting MPLS interface state",
		)
	}
	if _, ok := m.interfaces[ifIndex]; !ok {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Interface with index %d not found", ifIndex),
			"Interface does not exist",
			"Create the interface before setting MPLS interface state",
		)
	}

	if enabled {
		m.mplsInterfaces[ifIndex] = true
		return nil
	}
	delete(m.mplsInterfaces, ifIndex)
	return nil
}

// MPLSInterfaceEnabled reports whether MPLS is enabled on a mock interface.
func (m *MockClient) MPLSInterfaceEnabled(ifIndex uint32) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mplsInterfaces[ifIndex]
}

// AddIPTable creates an IPv4 or IPv6 FIB table in the mock state.
func (m *MockClient) AddIPTable(ctx context.Context, table IPTable) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.AddIPTableError != nil {
		return m.AddIPTableError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before adding IP tables",
		)
	}
	if table.ID == 0 {
		return errors.New(
			errors.ErrCodeVPPOperation,
			"IP table ID 0 is reserved",
			"Routing instances must use a non-default table",
			"Use a non-zero table ID",
		)
	}

	m.ipTables[ipTableKey{id: table.ID, isIPv6: table.IsIPv6}] = table
	return nil
}

// DeleteIPTable deletes an IPv4 or IPv6 FIB table from the mock state.
func (m *MockClient) DeleteIPTable(ctx context.Context, table IPTable) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.DeleteIPTableError != nil {
		return m.DeleteIPTableError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before deleting IP tables",
		)
	}

	delete(m.ipTables, ipTableKey{id: table.ID, isIPv6: table.IsIPv6})
	return nil
}

// SetInterfaceTable binds an interface to an IPv4 or IPv6 FIB table in the mock state.
func (m *MockClient) SetInterfaceTable(ctx context.Context, ifIndex uint32, tableID uint32, isIPv6 bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.SetInterfaceTableError != nil {
		return m.SetInterfaceTableError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before setting interface tables",
		)
	}
	if _, ok := m.interfaces[ifIndex]; !ok {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Interface with index %d not found", ifIndex),
			"Interface does not exist",
			"Create the interface before setting its table",
		)
	}
	if tableID != 0 {
		if _, ok := m.ipTables[ipTableKey{id: tableID, isIPv6: isIPv6}]; !ok {
			return errors.New(
				errors.ErrCodeVPPOperation,
				fmt.Sprintf("IP table %d not found", tableID),
				"IP table does not exist",
				"Create the IP table before binding an interface",
			)
		}
	}

	m.interfaceTable[interfaceTableKey{ifIndex: ifIndex, isIPv6: isIPv6}] = tableID
	return nil
}

// IPTableExists reports whether a mock IP table exists.
func (m *MockClient) IPTableExists(tableID uint32, isIPv6 bool) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.ipTables[ipTableKey{id: tableID, isIPv6: isIPv6}]
	return ok
}

// InterfaceTableID returns the table bound to a mock interface.
func (m *MockClient) InterfaceTableID(ifIndex uint32, isIPv6 bool) uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.interfaceTable[interfaceTableKey{ifIndex: ifIndex, isIPv6: isIPv6}]
}

// SetQoSProfile binds output QoS policy intent to a mock interface.
func (m *MockClient) SetQoSProfile(ctx context.Context, ifIndex uint32, profile QoSProfile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.SetQoSProfileError != nil {
		return m.SetQoSProfileError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before setting QoS profiles",
		)
	}
	if _, ok := m.interfaces[ifIndex]; !ok {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Interface with index %d not found", ifIndex),
			"Interface does not exist",
			"Create the interface before setting its QoS profile",
		)
	}

	m.qosProfiles[ifIndex] = cloneQoSProfile(profile)
	return nil
}

// ClearQoSProfile removes output QoS policy intent from a mock interface.
func (m *MockClient) ClearQoSProfile(ctx context.Context, ifIndex uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.ClearQoSProfileError != nil {
		return m.ClearQoSProfileError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before clearing QoS profiles",
		)
	}
	if _, ok := m.interfaces[ifIndex]; !ok {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("Interface with index %d not found", ifIndex),
			"Interface does not exist",
			"Create the interface before clearing its QoS profile",
		)
	}

	delete(m.qosProfiles, ifIndex)
	return nil
}

// QoSProfile returns the mock QoS profile bound to an interface.
func (m *MockClient) QoSProfile(ifIndex uint32) (QoSProfile, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	profile, ok := m.qosProfiles[ifIndex]
	return cloneQoSProfile(profile), ok
}

func cloneQoSProfile(profile QoSProfile) QoSProfile {
	profile.Queues = append([]QoSQueue(nil), profile.Queues...)
	return profile
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
	m.lcpInterfaces = make(map[uint32]*LCPInterface)
	m.mplsInterfaces = make(map[uint32]bool)
	m.ipTables = make(map[ipTableKey]IPTable)
	m.interfaceTable = make(map[interfaceTableKey]uint32)
	m.qosProfiles = make(map[uint32]QoSProfile)
	m.nextIfIdx = 1

	m.ConnectError = nil
	m.CreateInterfaceError = nil
	m.SetInterfaceUpError = nil
	m.SetInterfaceDownError = nil
	m.SetInterfaceAddressError = nil
	m.DeleteInterfaceAddressError = nil
	m.SetMPLSInterfaceError = nil
	m.AddIPTableError = nil
	m.DeleteIPTableError = nil
	m.SetInterfaceTableError = nil
	m.SetQoSProfileError = nil
	m.ClearQoSProfileError = nil
	m.GetInterfaceError = nil
	m.ListInterfacesError = nil
	m.CreateLCPInterfaceError = nil
	m.DeleteLCPInterfaceError = nil
	m.GetLCPInterfaceError = nil
	m.ListLCPInterfacesError = nil
}

// CreateLCPInterface creates a mock LCP interface pair
func (m *MockClient) CreateLCPInterface(ctx context.Context, ifIndex uint32, linuxIfName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if m.CreateLCPInterfaceError != nil {
		return m.CreateLCPInterfaceError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before creating LCP interfaces",
		)
	}

	// Check if VPP interface exists
	if _, ok := m.interfaces[ifIndex]; !ok {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("VPP interface with index %d not found", ifIndex),
			"VPP interface does not exist",
			"Create the VPP interface before creating LCP pair",
		)
	}

	// Check if LCP pair already exists
	if _, exists := m.lcpInterfaces[ifIndex]; exists {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("LCP pair already exists for interface %d", ifIndex),
			"LCP pair already configured",
			"Delete the existing LCP pair before creating a new one",
		)
	}

	// Validate Linux interface name
	if err := ValidateLinuxIfName(linuxIfName); err != nil {
		return err
	}

	// Create LCP interface
	lcp := &LCPInterface{
		VPPSwIfIndex: ifIndex,
		LinuxIfName:  linuxIfName,
		HostIfType:   "tap",
		Netns:        "",
	}

	m.lcpInterfaces[ifIndex] = lcp
	return nil
}

// DeleteLCPInterface removes a mock LCP interface pair
func (m *MockClient) DeleteLCPInterface(ctx context.Context, ifIndex uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if m.DeleteLCPInterfaceError != nil {
		return m.DeleteLCPInterfaceError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before deleting LCP interfaces",
		)
	}

	if _, exists := m.lcpInterfaces[ifIndex]; !exists {
		return errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("LCP pair not found for interface %d", ifIndex),
			"LCP pair does not exist",
			"LCP pair must exist to be deleted",
		)
	}

	delete(m.lcpInterfaces, ifIndex)
	return nil
}

// GetLCPInterface retrieves mock LCP interface information by VPP interface index
func (m *MockClient) GetLCPInterface(ctx context.Context, ifIndex uint32) (*LCPInterface, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if m.GetLCPInterfaceError != nil {
		return nil, m.GetLCPInterfaceError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before getting LCP interface information",
		)
	}

	lcp, ok := m.lcpInterfaces[ifIndex]
	if !ok {
		return nil, errors.New(
			errors.ErrCodeVPPOperation,
			fmt.Sprintf("LCP pair not found for interface %d", ifIndex),
			"LCP pair does not exist",
			"LCP pair must exist to retrieve its information",
		)
	}

	// Return a copy
	return &LCPInterface{
		VPPSwIfIndex: lcp.VPPSwIfIndex,
		LinuxIfName:  lcp.LinuxIfName,
		JunosName:    lcp.JunosName,
		HostIfType:   lcp.HostIfType,
		Netns:        lcp.Netns,
	}, nil
}

// ListLCPInterfaces lists all mock LCP interface pairs
func (m *MockClient) ListLCPInterfaces(ctx context.Context) ([]*LCPInterface, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if m.ListLCPInterfacesError != nil {
		return nil, m.ListLCPInterfacesError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before listing LCP interfaces",
		)
	}

	interfaces := make([]*LCPInterface, 0, len(m.lcpInterfaces))
	for _, lcp := range m.lcpInterfaces {
		// Return copies
		interfaces = append(interfaces, &LCPInterface{
			VPPSwIfIndex: lcp.VPPSwIfIndex,
			LinuxIfName:  lcp.LinuxIfName,
			JunosName:    lcp.JunosName,
			HostIfType:   lcp.HostIfType,
			Netns:        lcp.Netns,
		})
	}

	return interfaces, nil
}

// GetVersion retrieves VPP version information (mock)
func (m *MockClient) GetVersion(ctx context.Context) (string, error) {
	if !m.connected {
		return "", errors.New(
			errors.ErrCodeVPPConnection,
			"Not connected to VPP",
			"VPP connection not established",
			"Connect to VPP before getting version",
		)
	}

	// Return mock VPP version
	return "24.10-mock (build: 2024-01-01T00:00:00)", nil
}
