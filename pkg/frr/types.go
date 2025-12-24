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

	// OSPF holds OSPF configuration
	OSPF *OSPFConfig

	// StaticRoutes holds static route configurations
	StaticRoutes []StaticRoute

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

	// IsIPv6 indicates if this is an IPv6 neighbor
	IsIPv6 bool
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
}
