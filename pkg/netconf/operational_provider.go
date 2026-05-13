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
	Counters    *InterfaceOperationalCounters
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
