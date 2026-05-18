package vpp

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/akam1o/arca-router/pkg/vpp/binapi/avf"
	vppif "github.com/akam1o/arca-router/pkg/vpp/binapi/interface"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/interface_types"
	vppip "github.com/akam1o/arca-router/pkg/vpp/binapi/ip"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/ip_types"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/lcp"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/mpls"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/rdma"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/vpe"
	"go.fd.io/govpp/adapter/socketclient"
	"go.fd.io/govpp/adapter/statsclient"
	"go.fd.io/govpp/api"
	govppiftypes "go.fd.io/govpp/binapi/interface_types"
	govppiptypes "go.fd.io/govpp/binapi/ip_types"
	govppl2 "go.fd.io/govpp/binapi/l2"
	govppvxlan "go.fd.io/govpp/binapi/vxlan"
	"go.fd.io/govpp/core"
)

const (
	// Default VPP API socket path
	defaultSocketPath = "/run/vpp/api.sock"

	// Environment override for the VPP stats socket path.
	statsSocketPathEnv = "VPP_STATS_SOCKET_PATH"

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
	socketPath      string
	statsSocketPath string
	conn            *core.Connection
	statsConn       *core.StatsConnection
	ch              api.Channel
}

// NewGovppClient creates a new govpp-based VPP client
func NewGovppClient() Client {
	socketPath := os.Getenv("VPP_API_SOCKET_PATH")
	if socketPath == "" {
		socketPath = defaultSocketPath
	}
	statsSocketPath := os.Getenv(statsSocketPathEnv)
	if statsSocketPath == "" {
		statsSocketPath = statsclient.DefaultSocketName
	}

	return &govppClient{
		socketPath:      socketPath,
		statsSocketPath: statsSocketPath,
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
	if c.statsConn != nil {
		c.statsConn.Disconnect()
		c.statsConn = nil
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

// SetMPLSInterface enables or disables MPLS forwarding on an interface.
func (c *govppClient) SetMPLSInterface(ctx context.Context, ifIndex uint32, enabled bool) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	req := &mpls.SwInterfaceSetMplsEnable{
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		Enable:    enabled,
	}
	reply := &mpls.SwInterfaceSetMplsEnableReply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to set MPLS interface state: %w", err)
	}
	if reply.Retval != 0 {
		return fmt.Errorf("set MPLS interface state returned error code: %d", reply.Retval)
	}
	return nil
}

// AddIPTable creates an IPv4 or IPv6 FIB table.
func (c *govppClient) AddIPTable(ctx context.Context, table IPTable) error {
	return c.setIPTable(ctx, table, true)
}

// DeleteIPTable deletes an IPv4 or IPv6 FIB table.
func (c *govppClient) DeleteIPTable(ctx context.Context, table IPTable) error {
	return c.setIPTable(ctx, table, false)
}

func (c *govppClient) setIPTable(ctx context.Context, table IPTable, add bool) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	req := &vppip.IPTableAddDelV2{
		Table: vppip.IPTable{
			TableID: table.ID,
			IsIP6:   table.IsIPv6,
			Name:    table.Name,
		},
		CreateMfib: true,
		IsAdd:      add,
	}
	reply := &vppip.IPTableAddDelV2Reply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to configure IP table: %w", err)
	}
	if reply.Retval != 0 {
		action := "add"
		if !add {
			action = "delete"
		}
		return fmt.Errorf("%s IP table returned error code: %d", action, reply.Retval)
	}
	return nil
}

// SetInterfaceTable binds an interface to an IPv4 or IPv6 FIB table.
func (c *govppClient) SetInterfaceTable(ctx context.Context, ifIndex uint32, tableID uint32, isIPv6 bool) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	req := &vppif.SwInterfaceSetTable{
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		IsIPv6:    isIPv6,
		VrfID:     tableID,
	}
	reply := &vppif.SwInterfaceSetTableReply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to set interface table: %w", err)
	}
	if reply.Retval != 0 {
		return fmt.Errorf("set interface table returned error code: %d", reply.Retval)
	}
	return nil
}

// GetInterfaceTable returns the IPv4 or IPv6 FIB table bound to an interface.
func (c *govppClient) GetInterfaceTable(ctx context.Context, ifIndex uint32, isIPv6 bool) (uint32, error) {
	if c.ch == nil {
		return 0, fmt.Errorf("not connected to VPP")
	}

	select {
	case <-ctx.Done():
		return 0, fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	req := &vppif.SwInterfaceGetTable{
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
		IsIPv6:    isIPv6,
	}
	reply := &vppif.SwInterfaceGetTableReply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return 0, fmt.Errorf("failed to get interface table: %w", err)
	}
	if reply.Retval != 0 {
		return 0, fmt.Errorf("get interface table returned error code: %d", reply.Retval)
	}
	return reply.VrfID, nil
}

// GetQoSCapabilities reports class-of-service dataplane support for the bundled VPP binapi set.
func (c *govppClient) GetQoSCapabilities(ctx context.Context) (QoSCapabilities, error) {
	if err := ctx.Err(); err != nil {
		return QoSCapabilities{}, err
	}
	return QoSCapabilities{
		MetadataBinding:     true,
		QueueScheduler:      false,
		Policer:             false,
		OperationalCounters: false,
		Diagnostics: []string{
			"VPP 24.10 binapi set does not expose scheduler or policer services; arca stores output QoS intent in interface metadata",
		},
	}, nil
}

// SetQoSProfile binds output QoS policy intent to an interface.
func (c *govppClient) SetQoSProfile(ctx context.Context, ifIndex uint32, profile QoSProfile) error {
	if profile.Name == "" {
		return fmt.Errorf("QoS profile name is required")
	}

	// The bundled VPP 24.10 binapi set does not expose scheduler/policer
	// services, so keep the arca profile binding in the interface tag.
	tag, err := c.interfaceTagWithQoSProfile(ctx, ifIndex, profile.Name)
	if err != nil {
		return err
	}
	if err := c.setInterfaceTag(ctx, ifIndex, tag); err != nil {
		return fmt.Errorf("set QoS profile tag: %w", err)
	}
	return nil
}

// ClearQoSProfile removes output QoS policy intent from an interface.
func (c *govppClient) ClearQoSProfile(ctx context.Context, ifIndex uint32) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	iface, err := c.GetInterface(ctx, ifIndex)
	if err != nil {
		return fmt.Errorf("get interface for QoS profile clear: %w", err)
	}
	if iface.PCIAddress == "" {
		if err := c.clearInterfaceTag(ctx, ifIndex); err != nil {
			return fmt.Errorf("clear QoS profile tag: %w", err)
		}
		return nil
	}

	tag, err := formatInterfaceTag(iface.PCIAddress, "")
	if err != nil {
		return err
	}
	if err := c.setInterfaceTag(ctx, ifIndex, tag); err != nil {
		return fmt.Errorf("clear QoS profile tag: %w", err)
	}
	return nil
}

// AddBridgeDomain creates a VPP bridge domain.
func (c *govppClient) AddBridgeDomain(ctx context.Context, bridge BridgeDomain) error {
	if c.conn == nil {
		return fmt.Errorf("not connected to VPP")
	}
	if bridge.ID == 0 {
		return fmt.Errorf("bridge domain ID cannot be 0")
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	_, err := govppl2.NewServiceClient(c.conn).BridgeDomainAddDelV2(ctx, &govppl2.BridgeDomainAddDelV2{
		BdID:    bridge.ID,
		Flood:   bridge.Flood,
		UuFlood: bridge.UUFlood,
		Forward: bridge.Forward,
		Learn:   bridge.Learn,
		BdTag:   bridge.Tag,
		IsAdd:   true,
	})
	if err != nil {
		return fmt.Errorf("add bridge domain %d: %w", bridge.ID, err)
	}
	return nil
}

// DeleteBridgeDomain deletes a VPP bridge domain.
func (c *govppClient) DeleteBridgeDomain(ctx context.Context, bridgeID uint32) error {
	if c.conn == nil {
		return fmt.Errorf("not connected to VPP")
	}
	if bridgeID == 0 {
		return fmt.Errorf("bridge domain ID cannot be 0")
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	_, err := govppl2.NewServiceClient(c.conn).BridgeDomainAddDelV2(ctx, &govppl2.BridgeDomainAddDelV2{
		BdID:  bridgeID,
		IsAdd: false,
	})
	if err != nil {
		return fmt.Errorf("delete bridge domain %d: %w", bridgeID, err)
	}
	return nil
}

// CreateVXLAN creates a VXLAN tunnel interface.
func (c *govppClient) CreateVXLAN(ctx context.Context, req VXLANRequest) (*Interface, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}
	if err := validateVXLANRequest(req); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	reply, err := govppvxlan.NewServiceClient(c.conn).VxlanAddDelTunnelV3(ctx, &govppvxlan.VxlanAddDelTunnelV3{
		IsAdd:          true,
		Instance:       ^uint32(0),
		SrcAddress:     govppiptypes.NewAddress(req.SourceAddress),
		DstAddress:     govppiptypes.NewAddress(req.DestinationAddress),
		DstPort:        4789,
		McastSwIfIndex: govppiftypes.InterfaceIndex(vxlanMulticastInterfaceIndex(req)),
		EncapVrfID:     req.EncapsulationTable,
		DecapNextIndex: ^uint32(0),
		Vni:            req.VNI,
		IsL3:           req.L3,
	})
	if err != nil {
		return nil, fmt.Errorf("create VXLAN tunnel VNI %d: %w", req.VNI, err)
	}
	return c.GetInterface(ctx, uint32(reply.SwIfIndex))
}

// DeleteVXLAN deletes a VXLAN tunnel interface.
func (c *govppClient) DeleteVXLAN(ctx context.Context, req VXLANRequest) error {
	if c.conn == nil {
		return fmt.Errorf("not connected to VPP")
	}
	if err := validateVXLANRequest(req); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	_, err := govppvxlan.NewServiceClient(c.conn).VxlanAddDelTunnelV3(ctx, &govppvxlan.VxlanAddDelTunnelV3{
		IsAdd:          false,
		Instance:       ^uint32(0),
		SrcAddress:     govppiptypes.NewAddress(req.SourceAddress),
		DstAddress:     govppiptypes.NewAddress(req.DestinationAddress),
		DstPort:        4789,
		McastSwIfIndex: govppiftypes.InterfaceIndex(vxlanMulticastInterfaceIndex(req)),
		EncapVrfID:     req.EncapsulationTable,
		DecapNextIndex: ^uint32(0),
		Vni:            req.VNI,
		IsL3:           req.L3,
	})
	if err != nil {
		return fmt.Errorf("delete VXLAN tunnel VNI %d: %w", req.VNI, err)
	}
	return nil
}

func vxlanMulticastInterfaceIndex(req VXLANRequest) uint32 {
	if req.DestinationAddress != nil && req.DestinationAddress.IsMulticast() {
		return req.MulticastInterfaceIndex
	}
	return ^uint32(0)
}

// SetInterfaceL2Bridge attaches or detaches an interface to a VPP bridge domain.
func (c *govppClient) SetInterfaceL2Bridge(ctx context.Context, ifIndex uint32, bridgeID uint32, enable bool) error {
	if c.conn == nil {
		return fmt.Errorf("not connected to VPP")
	}
	if bridgeID == 0 {
		return fmt.Errorf("bridge domain ID cannot be 0")
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	_, err := govppl2.NewServiceClient(c.conn).SwInterfaceSetL2Bridge(ctx, &govppl2.SwInterfaceSetL2Bridge{
		RxSwIfIndex: govppiftypes.InterfaceIndex(ifIndex),
		BdID:        bridgeID,
		PortType:    govppl2.L2_API_PORT_TYPE_NORMAL,
		Enable:      enable,
	})
	if err != nil {
		action := "attach"
		if !enable {
			action = "detach"
		}
		return fmt.Errorf("%s interface %d to bridge domain %d: %w", action, ifIndex, bridgeID, err)
	}
	return nil
}

func validateVXLANRequest(req VXLANRequest) error {
	if req.VNI == 0 || req.VNI > 16777215 {
		return fmt.Errorf("VXLAN VNI must be between 1 and 16777215, got %d", req.VNI)
	}
	if req.SourceAddress == nil || req.SourceAddress.To16() == nil {
		return fmt.Errorf("VXLAN source address is required")
	}
	if req.DestinationAddress == nil || req.DestinationAddress.To16() == nil {
		return fmt.Errorf("VXLAN destination address is required")
	}
	if (req.SourceAddress.To4() == nil) != (req.DestinationAddress.To4() == nil) {
		return fmt.Errorf("VXLAN source and destination address families must match")
	}
	return nil
}

func (c *govppClient) interfaceTagWithQoSProfile(ctx context.Context, ifIndex uint32, profileName string) (string, error) {
	if c.ch == nil {
		return "", fmt.Errorf("not connected to VPP")
	}

	iface, err := c.GetInterface(ctx, ifIndex)
	if err != nil {
		return "", fmt.Errorf("get interface for QoS profile binding: %w", err)
	}
	return formatInterfaceTag(iface.PCIAddress, profileName)
}

// ListInterfaceCounters returns packet and byte counters by VPP interface index.
func (c *govppClient) ListInterfaceCounters(ctx context.Context) (map[uint32]InterfaceCounters, error) {
	statsConn, err := c.ensureStatsConnection(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("operation cancelled: %w", err)
	}

	stats := &api.InterfaceStats{}
	if err := statsConn.GetInterfaceStats(stats); err != nil {
		c.closeStatsConnection()
		return nil, fmt.Errorf("get VPP interface counters: %w", err)
	}

	counters := make(map[uint32]InterfaceCounters, len(stats.Interfaces))
	for _, iface := range stats.Interfaces {
		counters[iface.InterfaceIndex] = convertInterfaceCounters(iface)
	}
	return counters, nil
}

// ListInterfaceQueuePlacements returns RX/TX queue placement by VPP interface index.
func (c *govppClient) ListInterfaceQueuePlacements(ctx context.Context) (map[uint32]InterfaceQueuePlacements, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("operation cancelled: %w", err)
	}

	svc := vppif.NewServiceClient(c.conn)
	placements := make(map[uint32]InterfaceQueuePlacements)
	if err := c.collectRxQueuePlacements(ctx, svc, placements); err != nil {
		return nil, err
	}
	if err := c.collectTxQueuePlacements(ctx, svc, placements); err != nil {
		return nil, err
	}
	return placements, nil
}

func (c *govppClient) collectRxQueuePlacements(ctx context.Context, svc vppif.RPCService, placements map[uint32]InterfaceQueuePlacements) error {
	stream, err := svc.SwInterfaceRxPlacementDump(ctx, &vppif.SwInterfaceRxPlacementDump{
		SwIfIndex: interface_types.InterfaceIndex(^uint32(0)),
	})
	if err != nil {
		return fmt.Errorf("dump RX queue placements: %w", err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("operation cancelled: %w", err)
		}
		detail, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("receive RX queue placement: %w", err)
		}
		if detail == nil {
			continue
		}
		ifIndex := uint32(detail.SwIfIndex)
		placement := placements[ifIndex]
		placement.Rx = append(placement.Rx, InterfaceRxQueuePlacement{
			QueueID:  detail.QueueID,
			WorkerID: detail.WorkerID,
			Mode:     rxModeName(detail.Mode),
		})
		placements[ifIndex] = placement
	}
}

func (c *govppClient) collectTxQueuePlacements(ctx context.Context, svc vppif.RPCService, placements map[uint32]InterfaceQueuePlacements) error {
	cursor := uint32(0)
	for {
		stream, err := svc.SwInterfaceTxPlacementGet(ctx, &vppif.SwInterfaceTxPlacementGet{
			Cursor:    cursor,
			SwIfIndex: interface_types.InterfaceIndex(^uint32(0)),
		})
		if err != nil {
			return fmt.Errorf("get TX queue placements: %w", err)
		}

		nextCursor := uint32(0)
		for {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("operation cancelled: %w", err)
			}
			detail, reply, err := stream.Recv()
			if err == io.EOF {
				if reply != nil {
					nextCursor = reply.Cursor
				}
				break
			}
			if err != nil {
				return fmt.Errorf("receive TX queue placement: %w", err)
			}
			if detail == nil {
				continue
			}
			ifIndex := uint32(detail.SwIfIndex)
			threads := append([]uint32(nil), detail.Threads...)
			placement := placements[ifIndex]
			placement.Tx = append(placement.Tx, InterfaceTxQueuePlacement{
				QueueID: detail.QueueID,
				Shared:  detail.Shared != 0,
				Threads: threads,
			})
			placements[ifIndex] = placement
		}
		if nextCursor == 0 {
			return nil
		}
		cursor = nextCursor
	}
}

func rxModeName(mode interface_types.RxMode) string {
	switch mode {
	case interface_types.RX_MODE_API_POLLING:
		return "polling"
	case interface_types.RX_MODE_API_INTERRUPT:
		return "interrupt"
	case interface_types.RX_MODE_API_ADAPTIVE:
		return "adaptive"
	case interface_types.RX_MODE_API_DEFAULT:
		return "default"
	default:
		return "unknown"
	}
}

func (c *govppClient) ensureStatsConnection(ctx context.Context) (*core.StatsConnection, error) {
	if c.statsConn != nil {
		return c.statsConn, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("operation cancelled: %w", err)
	}
	statsSocketPath := c.statsSocketPath
	if statsSocketPath == "" {
		statsSocketPath = statsclient.DefaultSocketName
	}

	client := statsclient.NewStatsClient(
		statsSocketPath,
		statsclient.SetSocketRetryTimeout(apiTimeout),
		statsclient.SetSocketRetryPeriod(100*time.Millisecond),
	)
	conn, err := core.ConnectStats(client)
	if err != nil {
		return nil, fmt.Errorf("connect to VPP stats socket %s: %w", statsSocketPath, err)
	}
	c.statsConn = conn
	return conn, nil
}

func (c *govppClient) closeStatsConnection() {
	if c.statsConn != nil {
		c.statsConn.Disconnect()
		c.statsConn = nil
	}
}

func convertInterfaceCounters(iface api.InterfaceCounters) InterfaceCounters {
	return InterfaceCounters{
		RxPackets: iface.Rx.Packets,
		TxPackets: iface.Tx.Packets,
		RxBytes:   iface.Rx.Bytes,
		TxBytes:   iface.Tx.Bytes,
		RxErrors:  iface.RxErrors,
		TxErrors:  iface.TxErrors,
		Drops:     iface.Drops,
	}
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

	// Extract PCI address from interface tag if available.
	fields := parseInterfaceTag(msg.Tag)
	if fields["pci"] != "" {
		iface.PCIAddress = fields["pci"]
	}
	if fields["qos"] != "" {
		iface.QoSProfile = fields["qos"]
	}

	return iface
}

// setInterfaceTag sets a tag on a VPP interface for metadata storage
func (c *govppClient) setInterfaceTag(ctx context.Context, ifIndex uint32, tag string) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
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

func (c *govppClient) clearInterfaceTag(ctx context.Context, ifIndex uint32) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled: %w", ctx.Err())
	default:
	}

	req := &vppif.SwInterfaceTagAddDel{
		IsAdd:     false,
		SwIfIndex: interface_types.InterfaceIndex(ifIndex),
	}

	reply := &vppif.SwInterfaceTagAddDelReply{}
	if err := c.ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("failed to clear interface tag: %w", err)
	}
	if reply.Retval != 0 {
		return fmt.Errorf("clear interface tag returned error code: %d", reply.Retval)
	}
	return nil
}

func formatInterfaceTag(pciAddress, qosProfile string) (string, error) {
	fields := make([]string, 0, 2)
	if pciAddress != "" {
		if err := validateInterfaceTagValue("PCI address", pciAddress); err != nil {
			return "", err
		}
		fields = append(fields, "pci="+pciAddress)
	}
	if qosProfile != "" {
		if err := validateInterfaceTagValue("QoS profile", qosProfile); err != nil {
			return "", err
		}
		fields = append(fields, "qos="+qosProfile)
	}

	tag := strings.Join(fields, ";")
	if len(tag) > 64 {
		return "", fmt.Errorf("interface tag %q exceeds VPP 64 byte limit", tag)
	}
	return tag, nil
}

func validateInterfaceTagValue(field, value string) error {
	if strings.ContainsAny(value, ";=") {
		return fmt.Errorf("%s %q contains unsupported interface tag delimiter", field, value)
	}
	return nil
}

func parseInterfaceTag(tag string) map[string]string {
	fields := make(map[string]string)
	for _, part := range strings.Split(tag, ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok || key == "" {
			continue
		}
		fields[key] = value
	}
	return fields
}

// checkSocketAccess verifies socket accessibility.
func checkSocketAccess(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("not a socket: %s", path)
	}

	if err := checkSocketWritableByProcess(info); err != nil {
		return err
	}

	return nil
}

func checkSocketWritableByProcess(info os.FileInfo) error {
	mode := info.Mode().Perm()
	if mode&0002 != 0 || os.Geteuid() == 0 {
		return nil
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	euid := os.Geteuid()
	if int(stat.Uid) == euid && mode&0200 != 0 {
		return nil
	}

	gids, err := os.Getgroups()
	if err != nil {
		return fmt.Errorf("inspect process groups: %w", err)
	}
	egid := os.Getegid()
	if !containsGroupID(gids, egid) {
		gids = append(gids, egid)
	}
	for _, gid := range gids {
		if int(stat.Gid) == gid && mode&0020 != 0 {
			return nil
		}
	}

	return fmt.Errorf("socket mode=%04o owner uid=%d gid=%d is not writable by uid=%d gids=%v", mode, stat.Uid, stat.Gid, euid, gids)
}

func containsGroupID(groups []int, want int) bool {
	for _, group := range groups {
		if group == want {
			return true
		}
	}
	return false
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
