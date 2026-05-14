// Package frr provides FRR (Free Range Routing) configuration generation and management.
package frr

// Config represents the complete FRR configuration to be generated.
type Config struct {
	// Hostname is the router hostname
	Hostname string

	// LogFile is the FRR log file path
	LogFile string

	// LogTimestamp enables timestamp in logs
	LogTimestamp bool

	// BGP holds BGP configuration
	BGP *BGPConfig

	// BFD holds Bidirectional Forwarding Detection configuration
	BFD *BFDConfig

	// OSPF holds OSPF configuration
	OSPF *OSPFConfig

	// OSPF3 holds OSPFv3 configuration
	OSPF3 *OSPFConfig

	// VRRP holds VRRP configuration
	VRRP *VRRPConfig

	// VRFs holds L3VPN routing-instance configuration
	VRFs []VRFConfig

	// StaticRoutes holds static route configurations
	StaticRoutes []StaticRoute

	// PrefixLists holds prefix-list configurations
	PrefixLists []PrefixList

	// RouteMaps holds route-map configurations
	RouteMaps []RouteMap

	// ASPathAccessLists holds AS-path access-list configurations
	ASPathAccessLists []ASPathAccessList

	// InterfaceMapping maps Junos interface names to Linux interface names
	// Key: Junos name (e.g., "ge-0/0/0"), Value: Linux name (e.g., "ge0-0-0")
	InterfaceMapping map[string]string
}

// BGPConfig represents FRR BGP configuration.
type BGPConfig struct {
	// ASN is the local AS number
	ASN uint32

	// RouterID is the BGP router ID
	RouterID string

	// Neighbors holds BGP neighbor configurations
	Neighbors []BGPNeighbor

	// IPv4Unicast enables IPv4 unicast address family
	IPv4Unicast bool

	// IPv6Unicast enables IPv6 unicast address family
	IPv6Unicast bool

	// EVPN holds EVPN/VXLAN BGP address-family configuration
	EVPN *EVPNConfig
}

// EVPNConfig represents FRR EVPN/VXLAN BGP configuration.
type EVPNConfig struct {
	VNIs []EVPNVNI
}

// EVPNVNI represents one EVPN VNI in FRR format.
type EVPNVNI struct {
	VNI                int
	Type               string
	BridgeDomain       string
	VLANID             int
	RoutingInstance    string
	RouteDistinguisher string
	ImportTargets      []string
	ExportTargets      []string
	SourceInterface    string
	SourceAddress      string
	MulticastGroup     string
}

// BFDConfig represents FRR BFD configuration.
type BFDConfig struct {
	Profiles []BFDProfile
	Peers    []BFDPeer
}

// BFDProfile represents an FRR BFD profile.
type BFDProfile struct {
	Name             string
	DetectMultiplier int
	ReceiveInterval  int
	TransmitInterval int
	EchoMode         bool
	PassiveMode      bool
}

// BFDPeer represents an FRR BFD peer.
type BFDPeer struct {
	Address          string
	LocalAddress     string
	Interface        string
	VRF              string
	Multihop         bool
	Profile          string
	DetectMultiplier int
	ReceiveInterval  int
	TransmitInterval int
	EchoMode         bool
	PassiveMode      bool
	Shutdown         bool
}

// BGPNeighbor represents a BGP neighbor configuration in FRR format.
type BGPNeighbor struct {
	// IP is the neighbor IP address
	IP string

	// RemoteAS is the peer AS number
	RemoteAS uint32

	// Description is a human-readable description
	Description string

	// UpdateSource is the source interface or IP for BGP connection
	// Can be either an interface name (Linux format) or IP address
	UpdateSource string

	// BFD enables BFD failure detection for this neighbor
	BFD bool

	// BFDProfile selects the BFD profile for this neighbor
	BFDProfile string

	// IsIPv6 indicates if this is an IPv6 neighbor
	IsIPv6 bool

	// RouteMapIn is the route-map applied to incoming routes (import policy)
	RouteMapIn string

	// RouteMapOut is the route-map applied to outgoing routes (export policy)
	RouteMapOut string
}

// OSPFConfig represents FRR OSPF configuration.
type OSPFConfig struct {
	// RouterID is the OSPF router ID
	RouterID string

	// Networks holds OSPF network configurations
	Networks []OSPFNetwork

	// Interfaces holds OSPF interface-specific configurations
	Interfaces []OSPFInterface

	// IsOSPFv3 indicates if this is OSPFv3 (IPv6)
	IsOSPFv3 bool
}

// OSPFNetwork represents an OSPF network statement.
type OSPFNetwork struct {
	// Prefix is the network prefix in CIDR format
	Prefix string

	// AreaID is the OSPF area ID (e.g., "0.0.0.0" or "0")
	AreaID string
}

// OSPFInterface represents OSPF interface-specific configuration.
type OSPFInterface struct {
	// Name is the Linux interface name
	Name string

	// AreaID is the OSPF area ID for this interface
	AreaID string

	// Passive indicates if this is a passive interface
	Passive bool

	// Metric is the OSPF metric for this interface (0 = not set)
	Metric int

	// Priority is the OSPF priority for this interface (nil = not set)
	Priority *int

	// BFD enables BFD failure detection on this OSPF interface
	BFD bool

	// BFDProfile selects the BFD profile for this OSPF interface
	BFDProfile string
}

// VRRPConfig represents FRR VRRP configuration.
type VRRPConfig struct {
	Groups []VRRPGroup
}

// VRRPGroup represents one VRRP virtual router in FRR format.
type VRRPGroup struct {
	ID             int
	Interface      string
	VirtualAddress string
	Priority       int
	Preempt        bool
}

// VRFConfig represents FRR VRF/L3VPN route leaking configuration.
type VRFConfig struct {
	Name               string
	ASN                uint32
	VNI                int
	RouteDistinguisher string
	ImportTargets      []string
	ExportTargets      []string
	ImportRouteMap     string
	ExportRouteMap     string
	EVPN               *VRFEVPNConfig
}

// VRFEVPNConfig represents per-VRF EVPN address-family configuration.
type VRFEVPNConfig struct {
	ImportTargets        []string
	ExportTargets        []string
	AdvertiseIPv4Unicast bool
	AdvertiseIPv6Unicast bool
}

// StaticRoute represents a static route configuration in FRR format.
type StaticRoute struct {
	// Prefix is the destination network in CIDR format
	Prefix string

	// NextHop is the next-hop IP address
	NextHop string

	// Distance is the administrative distance (metric)
	Distance int

	// IsIPv6 indicates if this is an IPv6 route
	IsIPv6 bool

	// BFD enables BFD monitoring for this static route
	BFD bool

	// BFDProfile selects the BFD profile for this static route
	BFDProfile string

	// BFDSource selects the source address for the BFD session
	BFDSource string

	// BFDMultihop enables multihop BFD for this static route
	BFDMultihop bool
}

// PrefixList represents an FRR prefix-list configuration.
type PrefixList struct {
	// Name is the prefix-list name
	Name string

	// IsIPv6 indicates if this is an IPv6 prefix-list
	IsIPv6 bool

	// Entries holds prefix-list entries
	Entries []PrefixListEntry
}

// PrefixListEntry represents a single entry in a prefix-list.
type PrefixListEntry struct {
	// Seq is the sequence number
	Seq int

	// Action is "permit" or "deny"
	Action string

	// Prefix is the network prefix in CIDR format
	Prefix string
}

// RouteMap represents an FRR route-map configuration.
type RouteMap struct {
	// Name is the route-map name
	Name string

	// Entries holds route-map entries (terms)
	Entries []RouteMapEntry
}

// ASPathAccessList represents an FRR BGP AS-path access-list.
type ASPathAccessList struct {
	// Name is the access-list name
	Name string

	// Entries holds AS-path access-list entries
	Entries []ASPathAccessListEntry
}

// ASPathAccessListEntry represents a single entry in an AS-path access-list.
type ASPathAccessListEntry struct {
	// Seq is the sequence number
	Seq int

	// Action is "permit" or "deny"
	Action string

	// Regex is the AS-path regular expression
	Regex string
}

// RouteMapEntry represents a single entry in a route-map.
type RouteMapEntry struct {
	// Seq is the sequence number
	Seq int

	// Action is "permit" or "deny"
	Action string

	// MatchPrefixLists holds prefix-list names to match
	MatchPrefixLists []string

	// MatchProtocol is the routing protocol to match
	MatchProtocol string

	// MatchNeighbor is the BGP neighbor IP to match
	MatchNeighbor string

	// MatchASPath is the AS path access-list name to match
	MatchASPath string

	// SetLocalPreference is the local-preference value to set (nil = not set)
	SetLocalPreference *uint32

	// SetCommunity is the BGP community to set
	SetCommunity string
}
