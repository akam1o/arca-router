package netconf

import "context"

// OperationalStateProvider supplies live state for NETCONF <get> replies.
type OperationalStateProvider interface {
	// InterfaceStates returns interface state keyed by management-plane interface name.
	InterfaceStates(ctx context.Context) (map[string]*InterfaceOperationalState, error)
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
