package vpp

import (
	"context"
	"net"
)

// LCPInterface represents a Linux Control Plane interface pair
type LCPInterface struct {
	// VPPSwIfIndex is the VPP software interface index
	VPPSwIfIndex uint32

	// LinuxIfName is the Linux kernel interface name
	LinuxIfName string

	// JunosName is the original Junos configuration name (for reference)
	// This field is populated by the state manager, not VPP
	JunosName string

	// HostIfType is the type of host interface (TAP or TUN)
	HostIfType string

	// Netns is the network namespace (empty for default namespace)
	Netns string
}

// Client provides an interface for VPP operations
type Client interface {
	// Connect establishes a connection to VPP
	Connect(ctx context.Context) error

	// Close closes the VPP connection
	Close() error

	// CreateInterface creates a new VPP interface
	CreateInterface(ctx context.Context, req *CreateInterfaceRequest) (*Interface, error)

	// SetInterfaceUp sets an interface to admin up state
	SetInterfaceUp(ctx context.Context, ifIndex uint32) error

	// SetInterfaceDown sets an interface to admin down state
	SetInterfaceDown(ctx context.Context, ifIndex uint32) error

	// SetInterfaceAddress adds an IP address to an interface
	SetInterfaceAddress(ctx context.Context, ifIndex uint32, addr *net.IPNet) error

	// DeleteInterfaceAddress removes an IP address from an interface
	DeleteInterfaceAddress(ctx context.Context, ifIndex uint32, addr *net.IPNet) error

	// SetMPLSInterface enables or disables MPLS forwarding on an interface
	SetMPLSInterface(ctx context.Context, ifIndex uint32, enabled bool) error

	// AddIPTable creates an IPv4 or IPv6 FIB table.
	AddIPTable(ctx context.Context, table IPTable) error

	// DeleteIPTable deletes an IPv4 or IPv6 FIB table.
	DeleteIPTable(ctx context.Context, table IPTable) error

	// SetInterfaceTable binds an interface to an IPv4 or IPv6 FIB table.
	SetInterfaceTable(ctx context.Context, ifIndex uint32, tableID uint32, isIPv6 bool) error

	// GetInterfaceTable returns the IPv4 or IPv6 FIB table bound to an interface.
	GetInterfaceTable(ctx context.Context, ifIndex uint32, isIPv6 bool) (uint32, error)

	// GetQoSCapabilities reports VPP class-of-service dataplane capabilities.
	GetQoSCapabilities(ctx context.Context) (QoSCapabilities, error)

	// SetQoSProfile binds output QoS policy intent to an interface.
	SetQoSProfile(ctx context.Context, ifIndex uint32, profile QoSProfile) error

	// ClearQoSProfile removes output QoS policy intent from an interface.
	ClearQoSProfile(ctx context.Context, ifIndex uint32) error

	// AddBridgeDomain creates a VPP L2 bridge domain.
	AddBridgeDomain(ctx context.Context, bridge BridgeDomain) error

	// DeleteBridgeDomain deletes a VPP L2 bridge domain.
	DeleteBridgeDomain(ctx context.Context, bridgeID uint32) error

	// CreateVXLAN creates a VXLAN tunnel interface.
	CreateVXLAN(ctx context.Context, req VXLANRequest) (*Interface, error)

	// DeleteVXLAN deletes a VXLAN tunnel interface.
	DeleteVXLAN(ctx context.Context, req VXLANRequest) error

	// SetInterfaceL2Bridge attaches or detaches an interface to a bridge domain.
	SetInterfaceL2Bridge(ctx context.Context, ifIndex uint32, bridgeID uint32, enable bool) error

	// ListInterfaceCounters returns packet and byte counters by VPP interface index.
	ListInterfaceCounters(ctx context.Context) (map[uint32]InterfaceCounters, error)

	// ListInterfaceQueuePlacements returns RX/TX queue placement by VPP interface index.
	ListInterfaceQueuePlacements(ctx context.Context) (map[uint32]InterfaceQueuePlacements, error)

	// GetInterface retrieves interface information by index
	GetInterface(ctx context.Context, ifIndex uint32) (*Interface, error)

	// ListInterfaces lists all VPP interfaces
	ListInterfaces(ctx context.Context) ([]*Interface, error)

	// CreateLCPInterface creates an LCP pair for an existing VPP interface
	// This makes the VPP interface visible in the Linux kernel
	CreateLCPInterface(ctx context.Context, ifIndex uint32, linuxIfName string) error

	// DeleteLCPInterface removes an LCP pair
	DeleteLCPInterface(ctx context.Context, ifIndex uint32) error

	// GetLCPInterface retrieves LCP pair information by VPP interface index
	GetLCPInterface(ctx context.Context, ifIndex uint32) (*LCPInterface, error)

	// ListLCPInterfaces lists all LCP pairs
	ListLCPInterfaces(ctx context.Context) ([]*LCPInterface, error)

	// GetVersion retrieves VPP version information
	GetVersion(ctx context.Context) (string, error)
}

// CreateInterfaceRequest represents a request to create a VPP interface
type CreateInterfaceRequest struct {
	// Type of interface
	Type InterfaceType

	// DeviceInstance for AVF/RDMA
	// - AVF: PCI address (e.g., "0000:03:00.0")
	// - RDMA: Linux interface name (e.g., "eth1")
	DeviceInstance string

	// PCIAddress is the original PCI address (optional, for reconciliation)
	// This is used to store the PCI address for RDMA interfaces where
	// DeviceInstance is a Linux interface name
	PCIAddress string

	// Name is the interface name (for tap interfaces)
	Name string

	// NumRxQueues is the number of RX queues
	NumRxQueues uint16

	// NumTxQueues is the number of TX queues
	NumTxQueues uint16

	// RxqSize is the RX queue size
	RxqSize uint16

	// TxqSize is the TX queue size
	TxqSize uint16
}

// Interface represents a VPP interface
type Interface struct {
	// SwIfIndex is the software interface index
	SwIfIndex uint32

	// Name is the interface name
	Name string

	// AdminUp indicates if the interface is administratively up
	AdminUp bool

	// LinkUp indicates if the link is up
	LinkUp bool

	// MAC is the MAC address
	MAC net.HardwareAddr

	// Addresses contains the IP addresses assigned to the interface
	Addresses []*net.IPNet

	// PCIAddress is the PCI address (e.g., "0000:00:06.0") for hardware interfaces
	// Empty for non-hardware interfaces (e.g., tap, loopback)
	PCIAddress string

	// QoSProfile is the bound output QoS profile name, if any.
	QoSProfile string
}

// IPTable represents a VPP IPv4 or IPv6 FIB table.
type IPTable struct {
	ID     uint32
	IsIPv6 bool
	Name   string
}

// QoSProfile represents output QoS policy intent for a VPP interface.
type QoSProfile struct {
	Name         string
	ShapingRate  uint64
	SchedulerMap string
	Queues       []QoSQueue
}

// QoSQueue maps an arca forwarding class to a VPP output queue.
type QoSQueue struct {
	ForwardingClass string
	Queue           uint8
}

// QoSCapabilities describes class-of-service dataplane support exposed by VPP.
type QoSCapabilities struct {
	MetadataBinding     bool
	QueueScheduler      bool
	Policer             bool
	OperationalCounters bool
	Diagnostics         []string
}

// BridgeDomain represents a VPP bridge-domain.
type BridgeDomain struct {
	ID      uint32
	Tag     string
	Flood   bool
	UUFlood bool
	Forward bool
	Learn   bool
}

// VXLANRequest represents the parameters for one VXLAN tunnel.
type VXLANRequest struct {
	VNI                     uint32
	SourceAddress           net.IP
	DestinationAddress      net.IP
	MulticastInterfaceIndex uint32
	EncapsulationTable      uint32
	L3                      bool
}

// InterfaceQueuePlacements holds VPP RX/TX queue placement for an interface.
type InterfaceQueuePlacements struct {
	Rx []InterfaceRxQueuePlacement
	Tx []InterfaceTxQueuePlacement
}

// InterfaceRxQueuePlacement maps an RX queue to a VPP worker.
type InterfaceRxQueuePlacement struct {
	QueueID  uint32
	WorkerID uint32
	Mode     string
}

// InterfaceTxQueuePlacement maps a TX queue to VPP worker threads.
type InterfaceTxQueuePlacement struct {
	QueueID uint32
	Shared  bool
	Threads []uint32
}

// InterfaceCounters holds VPP packet, byte, error, and drop counters.
type InterfaceCounters struct {
	RxPackets uint64
	TxPackets uint64
	RxBytes   uint64
	TxBytes   uint64
	RxErrors  uint64
	TxErrors  uint64
	Drops     uint64
}

// InterfaceType represents the type of interface
type InterfaceType string

const (
	// InterfaceTypeAVF is the AVF (Intel DPDK) interface type
	InterfaceTypeAVF InterfaceType = "avf"

	// InterfaceTypeRDMA is the RDMA (Mellanox) interface type
	InterfaceTypeRDMA InterfaceType = "rdma"

	// InterfaceTypeTap is the TAP interface type (for LCP)
	InterfaceTypeTap InterfaceType = "tap"
)
