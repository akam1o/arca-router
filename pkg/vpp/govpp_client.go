package vpp

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/akam1o/arca-router/pkg/vpp/binapi/avf"
	vppif "github.com/akam1o/arca-router/pkg/vpp/binapi/interface"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/interface_types"
	vppip "github.com/akam1o/arca-router/pkg/vpp/binapi/ip"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/ip_types"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/lcp"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/rdma"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/vpe"
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

	// Expected VPP version (major.minor)
	expectedVPPMajor = 24
	expectedVPPMinor = 10
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
		errCh := make(chan error, 1)             // Buffered to prevent goroutine leak

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
				return err
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
	// Call vpe.ShowVersion to get VPP version
	req := &vpe.ShowVersion{}
	reply := &vpe.ShowVersionReply{}

	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to get VPP version (API error): %w", err)
	}

	if reply.Retval != 0 {
		return fmt.Errorf("VPP ShowVersion API returned error code: %d", reply.Retval)
	}

	// Parse version string (expected format: "24.10.x", "v24.10.x", "24.10-rc0", "24.10-rc0~...")
	version := strings.TrimSpace(reply.Version)
	if version == "" {
		return fmt.Errorf("VPP returned empty version string")
	}

	// Remove 'v' prefix if present
	version = strings.TrimPrefix(version, "v")

	// Extract major.minor from version string (handle -rc, ~, and other suffixes)
	// Examples: "24.10.0" -> "24.10", "24.10-rc0" -> "24.10", "24.10-rc0~123-abc" -> "24.10"
	versionCore := version
	if idx := strings.IndexAny(version, "-~"); idx != -1 {
		versionCore = version[:idx]
	}

	// Split version into components
	parts := strings.Split(versionCore, ".")
	if len(parts) < 2 {
		return fmt.Errorf("invalid VPP version format: %s (expected: major.minor[.patch][-suffix])", version)
	}

	// Parse major version
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("invalid VPP major version in '%s': %s", version, parts[0])
	}

	// Parse minor version
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid VPP minor version in '%s': %s", version, parts[1])
	}

	// Check version compatibility (major.minor must match)
	if major != expectedVPPMajor || minor != expectedVPPMinor {
		return fmt.Errorf("VPP version incompatible: got %d.%d, expected %d.%d (full version: %s)",
			major, minor, expectedVPPMajor, expectedVPPMinor, version)
	}

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
	iface, err := c.GetInterface(ctx, uint32(reply.SwIfIndex))
	if err != nil {
		return nil, err
	}

	// Set PCI address from request (AVF uses PCI address directly)
	// Normalize to lowercase for consistent comparison
	pciAddrStr := strings.ToLower(req.DeviceInstance)
	iface.PCIAddress = pciAddrStr

	// Store PCI address in interface tag for reconciliation after restart
	tagErr := c.setInterfaceTag(ctx, uint32(reply.SwIfIndex), fmt.Sprintf("pci=%s", pciAddrStr))
	if tagErr != nil {
		// Not fatal - tag is only for reconciliation
		// Continue with interface creation
		_ = tagErr
	}

	return iface, nil
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
	iface, err := c.GetInterface(ctx, uint32(reply.SwIfIndex))
	if err != nil {
		return nil, err
	}

	// Set PCI address from request if provided
	// For RDMA, the caller should have set this from hardware.yaml
	var pciAddr string
	if req.PCIAddress != "" {
		pciAddr = strings.ToLower(req.PCIAddress)
		iface.PCIAddress = pciAddr
	} else {
		// Fallback: try to get PCI address from sysfs
		// DeviceInstance contains the Linux interface name (e.g., "eth1")
		sysfsAddr, err := getPCIAddressFromSysfs(req.DeviceInstance)
		if err == nil {
			pciAddr = sysfsAddr
			iface.PCIAddress = pciAddr
		}
		// If both methods fail, PCI address remains empty (not fatal)
	}

	// Store PCI address in interface tag for reconciliation after restart
	if pciAddr != "" {
		tagErr := c.setInterfaceTag(ctx, uint32(reply.SwIfIndex), fmt.Sprintf("pci=%s", pciAddr))
		if tagErr != nil {
			// Not fatal - tag is only for reconciliation
			// Continue with interface creation
			_ = tagErr
		}
	}

	return iface, nil
}

// getPCIAddressFromSysfs retrieves PCI address from Linux sysfs for a network interface
func getPCIAddressFromSysfs(ifName string) (string, error) {
	// Read symlink /sys/class/net/<ifname>/device -> ../../../<pci_address>
	devicePath := fmt.Sprintf("/sys/class/net/%s/device", ifName)
	target, err := os.Readlink(devicePath)
	if err != nil {
		return "", fmt.Errorf("failed to read sysfs device link: %w", err)
	}

	// Extract PCI address from path (e.g., "../../../0000:5e:00.0" -> "0000:5e:00.0")
	pciAddr := filepath.Base(target)

	// Normalize PCI address to lowercase for consistent comparison
	pciAddr = strings.ToLower(pciAddr)

	// Validate PCI address format (DDDD:BB:SS.F)
	if !strings.Contains(pciAddr, ":") || !strings.Contains(pciAddr, ".") {
		return "", fmt.Errorf("invalid PCI address format: %s", pciAddr)
	}

	return pciAddr, nil
}

// GetLinuxIfNameFromPCI retrieves Linux interface name from PCI address via sysfs
// This is exported for use in apply.go to resolve RDMA interface names
func GetLinuxIfNameFromPCI(pciAddr string) (string, error) {
	// Normalize PCI address to lowercase for consistent sysfs lookup
	pciAddr = strings.ToLower(pciAddr)

	// List network interfaces in /sys/bus/pci/devices/<pci>/net/
	netPath := fmt.Sprintf("/sys/bus/pci/devices/%s/net", pciAddr)
	entries, err := os.ReadDir(netPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PCI device net directory: %w", err)
	}

	if len(entries) == 0 {
		return "", fmt.Errorf("no network interfaces found for PCI %s", pciAddr)
	}

	// Return the first interface name (usually there's only one)
	return entries[0].Name(), nil
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
		SwIfIndex:  interface_types.InterfaceIndex(ifIndex),
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
		SwIfIndex:  interface_types.InterfaceIndex(^uint32(0)),
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

		iface := convertToInterface(msg)

		// Populate IP addresses for this interface
		addresses, err := c.getInterfaceAddresses(ctx, uint32(msg.SwIfIndex))
		if err != nil {
			// Note: We don't fail the entire operation if IP address dump fails
			// for a single interface. The interface may not have IP addresses,
			// or the context may have been cancelled.
			// We leave Addresses as empty slice and continue.
			// Callers should check context.Err() if they need to detect cancellation.
		} else {
			iface.Addresses = addresses
		}

		interfaces = append(interfaces, iface)
	}

	// Final check for context cancellation after loop completes
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("operation cancelled: %w", err)
	}

	return interfaces, nil
}

// getInterfaceAddresses retrieves IP addresses for a specific interface
func (c *govppClient) getInterfaceAddresses(ctx context.Context, swIfIndex uint32) ([]*net.IPNet, error) {
	if c.ch == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}

	var addresses []*net.IPNet

	// Get IPv4 addresses
	req4 := &vppip.IPAddressDump{
		SwIfIndex: interface_types.InterfaceIndex(swIfIndex),
		IsIPv6:    false,
	}
	reqCtx4 := c.ch.SendMultiRequest(req4)

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("operation cancelled: %w", ctx.Err())
		default:
		}

		msg := &vppip.IPAddressDetails{}
		stop, err := reqCtx4.ReceiveReply(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to receive IPv4 address details: %w", err)
		}
		if stop {
			break
		}

		// Convert VPP AddressWithPrefix to net.IPNet
		ipNet := convertVPPAddressToIPNet(msg.Prefix, false)
		if ipNet != nil {
			addresses = append(addresses, ipNet)
		}
	}

	// Get IPv6 addresses
	req6 := &vppip.IPAddressDump{
		SwIfIndex: interface_types.InterfaceIndex(swIfIndex),
		IsIPv6:    true,
	}
	reqCtx6 := c.ch.SendMultiRequest(req6)

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("operation cancelled: %w", ctx.Err())
		default:
		}

		msg := &vppip.IPAddressDetails{}
		stop, err := reqCtx6.ReceiveReply(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to receive IPv6 address details: %w", err)
		}
		if stop {
			break
		}

		// Convert VPP AddressWithPrefix to net.IPNet
		ipNet := convertVPPAddressToIPNet(msg.Prefix, true)
		if ipNet != nil {
			addresses = append(addresses, ipNet)
		}
	}

	return addresses, nil
}

// convertVPPAddressToIPNet converts VPP AddressWithPrefix to net.IPNet
func convertVPPAddressToIPNet(prefix ip_types.AddressWithPrefix, isIPv6 bool) *net.IPNet {
	var ip net.IP
	if isIPv6 {
		ip6 := prefix.Address.Un.GetIP6()
		ip = net.IP(ip6[:])
	} else {
		ipv4 := prefix.Address.Un.GetIP4()
		ip = net.IPv4(ipv4[0], ipv4[1], ipv4[2], ipv4[3])
	}

	// Create IPNet with prefix length
	mask := net.CIDRMask(int(prefix.Len), func() int {
		if isIPv6 {
			return 128
		}
		return 32
	}())

	return &net.IPNet{
		IP:   ip,
		Mask: mask,
	}
}

// convertToInterface converts VPP SwInterfaceDetails to Interface
func convertToInterface(msg *vppif.SwInterfaceDetails) *Interface {
	adminUp := (msg.Flags & interface_types.IF_STATUS_API_FLAG_ADMIN_UP) != 0
	linkUp := (msg.Flags & interface_types.IF_STATUS_API_FLAG_LINK_UP) != 0

	iface := &Interface{
		SwIfIndex: uint32(msg.SwIfIndex),
		Name:      msg.InterfaceName,
		AdminUp:   adminUp,
		LinkUp:    linkUp,
		MAC:       net.HardwareAddr(msg.L2Address[:]),
		Addresses: nil, // IP addresses will be populated by separate API calls
	}

	// Extract PCI address from interface tag if available (format: "pci=0000:03:00.0")
	if msg.Tag != "" && strings.HasPrefix(msg.Tag, "pci=") {
		iface.PCIAddress = strings.TrimPrefix(msg.Tag, "pci=")
	}

	return iface
}

// setInterfaceTag sets a tag on a VPP interface for metadata storage
func (c *govppClient) setInterfaceTag(ctx context.Context, ifIndex uint32, tag string) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	req := &vppif.SwInterfaceTagAddDel{
		IsAdd:     true,
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		Tag:       tag,
	}

	reply := &vppif.SwInterfaceTagAddDelReply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to set interface tag: %w", err)
	}

	if reply.Retval != 0 {
		return fmt.Errorf("set interface tag returned error code: %d", reply.Retval)
	}

	return nil
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

// GetVersion retrieves VPP version information
func (c *govppClient) GetVersion(ctx context.Context) (string, error) {
	if c.ch == nil {
		return "", fmt.Errorf("not connected to VPP")
	}

	req := &vpe.ShowVersion{}
	reply := &vpe.ShowVersionReply{}

	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return "", fmt.Errorf("failed to get VPP version: %w", err)
	}

	// Check return value
	if reply.Retval != 0 {
		return "", fmt.Errorf("VPP API returned error code: %d", reply.Retval)
	}

	// Check for empty version
	if reply.Version == "" {
		return "", fmt.Errorf("VPP returned empty version string")
	}

	// Format version string
	version := fmt.Sprintf("%s (build: %s)", reply.Version, reply.BuildDate)
	return version, nil
}

// Ensure govppClient implements Client interface
var _ Client = (*govppClient)(nil)
