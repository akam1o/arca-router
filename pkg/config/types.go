package config

// Config represents the complete router configuration
type Config struct {
	// System holds system-level configuration
	System *SystemConfig `json:"system,omitempty"`

	// Chassis holds multi-chassis and clustering configuration
	Chassis *ChassisConfig `json:"chassis,omitempty"`

	// Interfaces holds interface configuration
	Interfaces map[string]*Interface `json:"interfaces,omitempty"`

	// Protocols holds routing protocol configuration
	Protocols *ProtocolConfig `json:"protocols,omitempty"`

	// RoutingOptions holds routing options
	RoutingOptions *RoutingOptions `json:"routing-options,omitempty"`

	// RoutingInstances holds per-instance VPN/VRF configuration
	RoutingInstances map[string]*RoutingInstance `json:"routing-instances,omitempty"`

	// PolicyOptions holds policy-options configuration
	PolicyOptions *PolicyOptions `json:"policy-options,omitempty"`

	// ClassOfService holds QoS and traffic-control configuration
	ClassOfService *ClassOfServiceConfig `json:"class-of-service,omitempty"`

	// Security holds security configuration (Phase 3)
	Security *SecurityConfig `json:"security,omitempty"`
}

// SystemConfig represents system-level settings
// Note: JSON tags use kebab-case to align with Junos-style naming
type SystemConfig struct {
	// HostName is the router's hostname
	HostName string `json:"host-name,omitempty"`

	// Services holds system service settings
	Services *SystemServicesConfig `json:"services,omitempty"`
}

// SystemServicesConfig represents system service settings.
type SystemServicesConfig struct {
	// WebUI holds browser UI service settings.
	WebUI *WebUIConfig `json:"web-ui,omitempty"`

	// Prometheus holds Prometheus metrics service settings.
	Prometheus *PrometheusConfig `json:"prometheus,omitempty"`

	// SNMP holds read-only SNMP service settings.
	SNMP *SNMPConfig `json:"snmp,omitempty"`
}

// WebUIConfig represents browser UI service settings.
type WebUIConfig struct {
	Enabled       bool   `json:"enabled,omitempty"`
	ListenAddress string `json:"listen-address,omitempty"`
	Port          int    `json:"port,omitempty"`
}

// PrometheusConfig represents Prometheus metrics service settings.
type PrometheusConfig struct {
	Enabled       bool   `json:"enabled,omitempty"`
	ListenAddress string `json:"listen-address,omitempty"`
	Port          int    `json:"port,omitempty"`
}

// SNMPConfig represents read-only SNMP service settings.
type SNMPConfig struct {
	Enabled       bool   `json:"enabled,omitempty"`
	ListenAddress string `json:"listen-address,omitempty"`
	Port          int    `json:"port,omitempty"`
	Community     string `json:"community,omitempty"`
}

// ChassisConfig represents chassis-level configuration.
type ChassisConfig struct {
	Cluster *ClusterConfig `json:"cluster,omitempty"`
}

// ClusterConfig represents multi-chassis clustering configuration.
type ClusterConfig struct {
	Enabled bool                    `json:"enabled,omitempty"`
	Nodes   map[string]*ClusterNode `json:"nodes,omitempty"`
	Sync    *ClusterSyncConfig      `json:"sync,omitempty"`
}

// ClusterNode represents a node in an HA cluster.
type ClusterNode struct {
	Name     string `json:"name"`
	Address  string `json:"address,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// ClusterSyncConfig represents cluster synchronization settings.
type ClusterSyncConfig struct {
	Etcd *EtcdSyncConfig `json:"etcd,omitempty"`
}

// EtcdSyncConfig represents etcd-backed config synchronization settings.
type EtcdSyncConfig struct {
	Endpoints []string `json:"endpoints,omitempty"`
}

// Interface represents a logical interface configuration
type Interface struct {
	// Description is a human-readable description
	Description string `json:"description,omitempty"`

	// Units holds logical unit configurations (sub-interfaces)
	Units map[int]*Unit `json:"units,omitempty"`
}

// Unit represents a logical unit (sub-interface) configuration
type Unit struct {
	// Family holds address family configurations
	Family map[string]*Family `json:"family,omitempty"`
}

// Family represents an address family (inet, inet6) configuration
type Family struct {
	// Addresses holds IP addresses in CIDR format
	Addresses []string `json:"addresses,omitempty"`
}

// NewConfig creates a new empty configuration
func NewConfig() *Config {
	return &Config{
		Interfaces: make(map[string]*Interface),
	}
}

// GetOrCreateInterface gets or creates an interface configuration
func (c *Config) GetOrCreateInterface(name string) *Interface {
	if c.Interfaces == nil {
		c.Interfaces = make(map[string]*Interface)
	}
	if c.Interfaces[name] == nil {
		c.Interfaces[name] = &Interface{
			Units: make(map[int]*Unit),
		}
	}
	return c.Interfaces[name]
}

// GetOrCreateUnit gets or creates a unit configuration
func (i *Interface) GetOrCreateUnit(unitNum int) *Unit {
	if i.Units == nil {
		i.Units = make(map[int]*Unit)
	}
	if i.Units[unitNum] == nil {
		i.Units[unitNum] = &Unit{
			Family: make(map[string]*Family),
		}
	}
	return i.Units[unitNum]
}

// GetOrCreateFamily gets or creates a family configuration
func (u *Unit) GetOrCreateFamily(familyName string) *Family {
	if u.Family == nil {
		u.Family = make(map[string]*Family)
	}
	if u.Family[familyName] == nil {
		u.Family[familyName] = &Family{
			Addresses: make([]string, 0),
		}
	}
	return u.Family[familyName]
}

// RoutingOptions represents routing options configuration
type RoutingOptions struct {
	// StaticRoutes holds static route configurations
	StaticRoutes []*StaticRoute `json:"static-routes,omitempty"`

	// AutonomousSystem is the AS number for BGP
	AutonomousSystem uint32 `json:"autonomous-system,omitempty"`

	// RouterID is the global router ID
	RouterID string `json:"router-id,omitempty"`
}

// StaticRoute represents a static route entry
type StaticRoute struct {
	// Prefix is the destination network in CIDR format
	Prefix string `json:"prefix"`

	// NextHop is the next-hop IP address
	NextHop string `json:"next-hop"`

	// Distance is the administrative distance (metric)
	Distance int `json:"distance,omitempty"`

	// BFD enables BFD monitoring for this static route
	BFD bool `json:"bfd,omitempty"`

	// BFDProfile selects the BFD profile for this static route
	BFDProfile string `json:"bfd-profile,omitempty"`

	// BFDSource selects the source address for the BFD session
	BFDSource string `json:"bfd-source,omitempty"`

	// BFDMultihop enables multihop BFD for this static route
	BFDMultihop bool `json:"bfd-multihop,omitempty"`
}

// RoutingInstance represents a routing instance, initially focused on VRF/L3VPN.
type RoutingInstance struct {
	Name               string   `json:"name"`
	InstanceType       string   `json:"instance-type,omitempty"`
	RouteDistinguisher string   `json:"route-distinguisher,omitempty"`
	VRFTarget          string   `json:"vrf-target,omitempty"`
	VRFTargetImport    []string `json:"vrf-target-import,omitempty"`
	VRFTargetExport    []string `json:"vrf-target-export,omitempty"`
	VRFImport          []string `json:"vrf-import,omitempty"`
	VRFExport          []string `json:"vrf-export,omitempty"`
	Interfaces         []string `json:"interfaces,omitempty"`
}

// ProtocolConfig represents routing protocol configuration
type ProtocolConfig struct {
	// BFD holds Bidirectional Forwarding Detection configuration
	BFD *BFDConfig `json:"bfd,omitempty"`

	// BGP holds BGP protocol configuration
	BGP *BGPConfig `json:"bgp,omitempty"`

	// EVPN holds EVPN/VXLAN overlay configuration
	EVPN *EVPNConfig `json:"evpn,omitempty"`

	// OSPF holds OSPF protocol configuration
	OSPF *OSPFConfig `json:"ospf,omitempty"`

	// OSPF3 holds OSPFv3 protocol configuration
	OSPF3 *OSPFConfig `json:"ospf3,omitempty"`

	// MPLS holds MPLS protocol configuration
	MPLS *MPLSConfig `json:"mpls,omitempty"`

	// VRRP holds VRRP high-availability groups
	VRRP *VRRPConfig `json:"vrrp,omitempty"`
}

// BFDConfig represents Bidirectional Forwarding Detection configuration.
type BFDConfig struct {
	Profiles map[string]*BFDProfile `json:"profiles,omitempty"`
	Peers    map[string]*BFDPeer    `json:"peers,omitempty"`
}

// BFDProfile represents reusable BFD timer and mode settings.
type BFDProfile struct {
	Name             string `json:"name"`
	DetectMultiplier int    `json:"detect-multiplier,omitempty"`
	ReceiveInterval  int    `json:"receive-interval,omitempty"`
	TransmitInterval int    `json:"transmit-interval,omitempty"`
	EchoMode         bool   `json:"echo-mode,omitempty"`
	PassiveMode      bool   `json:"passive-mode,omitempty"`
}

// BFDPeer represents one BFD peer session.
type BFDPeer struct {
	Address          string `json:"address"`
	LocalAddress     string `json:"local-address,omitempty"`
	Interface        string `json:"interface,omitempty"`
	VRF              string `json:"vrf,omitempty"`
	Multihop         bool   `json:"multihop,omitempty"`
	Profile          string `json:"profile,omitempty"`
	DetectMultiplier int    `json:"detect-multiplier,omitempty"`
	ReceiveInterval  int    `json:"receive-interval,omitempty"`
	TransmitInterval int    `json:"transmit-interval,omitempty"`
	EchoMode         bool   `json:"echo-mode,omitempty"`
	PassiveMode      bool   `json:"passive-mode,omitempty"`
	Shutdown         bool   `json:"shutdown,omitempty"`
}

// EVPNConfig represents EVPN/VXLAN overlay configuration.
type EVPNConfig struct {
	VNIs map[int]*EVPNVNI `json:"vnis,omitempty"`
}

// EVPNVNI represents one L2 or L3 VXLAN network identifier.
type EVPNVNI struct {
	VNI                int      `json:"vni"`
	Type               string   `json:"type,omitempty"`
	BridgeDomain       string   `json:"bridge-domain,omitempty"`
	VLANID             int      `json:"vlan-id,omitempty"`
	RoutingInstance    string   `json:"routing-instance,omitempty"`
	RouteDistinguisher string   `json:"route-distinguisher,omitempty"`
	VRFTarget          string   `json:"vrf-target,omitempty"`
	VRFTargetImport    []string `json:"vrf-target-import,omitempty"`
	VRFTargetExport    []string `json:"vrf-target-export,omitempty"`
	SourceInterface    string   `json:"source-interface,omitempty"`
	SourceAddress      string   `json:"source-address,omitempty"`
	MulticastGroup     string   `json:"multicast-group,omitempty"`
	RemoteVTEP         string   `json:"remote-vtep,omitempty"`
}

// MPLSConfig represents MPLS forwarding configuration.
type MPLSConfig struct {
	Interfaces []string `json:"interfaces,omitempty"`
}

// VRRPConfig represents VRRP groups.
type VRRPConfig struct {
	Groups map[string]*VRRPGroup `json:"groups,omitempty"`
}

// VRRPGroup represents one VRRP group.
type VRRPGroup struct {
	Name           string `json:"name"`
	Interface      string `json:"interface,omitempty"`
	VirtualAddress string `json:"virtual-address,omitempty"`
	Priority       int    `json:"priority,omitempty"`
	Preempt        bool   `json:"preempt,omitempty"`
}

// BGPConfig represents BGP protocol configuration
type BGPConfig struct {
	// Groups holds BGP group configurations
	Groups map[string]*BGPGroup `json:"groups,omitempty"`
}

// BGPGroup represents a BGP peer group configuration
type BGPGroup struct {
	// Type is the group type (internal or external)
	Type string `json:"type,omitempty"`

	// Neighbors holds neighbor configurations within this group
	Neighbors map[string]*BGPNeighbor `json:"neighbors,omitempty"`

	// Import is the import policy name (Phase 2: string only)
	Import string `json:"import,omitempty"`

	// Export is the export policy name (Phase 2: string only)
	Export string `json:"export,omitempty"`
}

// BGPNeighbor represents a BGP neighbor configuration
type BGPNeighbor struct {
	// IP is the neighbor IP address
	IP string `json:"ip"`

	// PeerAS is the peer AS number
	PeerAS uint32 `json:"peer-as"`

	// Description is a human-readable description
	Description string `json:"description,omitempty"`

	// LocalAddress is the local address to use for peering
	LocalAddress string `json:"local-address,omitempty"`

	// BFD enables BFD failure detection for this neighbor
	BFD bool `json:"bfd,omitempty"`

	// BFDProfile selects the BFD profile for this neighbor
	BFDProfile string `json:"bfd-profile,omitempty"`
}

// OSPFConfig represents OSPF protocol configuration
type OSPFConfig struct {
	// Areas holds OSPF area configurations
	Areas map[string]*OSPFArea `json:"areas,omitempty"`

	// RouterID is the OSPF router ID (overrides routing-options router-id)
	RouterID string `json:"router-id,omitempty"`
}

// OSPFArea represents an OSPF area configuration
type OSPFArea struct {
	// AreaID is the OSPF area ID (e.g., "0.0.0.0" or "0")
	AreaID string `json:"area-id"`

	// Interfaces holds interface configurations for this area
	Interfaces map[string]*OSPFInterface `json:"interfaces,omitempty"`
}

// OSPFInterface represents an OSPF interface configuration
type OSPFInterface struct {
	// Name is the interface name
	Name string `json:"name"`

	// Passive indicates if this is a passive interface
	Passive bool `json:"passive,omitempty"`

	// Metric is the OSPF metric for this interface
	Metric int `json:"metric,omitempty"`

	// Priority is the OSPF priority for DR election
	Priority int `json:"priority,omitempty"`

	// PrioritySet records whether priority was explicitly configured.
	PrioritySet bool `json:"-"`

	// BFD enables BFD failure detection on this OSPF interface
	BFD bool `json:"bfd,omitempty"`

	// BFDProfile selects the BFD profile for this OSPF interface
	BFDProfile string `json:"bfd-profile,omitempty"`
}

// PolicyOptions represents policy-options configuration
type PolicyOptions struct {
	// PrefixLists holds prefix-list configurations
	PrefixLists map[string]*PrefixList `json:"prefix-lists,omitempty"`

	// PolicyStatements holds policy-statement configurations
	PolicyStatements map[string]*PolicyStatement `json:"policy-statements,omitempty"`
}

// PrefixList represents a prefix-list configuration
type PrefixList struct {
	// Name is the prefix-list name
	Name string `json:"name"`

	// Prefixes holds the list of prefixes in CIDR format
	Prefixes []string `json:"prefixes,omitempty"`
}

// PolicyStatement represents a policy-statement configuration
type PolicyStatement struct {
	// Name is the policy-statement name
	Name string `json:"name"`

	// Terms holds policy terms
	Terms []*PolicyTerm `json:"terms,omitempty"`
}

// PolicyTerm represents a single term in a policy-statement
type PolicyTerm struct {
	// Name is the term name
	Name string `json:"name"`

	// From holds match conditions
	From *PolicyMatchConditions `json:"from,omitempty"`

	// Then holds actions
	Then *PolicyActions `json:"then,omitempty"`
}

// PolicyMatchConditions represents match conditions in a policy term
type PolicyMatchConditions struct {
	// PrefixLists holds prefix-list names to match
	PrefixLists []string `json:"prefix-lists,omitempty"`

	// Protocol is the routing protocol to match (e.g., "bgp", "ospf", "static")
	Protocol string `json:"protocol,omitempty"`

	// Neighbor is the BGP neighbor IP to match
	Neighbor string `json:"neighbor,omitempty"`

	// ASPath is the AS path regular expression to match
	ASPath string `json:"as-path,omitempty"`
}

// PolicyActions represents actions in a policy term
type PolicyActions struct {
	// Accept indicates whether to accept the route (true) or reject (false)
	// nil means no explicit accept/reject action
	Accept *bool `json:"accept,omitempty"`

	// LocalPreference is the local-preference value to set
	LocalPreference *uint32 `json:"local-preference,omitempty"`

	// Community is the BGP community to set
	Community string `json:"community,omitempty"`
}

// SecurityConfig represents security configuration (Phase 3)
type SecurityConfig struct {
	// NETCONF holds NETCONF server configuration
	NETCONF *NETCONFConfig `json:"netconf,omitempty"`

	// Users holds user configurations
	Users map[string]*UserConfig `json:"users,omitempty"`

	// RateLimit holds rate limiting configuration
	RateLimit *RateLimitConfig `json:"rate-limit,omitempty"`
}

// NETCONFConfig represents NETCONF server configuration
type NETCONFConfig struct {
	// SSH holds SSH configuration
	SSH *NETCONFSSHConfig `json:"ssh,omitempty"`
}

// NETCONFSSHConfig represents NETCONF SSH configuration
type NETCONFSSHConfig struct {
	// Enabled indicates whether the NETCONF/SSH service should be started
	Enabled bool `json:"enabled,omitempty"`

	// EnabledSet records whether enabled was explicitly configured.
	EnabledSet bool `json:"-"`

	// ListenAddress is the address to bind for NETCONF/SSH (default: 127.0.0.1)
	ListenAddress string `json:"listen-address,omitempty"`

	// Port is the TCP port for NETCONF/SSH (default: 830)
	Port int `json:"port,omitempty"`
}

// UserConfig represents a user configuration
type UserConfig struct {
	// Username is the username
	Username string `json:"username"`

	// Password is the user's password (will be hashed)
	Password string `json:"password,omitempty"`

	// Role is the user's role (admin, operator, read-only)
	Role string `json:"role,omitempty"`

	// SSHKey is the user's SSH public key
	SSHKey string `json:"ssh-key,omitempty"`
}

// RateLimitConfig represents rate limiting configuration
type RateLimitConfig struct {
	// PerIP is the per-IP rate limit (requests per second)
	PerIP int `json:"per-ip,omitempty"`

	// PerUser is the per-user rate limit (requests per second)
	PerUser int `json:"per-user,omitempty"`
}

// ClassOfServiceConfig represents QoS and traffic-control configuration.
type ClassOfServiceConfig struct {
	ForwardingClasses      map[string]*ForwardingClass       `json:"forwarding-classes,omitempty"`
	TrafficControlProfiles map[string]*TrafficControlProfile `json:"traffic-control-profiles,omitempty"`
	Interfaces             map[string]*CoSInterface          `json:"interfaces,omitempty"`
}

// ForwardingClass maps a forwarding class to a queue.
type ForwardingClass struct {
	Name  string `json:"name"`
	Queue int    `json:"queue"`
}

// TrafficControlProfile represents shaping and scheduler settings.
type TrafficControlProfile struct {
	Name         string `json:"name"`
	ShapingRate  uint64 `json:"shaping-rate,omitempty"`
	SchedulerMap string `json:"scheduler-map,omitempty"`
}

// CoSInterface binds QoS profiles to interfaces.
type CoSInterface struct {
	Name                        string `json:"name"`
	OutputTrafficControlProfile string `json:"output-traffic-control-profile,omitempty"`
}
