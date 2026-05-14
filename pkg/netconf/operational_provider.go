package netconf

import (
	"context"
	"time"
)

// OperationalStateProvider supplies live state for NETCONF <get> replies.
type OperationalStateProvider interface {
	// InterfaceStates returns interface state keyed by management-plane interface name.
	InterfaceStates(ctx context.Context) (map[string]*InterfaceOperationalState, error)
	// Routes returns live route table entries.
	Routes(ctx context.Context) ([]RouteOperationalState, error)
	// BGPNeighbors returns live BGP neighbor state.
	BGPNeighbors(ctx context.Context) ([]BGPNeighborOperationalState, error)
	// BFDStatus returns cached BFD protocol operational state.
	BFDStatus(ctx context.Context) (*BFDOperationalState, error)
}

// InterfaceOperationalState is a transport-neutral interface state snapshot.
type InterfaceOperationalState struct {
	Name        string
	AdminStatus string
	OperStatus  string
	MAC         string
	QoSProfile  string
	IPv4TableID uint32
	IPv6TableID uint32
	Counters    *InterfaceOperationalCounters
	Queues      *InterfaceOperationalQueues
}

// InterfaceOperationalCounters holds live interface counters.
type InterfaceOperationalCounters struct {
	RxPackets uint64
	TxPackets uint64
	RxBytes   uint64
	TxBytes   uint64
	RxErrors  uint64
	TxErrors  uint64
	Drops     uint64
}

// InterfaceOperationalQueues holds RX/TX queue placement for an interface.
type InterfaceOperationalQueues struct {
	Rx []InterfaceOperationalRxQueue
	Tx []InterfaceOperationalTxQueue
}

// RoutingInstanceOperationalState describes one routing-instance table mapping.
type RoutingInstanceOperationalState struct {
	Name               string
	InstanceType       string
	RouteDistinguisher string
	IPv4TableID        uint32
	IPv6TableID        uint32
	ImportTargets      []string
	ExportTargets      []string
	ImportPolicies     []string
	ExportPolicies     []string
	Interfaces         []string
}

// RouteOperationalState describes one live route entry or nexthop path.
type RouteOperationalState struct {
	Prefix    string
	NextHop   string
	Protocol  string
	Metric    uint32
	Interface string
	Active    bool
}

// BGPNeighborOperationalState describes one BGP peer in operational output.
type BGPNeighborOperationalState struct {
	PeerAddress    string
	PeerAS         uint32
	State          string
	UptimeSecs     uint64
	PrefixReceived uint32
	PrefixSent     uint32
}

// InterfaceOperationalRxQueue maps an RX queue to a VPP worker.
type InterfaceOperationalRxQueue struct {
	QueueID  uint32
	WorkerID uint32
	Mode     string
}

// InterfaceOperationalTxQueue maps a TX queue to VPP worker threads.
type InterfaceOperationalTxQueue struct {
	QueueID uint32
	Shared  bool
	Threads []uint32
}

// BFDOperationalState holds cached BFD convergence and failure counters.
type BFDOperationalState struct {
	LastRun           time.Time
	ConfiguredPeers   int
	ObservedPeers     int
	UpPeers           int
	DownPeers         int
	SessionDownEvents uint64
	RxFailPackets     uint64
	Peers             []BFDPeerOperationalState
	Issues            []string
	LastError         string
}

// BFDPeerOperationalState describes one BFD peer in operational output.
type BFDPeerOperationalState struct {
	Peer              string
	LocalAddress      string
	Interface         string
	VRF               string
	Status            string
	Diagnostic        string
	RemoteDiagnostic  string
	Observed          bool
	Up                bool
	SessionDownEvents uint64
	RxFailPackets     uint64
}
