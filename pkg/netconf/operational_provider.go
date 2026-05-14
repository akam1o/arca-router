package netconf

import (
	"context"
	"time"
)

// OperationalStateProvider supplies live state for NETCONF <get> replies.
type OperationalStateProvider interface {
	// InterfaceStates returns interface state keyed by management-plane interface name.
	InterfaceStates(ctx context.Context) (map[string]*InterfaceOperationalState, error)
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
