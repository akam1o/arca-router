package model

import "time"

// OperationalState holds live state collected from VPP, FRR, and the kernel.
type OperationalState struct {
	Interfaces map[string]*InterfaceState `json:"interfaces,omitempty"`
	BGP        *BGPState                  `json:"bgp,omitempty"`
	OSPF       *OSPFState                 `json:"ospf,omitempty"`
	Routes     []*RouteEntry              `json:"routes,omitempty"`
	System     *SystemState               `json:"system,omitempty"`
}

// InterfaceState holds live interface counters and status.
type InterfaceState struct {
	Name        string             `json:"name"`
	AdminStatus string             `json:"admin-status"` // "up" | "down"
	OperStatus  string             `json:"oper-status"`  // "up" | "down"
	Speed       uint64             `json:"speed,omitempty"`
	MTU         uint32             `json:"mtu,omitempty"`
	MAC         string             `json:"mac,omitempty"`
	Counters    *InterfaceCounters `json:"counters,omitempty"`
	LastChange  time.Time          `json:"last-change,omitempty"`
}

// InterfaceCounters holds packet/byte counters for an interface.
type InterfaceCounters struct {
	RxPackets uint64 `json:"rx-packets"`
	TxPackets uint64 `json:"tx-packets"`
	RxBytes   uint64 `json:"rx-bytes"`
	TxBytes   uint64 `json:"tx-bytes"`
	RxErrors  uint64 `json:"rx-errors"`
	TxErrors  uint64 `json:"tx-errors"`
	Drops     uint64 `json:"drops"`
}

// BGPState holds live BGP state.
type BGPState struct {
	RouterID  string          `json:"router-id,omitempty"`
	LocalAS   uint32          `json:"local-as,omitempty"`
	Neighbors []*BGPPeerState `json:"neighbors,omitempty"`
}

// BGPPeerState holds live state for a single BGP peer.
type BGPPeerState struct {
	PeerAddress    string    `json:"peer-address"`
	PeerAS         uint32    `json:"peer-as"`
	State          string    `json:"state"` // "Established", "Idle", "Connect", etc.
	UptimeSecs     uint64    `json:"uptime-secs,omitempty"`
	PrefixReceived uint32    `json:"prefix-received,omitempty"`
	PrefixSent     uint32    `json:"prefix-sent,omitempty"`
	LastUpdated    time.Time `json:"last-updated,omitempty"`
}

// OSPFState holds live OSPF state.
type OSPFState struct {
	RouterID   string              `json:"router-id,omitempty"`
	Neighbors  []*OSPFNeighborState `json:"neighbors,omitempty"`
}

// OSPFNeighborState holds live state for a single OSPF neighbor.
type OSPFNeighborState struct {
	NeighborID string `json:"neighbor-id"`
	Address    string `json:"address"`
	State      string `json:"state"` // "Full", "2-Way", etc.
	Interface  string `json:"interface"`
	AreaID     string `json:"area-id"`
}

// RouteEntry represents a single entry in the routing table.
type RouteEntry struct {
	Prefix    string `json:"prefix"`
	NextHop   string `json:"next-hop"`
	Protocol  string `json:"protocol"` // "bgp", "ospf", "static", "connected"
	Metric    uint32 `json:"metric,omitempty"`
	Preference uint32 `json:"preference,omitempty"`
	Interface string `json:"interface,omitempty"`
	Active    bool   `json:"active"`
}

// SystemState holds live system state.
type SystemState struct {
	Hostname  string    `json:"hostname"`
	Uptime    uint64    `json:"uptime-secs"`
	StartedAt time.Time `json:"started-at"`
}
