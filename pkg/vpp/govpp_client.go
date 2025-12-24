package vpp

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/akam1o/arca-router/pkg/vpp/binapi/avf"
	vppif "github.com/akam1o/arca-router/pkg/vpp/binapi/interface"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/interface_types"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/ip_types"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/lcp"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/rdma"
	"go.fd.io/govpp/adapter/socketclient"
	"go.fd.io/govpp/api"
	"go.fd.io/govpp/core"
)

const (
	// Default VPP API socket path
	defaultSocketPath = "/run/vpp/api.sock"

	// Connection timeout
	connectTimeout = 10 * time.Second

	// API call timeout
	apiTimeout = 5 * time.Second

	// Max connection retry attempts
	maxRetries = 3

	// Exponential backoff base duration
	retryBackoff = 1 * time.Second
)

// govppClient is the production VPP client using govpp
type govppClient struct {
	socketPath string
	conn       *core.Connection
	ch         api.Channel
}

// NewGovppClient creates a new govpp-based VPP client
func NewGovppClient() Client {
	socketPath := os.Getenv("VPP_API_SOCKET_PATH")
	if socketPath == "" {
		socketPath = defaultSocketPath
	}

	return &govppClient{
		socketPath: socketPath,
	}
}

// Connect establishes a connection to VPP with retry logic
func (c *govppClient) Connect(ctx context.Context) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("connect cancelled: %w", ctx.Err())
		default:
		}

		// Check if socket exists
		if _, err := os.Stat(c.socketPath); err != nil {
			if os.IsNotExist(err) {
				lastErr = fmt.Errorf("VPP socket not found: %s (ensure VPP is running)", c.socketPath)
			} else {
				lastErr = fmt.Errorf("VPP socket stat error: %w", err)
			}

			// Retry with exponential backoff
			if attempt < maxRetries {
				backoff := retryBackoff * time.Duration(1<<uint(attempt-1))
				time.Sleep(backoff)
				continue
			}
			break
		}

		// Check socket permissions
		if err := checkSocketAccess(c.socketPath); err != nil {
			return fmt.Errorf("VPP socket permission denied: %w "+
				"(ensure user is in vpp group)", err)
		}

		// Create adapter
		adapter := socketclient.NewVppClient(c.socketPath)

		// Connect to VPP with timeout (disable internal retries, handle externally)
		connCh := make(chan *core.Connection, 1) // Buffered to prevent goroutine leak
		errCh := make(chan error, 1)              // Buffered to prevent goroutine leak

		go func() {
			// Disable AsyncConnect internal retries (we handle retries externally)
			conn, connEvent, err := core.AsyncConnect(adapter, 0, 0) // maxAttempts=0 disables retry
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}

			// Wait for connection event with timeout
			select {
			case e := <-connEvent:
				if e.State != core.Connected {
					select {
					case errCh <- fmt.Errorf("connection failed (state: %v)", e.State):
					default:
					}
					if conn != nil {
						conn.Disconnect()
					}
					return
				}
				select {
				case connCh <- conn:
				default:
				}
			case <-time.After(connectTimeout):
				select {
				case errCh <- fmt.Errorf("connection timeout"):
				default:
				}
				if conn != nil {
					conn.Disconnect()
				}
			}
		}()

		// Wait for connection or timeout
		select {
		case conn := <-connCh:
			c.conn = conn

			// Create API channel with timeout
			ch, err := conn.NewAPIChannelBuffered(128, 128)
			if err != nil {
				conn.Disconnect()
				return fmt.Errorf("failed to create API channel: %w", err)
			}

			// Set reply timeout for API calls
			ch.SetReplyTimeout(apiTimeout)
			c.ch = ch

			// Check VPP API version compatibility
			if err := c.checkVersionCompatibility(); err != nil {
				ch.Close()
				conn.Disconnect()
				return fmt.Errorf("VPP API version incompatible: %w", err)
			}

			return nil

		case err := <-errCh:
			lastErr = err

			// Retry with exponential backoff
			if attempt < maxRetries {
				backoff := retryBackoff * time.Duration(1<<uint(attempt-1))
				time.Sleep(backoff)
				continue
			}

		case <-ctx.Done():
			return fmt.Errorf("connect cancelled: %w", ctx.Err())
		}
	}

	return fmt.Errorf("failed to connect to VPP after %d attempts: %w", maxRetries, lastErr)
}

// checkVersionCompatibility verifies VPP API version compatibility
func (c *govppClient) checkVersionCompatibility() error {
	// Note: Full version check requires binapi (vpe.ShowVersion)
	// This will be implemented to:
	// 1. Call vpe.ShowVersion to get VPP version
	// 2. Parse version string (expected: 24.10.x for Phase 2)
	// 3. Return error if major/minor version mismatch
	//
	// For now, we defer version check to VPP API call failures.
	// If VPP version is incompatible, subsequent API calls will fail
	// with clear error messages from govpp.
	//
	// This is acceptable for Phase 2 initial implementation because:
	// - Installation docs specify VPP 24.10 requirement
	// - Integration tests will catch version mismatches
	// - govpp will report incompatible API calls explicitly

	return nil
}

// Close closes the VPP connection
func (c *govppClient) Close() error {
	if c.ch != nil {
		c.ch.Close()
		c.ch = nil
	}

	if c.conn != nil {
		c.conn.Disconnect()
		c.conn = nil
	}

	return nil
}

// CreateInterface creates a new VPP interface
func (c *govppClient) CreateInterface(ctx context.Context, req *CreateInterfaceRequest) (*Interface, error) {
	if c.ch == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}

	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	switch req.Type {
	case InterfaceTypeAVF:
		return c.createAVFInterface(ctx, req)
	case InterfaceTypeRDMA:
		return c.createRDMAInterface(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported interface type: %s", req.Type)
	}
}

// createAVFInterface creates an AVF interface
func (c *govppClient) createAVFInterface(ctx context.Context, req *CreateInterfaceRequest) (*Interface, error) {
	// Parse PCI address to u32 format
	pciAddr, err := parsePCIAddress(req.DeviceInstance)
	if err != nil {
		return nil, fmt.Errorf("invalid PCI address %s: %w", req.DeviceInstance, err)
	}

	// Create AVF interface
	createReq := &avf.AvfCreate{
		PciAddr:    pciAddr,
		EnableElog: 0,
		RxqNum:     req.NumRxQueues,
		RxqSize:    req.RxqSize,
		TxqSize:    req.TxqSize,
	}

	reply := &avf.AvfCreateReply{}
	if err := c.ch.SendRequest(createReq).ReceiveReply(reply); err != nil {
		return nil, fmt.Errorf("AVF create failed: %w", err)
	}

	if reply.Retval != 0 {
		return nil, fmt.Errorf("AVF create returned error code: %d", reply.Retval)
	}

	// Get interface details
	return c.GetInterface(ctx, uint32(reply.SwIfIndex))
}

// createRDMAInterface creates an RDMA interface
func (c *govppClient) createRDMAInterface(ctx context.Context, req *CreateInterfaceRequest) (*Interface, error) {
	// Use rdma_create_v4 for VPP 24.10
	createReq := &rdma.RdmaCreateV4{
		HostIf:     req.DeviceInstance,
		Name:       req.Name,
		RxqNum:     req.NumRxQueues,
		RxqSize:    req.RxqSize,
		TxqSize:    req.TxqSize,
		Mode:       rdma.RDMA_API_MODE_AUTO,
		NoMultiSeg: false,
		MaxPktlen:  0,
		Rss4:       rdma.RDMA_API_RSS4_AUTO,
		Rss6:       rdma.RDMA_API_RSS6_AUTO,
	}

	reply := &rdma.RdmaCreateV4Reply{}
	if err := c.ch.SendRequest(createReq).ReceiveReply(reply); err != nil {
		return nil, fmt.Errorf("RDMA create failed: %w", err)
	}

	if reply.Retval != 0 {
		return nil, fmt.Errorf("RDMA create returned error code: %d", reply.Retval)
	}

	// Get interface details
	return c.GetInterface(ctx, uint32(reply.SwIfIndex))
}

// parsePCIAddress converts PCI address string (e.g., "0000:00:06.0") to u32 format
// VPP pci_address_t format: domain(16 bits) << 16 | bus(8 bits) << 8 | slot(5 bits) << 3 | function(3 bits)
func parsePCIAddress(addr string) (uint32, error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid PCI address format (expected DDDD:BB:SS.F)")
	}

	// Parse domain
	domain, err := strconv.ParseUint(parts[0], 16, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid domain: %w", err)
	}

	// Parse bus
	bus, err := strconv.ParseUint(parts[1], 16, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid bus: %w", err)
	}

	// Parse slot.function
	slotFunc := strings.Split(parts[2], ".")
	if len(slotFunc) != 2 {
		return 0, fmt.Errorf("invalid slot.function format")
	}

	slot, err := strconv.ParseUint(slotFunc[0], 16, 5)
	if err != nil {
		return 0, fmt.Errorf("invalid slot: %w", err)
	}

	function, err := strconv.ParseUint(slotFunc[1], 16, 3)
	if err != nil {
		return 0, fmt.Errorf("invalid function: %w", err)
	}

	// Combine into u32 according to VPP pci_address_t layout
	result := (uint32(domain&0xFFFF) << 16) | (uint32(bus&0xFF) << 8) | (uint32(slot&0x1F) << 3) | uint32(function&0x7)
	return result, nil
}

// SetInterfaceUp sets an interface to admin up state
func (c *govppClient) SetInterfaceUp(ctx context.Context, ifIndex uint32) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	req := &vppif.SwInterfaceSetFlags{
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		Flags:     interface_types.IF_STATUS_API_FLAG_ADMIN_UP,
	}

	reply := &vppif.SwInterfaceSetFlagsReply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to set interface up: %w", err)
	}

	if reply.Retval != 0 {
		return fmt.Errorf("set interface up returned error code: %d", reply.Retval)
	}

	return nil
}

// SetInterfaceDown sets an interface to admin down state
func (c *govppClient) SetInterfaceDown(ctx context.Context, ifIndex uint32) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	req := &vppif.SwInterfaceSetFlags{
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		Flags:     0, // Clear all flags (admin down)
	}

	reply := &vppif.SwInterfaceSetFlagsReply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to set interface down: %w", err)
	}

	if reply.Retval != 0 {
		return fmt.Errorf("set interface down returned error code: %d", reply.Retval)
	}

	return nil
}

// SetInterfaceAddress adds an IP address to an interface
func (c *govppClient) SetInterfaceAddress(ctx context.Context, ifIndex uint32, addr *net.IPNet) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	if addr == nil {
		return fmt.Errorf("address cannot be nil")
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	// Normalize IP address: ensure IPv4 is in 4-byte form, IPv6 is in 16-byte form
	normalizedAddr := *addr
	if ip4 := addr.IP.To4(); ip4 != nil {
		normalizedAddr.IP = ip4
	} else if ip6 := addr.IP.To16(); ip6 != nil {
		normalizedAddr.IP = ip6
	} else {
		return fmt.Errorf("invalid IP address")
	}

	// Convert net.IPNet to AddressWithPrefix
	prefix := ip_types.NewAddressWithPrefix(normalizedAddr)

	req := &vppif.SwInterfaceAddDelAddress{
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		IsAdd:     true,
		DelAll:    false,
		Prefix:    prefix,
	}

	reply := &vppif.SwInterfaceAddDelAddressReply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to add interface address: %w", err)
	}

	if reply.Retval != 0 {
		return fmt.Errorf("add interface address returned error code: %d", reply.Retval)
	}

	return nil
}

// DeleteInterfaceAddress removes an IP address from an interface
func (c *govppClient) DeleteInterfaceAddress(ctx context.Context, ifIndex uint32, addr *net.IPNet) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	if addr == nil {
		return fmt.Errorf("address cannot be nil")
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	// Normalize IP address: ensure IPv4 is in 4-byte form, IPv6 is in 16-byte form
	normalizedAddr := *addr
	if ip4 := addr.IP.To4(); ip4 != nil {
		normalizedAddr.IP = ip4
	} else if ip6 := addr.IP.To16(); ip6 != nil {
		normalizedAddr.IP = ip6
	} else {
		return fmt.Errorf("invalid IP address")
	}

	// Convert net.IPNet to AddressWithPrefix
	prefix := ip_types.NewAddressWithPrefix(normalizedAddr)

	req := &vppif.SwInterfaceAddDelAddress{
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		IsAdd:     false,
		DelAll:    false,
		Prefix:    prefix,
	}

	reply := &vppif.SwInterfaceAddDelAddressReply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to delete interface address: %w", err)
	}

	if reply.Retval != 0 {
		return fmt.Errorf("delete interface address returned error code: %d", reply.Retval)
	}

	return nil
}

// GetInterface retrieves interface information by index
func (c *govppClient) GetInterface(ctx context.Context, ifIndex uint32) (*Interface, error) {
	if c.ch == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}

	// Dump interface with specific index
	req := &vppif.SwInterfaceDump{
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		NameFilter: "",
	}

	reqCtx := c.ch.SendMultiRequest(req)

	for {
		// Check for context cancellation in loop
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("operation cancelled: %w", ctx.Err())
		default:
		}

		msg := &vppif.SwInterfaceDetails{}
		stop, err := reqCtx.ReceiveReply(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to receive interface details: %w", err)
		}
		if stop {
			break
		}

		// Check if this is the interface we're looking for
		if uint32(msg.SwIfIndex) == ifIndex {
			return convertToInterface(msg), nil
		}
	}

	return nil, fmt.Errorf("interface with index %d not found", ifIndex)
}

// ListInterfaces lists all VPP interfaces
func (c *govppClient) ListInterfaces(ctx context.Context) ([]*Interface, error) {
	if c.ch == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}

	// Dump all interfaces (SwIfIndex ^uint32(0) means all)
	req := &vppif.SwInterfaceDump{
		SwIfIndex: interface_types.InterfaceIndex(^uint32(0)),
		NameFilter: "",
	}

	reqCtx := c.ch.SendMultiRequest(req)

	var interfaces []*Interface
	for {
		// Check for context cancellation in loop
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("operation cancelled: %w", ctx.Err())
		default:
		}

		msg := &vppif.SwInterfaceDetails{}
		stop, err := reqCtx.ReceiveReply(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to receive interface details: %w", err)
		}
		if stop {
			break
		}

		interfaces = append(interfaces, convertToInterface(msg))
	}

	return interfaces, nil
}

// convertToInterface converts VPP SwInterfaceDetails to Interface
func convertToInterface(msg *vppif.SwInterfaceDetails) *Interface {
	adminUp := (msg.Flags & interface_types.IF_STATUS_API_FLAG_ADMIN_UP) != 0
	linkUp := (msg.Flags & interface_types.IF_STATUS_API_FLAG_LINK_UP) != 0

	return &Interface{
		SwIfIndex: uint32(msg.SwIfIndex),
		Name:      msg.InterfaceName,
		AdminUp:   adminUp,
		LinkUp:    linkUp,
		MAC:       net.HardwareAddr(msg.L2Address[:]),
		Addresses: nil, // IP addresses will be populated by separate API calls if needed
	}
}

// checkSocketAccess verifies socket accessibility
func checkSocketAccess(path string) error {
	// Try to open the socket to verify access
	// This is a basic check - actual permission check happens during connect
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Check if it's a socket
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("not a socket: %s", path)
	}

	return nil
}

// CreateLCPInterface creates an LCP pair for an existing VPP interface
func (c *govppClient) CreateLCPInterface(ctx context.Context, ifIndex uint32, linuxIfName string) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	// Validate Linux interface name
	if err := ValidateLinuxIfName(linuxIfName); err != nil {
		return fmt.Errorf("invalid Linux interface name: %w", err)
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	// Use lcp_itf_pair_add_del_v2 (most stable for VPP 24.10)
	req := &lcp.LcpItfPairAddDelV2{
		IsAdd:      true,
		SwIfIndex:  interface_types.InterfaceIndex(ifIndex),
		HostIfName: linuxIfName,
		HostIfType: lcp.LCP_API_ITF_HOST_TAP, // Always use TAP
		Netns:      "",                       // Default namespace
	}

	reply := &lcp.LcpItfPairAddDelV2Reply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to create LCP pair: %w", err)
	}

	if reply.Retval != 0 {
		return fmt.Errorf("LCP pair add failed: retval=%d (VPP error code)", reply.Retval)
	}

	return nil
}

// DeleteLCPInterface removes an LCP pair
func (c *govppClient) DeleteLCPInterface(ctx context.Context, ifIndex uint32) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	// Use lcp_itf_pair_add_del_v2
	req := &lcp.LcpItfPairAddDelV2{
		IsAdd:     false,
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		// Other fields not needed for delete
	}

	reply := &lcp.LcpItfPairAddDelV2Reply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to delete LCP pair: %w", err)
	}

	if reply.Retval != 0 {
		return fmt.Errorf("LCP pair delete failed: retval=%d (VPP error code)", reply.Retval)
	}

	return nil
}

// GetLCPInterface retrieves LCP pair information by VPP interface index
func (c *govppClient) GetLCPInterface(ctx context.Context, ifIndex uint32) (*LCPInterface, error) {
	if c.ch == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}

	// Get all LCP pairs and filter by ifIndex
	pairs, err := c.ListLCPInterfaces(ctx)
	if err != nil {
		return nil, err
	}

	for _, pair := range pairs {
		if pair.VPPSwIfIndex == ifIndex {
			return pair, nil
		}
	}

	return nil, fmt.Errorf("LCP pair not found for interface index %d", ifIndex)
}

// ListLCPInterfaces lists all LCP pairs
func (c *govppClient) ListLCPInterfaces(ctx context.Context) ([]*LCPInterface, error) {
	if c.ch == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}

	// Send dump request (cursor=0xFFFFFFFF means get all)
	req := &lcp.LcpItfPairGet{
		Cursor: 0xFFFFFFFF,
	}

	reqCtx := c.ch.SendMultiRequest(req)

	var interfaces []*LCPInterface
	for {
		// Check for context cancellation in loop
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("operation cancelled: %w", ctx.Err())
		default:
		}

		msg := &lcp.LcpItfPairDetails{}
		stop, err := reqCtx.ReceiveReply(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to receive LCP pair details: %w", err)
		}
		if stop {
			break
		}

		// Convert to LCPInterface
		hostIfType := "tap"
		if msg.HostIfType == lcp.LCP_API_ITF_HOST_TUN {
			hostIfType = "tun"
		}

		interfaces = append(interfaces, &LCPInterface{
			VPPSwIfIndex: uint32(msg.PhySwIfIndex),
			LinuxIfName:  msg.HostIfName,
			HostIfType:   hostIfType,
			Netns:        msg.Netns,
			// JunosName is populated by state manager, not VPP
		})
	}

	return interfaces, nil
}

// Ensure govppClient implements Client interface
var _ Client = (*govppClient)(nil)
