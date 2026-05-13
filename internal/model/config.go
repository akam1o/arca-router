// Package model defines the canonical configuration and operational state types
// for arca-router. These types are the single source of truth — all input formats
// (set commands, XML, YAML) are parsed into these types, and all output formats
// are serialized from them.
package model

import (
	"crypto/sha256"
	"encoding/json"
	"time"
)

// RouterConfig represents the complete, normalized router configuration.
// This is the primary data structure — not text. Diff, merge, and validate
// operations work directly on this struct.
type RouterConfig struct {
	System           *SystemConfig               `json:"system,omitempty"`
	Chassis          *ChassisConfig              `json:"chassis,omitempty"`
	Interfaces       map[string]*InterfaceConfig `json:"interfaces,omitempty"`
	Protocols        *ProtocolsConfig            `json:"protocols,omitempty"`
	Routing          *RoutingConfig              `json:"routing-options,omitempty"`
	RoutingInstances map[string]*RoutingInstance `json:"routing-instances,omitempty"`
	Policy           *PolicyConfig               `json:"policy-options,omitempty"`
	ClassOfService   *ClassOfServiceConfig       `json:"class-of-service,omitempty"`
	Security         *SecurityConfig             `json:"security,omitempty"`
}

// SystemConfig holds system-level settings.
type SystemConfig struct {
	HostName string                `json:"host-name,omitempty"`
	Services *SystemServicesConfig `json:"services,omitempty"`
}

// SystemServicesConfig holds system service settings.
type SystemServicesConfig struct {
	WebUI      *WebUIConfig      `json:"web-ui,omitempty"`
	Prometheus *PrometheusConfig `json:"prometheus,omitempty"`
	SNMP       *SNMPConfig       `json:"snmp,omitempty"`
}

// WebUIConfig holds browser UI service settings.
type WebUIConfig struct {
	Enabled       bool   `json:"enabled,omitempty"`
	ListenAddress string `json:"listen-address,omitempty"`
	Port          int    `json:"port,omitempty"`
}

// PrometheusConfig holds Prometheus metrics service settings.
type PrometheusConfig struct {
	Enabled       bool   `json:"enabled,omitempty"`
	ListenAddress string `json:"listen-address,omitempty"`
	Port          int    `json:"port,omitempty"`
}

// SNMPConfig holds read-only SNMP service settings.
type SNMPConfig struct {
	Enabled       bool   `json:"enabled,omitempty"`
	ListenAddress string `json:"listen-address,omitempty"`
	Port          int    `json:"port,omitempty"`
	Community     string `json:"community,omitempty"`
}

// ChassisConfig holds chassis-level settings.
type ChassisConfig struct {
	Cluster *ClusterConfig `json:"cluster,omitempty"`
}

// ClusterConfig holds multi-chassis clustering settings.
type ClusterConfig struct {
	Enabled bool                    `json:"enabled,omitempty"`
	Nodes   map[string]*ClusterNode `json:"nodes,omitempty"`
	Sync    *ClusterSyncConfig      `json:"sync,omitempty"`
}

// ClusterNode represents one HA cluster node.
type ClusterNode struct {
	Address  string `json:"address,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// ClusterSyncConfig holds cluster synchronization settings.
type ClusterSyncConfig struct {
	Etcd *EtcdSyncConfig `json:"etcd,omitempty"`
}

// EtcdSyncConfig holds etcd-backed synchronization settings.
type EtcdSyncConfig struct {
	Endpoints []string `json:"endpoints,omitempty"`
}

// InterfaceConfig represents a physical or logical interface.
type InterfaceConfig struct {
	Description string        `json:"description,omitempty"`
	Units       map[int]*Unit `json:"units,omitempty"`
}

// Unit represents a logical sub-interface.
type Unit struct {
	Family map[string]*AddressFamily `json:"family,omitempty"`
}

// AddressFamily represents inet or inet6 address configuration.
type AddressFamily struct {
	Addresses []string `json:"addresses,omitempty"`
}

// ProtocolsConfig holds routing protocol configurations.
type ProtocolsConfig struct {
	BGP   *BGPConfig  `json:"bgp,omitempty"`
	OSPF  *OSPFConfig `json:"ospf,omitempty"`
	OSPF3 *OSPFConfig `json:"ospf3,omitempty"`
	MPLS  *MPLSConfig `json:"mpls,omitempty"`
	VRRP  *VRRPConfig `json:"vrrp,omitempty"`
}

// MPLSConfig represents MPLS forwarding configuration.
type MPLSConfig struct {
	Interfaces []string `json:"interfaces,omitempty"`
}

// VRRPConfig represents VRRP groups.
type VRRPConfig struct {
	Groups map[string]*VRRPGroup `json:"groups,omitempty"`
}

// VRRPGroup represents a VRRP group.
type VRRPGroup struct {
	Interface      string `json:"interface,omitempty"`
	VirtualAddress string `json:"virtual-address,omitempty"`
	Priority       int    `json:"priority,omitempty"`
	Preempt        bool   `json:"preempt,omitempty"`
}

// BGPConfig represents BGP configuration.
type BGPConfig struct {
	Groups map[string]*BGPGroup `json:"groups,omitempty"`
}

// BGPGroup represents a BGP peer group.
type BGPGroup struct {
	Type      string                  `json:"type,omitempty"`
	Neighbors map[string]*BGPNeighbor `json:"neighbors,omitempty"`
	Import    string                  `json:"import,omitempty"`
	Export    string                  `json:"export,omitempty"`
}

// BGPNeighbor represents a BGP peer.
type BGPNeighbor struct {
	PeerAS       uint32 `json:"peer-as"`
	Description  string `json:"description,omitempty"`
	LocalAddress string `json:"local-address,omitempty"`
}

// OSPFConfig represents OSPF configuration.
type OSPFConfig struct {
	RouterID string               `json:"router-id,omitempty"`
	Areas    map[string]*OSPFArea `json:"areas,omitempty"`
}

// OSPFArea represents an OSPF area.
type OSPFArea struct {
	Interfaces map[string]*OSPFInterface `json:"interfaces,omitempty"`
}

// OSPFInterface represents OSPF per-interface settings.
type OSPFInterface struct {
	Passive  bool `json:"passive,omitempty"`
	Metric   int  `json:"metric,omitempty"`
	Priority *int `json:"priority,omitempty"`
}

// RoutingConfig holds routing options.
type RoutingConfig struct {
	AutonomousSystem uint32         `json:"autonomous-system,omitempty"`
	RouterID         string         `json:"router-id,omitempty"`
	StaticRoutes     []*StaticRoute `json:"static-routes,omitempty"`
}

// StaticRoute represents a static route entry.
type StaticRoute struct {
	Prefix   string `json:"prefix"`
	NextHop  string `json:"next-hop"`
	Distance int    `json:"distance,omitempty"`
}

// RoutingInstance represents a routing instance, initially focused on VRF/L3VPN.
type RoutingInstance struct {
	InstanceType       string   `json:"instance-type,omitempty"`
	RouteDistinguisher string   `json:"route-distinguisher,omitempty"`
	VRFTarget          string   `json:"vrf-target,omitempty"`
	VRFTargetImport    []string `json:"vrf-target-import,omitempty"`
	VRFTargetExport    []string `json:"vrf-target-export,omitempty"`
	VRFImport          []string `json:"vrf-import,omitempty"`
	VRFExport          []string `json:"vrf-export,omitempty"`
	Interfaces         []string `json:"interfaces,omitempty"`
}

// PolicyConfig holds policy-options.
type PolicyConfig struct {
	PrefixLists      map[string]*PrefixList      `json:"prefix-lists,omitempty"`
	PolicyStatements map[string]*PolicyStatement `json:"policy-statements,omitempty"`
}

// PrefixList represents a named prefix-list.
type PrefixList struct {
	Prefixes []string `json:"prefixes,omitempty"`
}

// PolicyStatement represents a named policy-statement.
type PolicyStatement struct {
	Terms []*PolicyTerm `json:"terms,omitempty"`
}

// PolicyTerm represents a single term in a policy-statement.
type PolicyTerm struct {
	Name string                 `json:"name"`
	From *PolicyMatchConditions `json:"from,omitempty"`
	Then *PolicyActions         `json:"then,omitempty"`
}

// PolicyMatchConditions represents match conditions.
type PolicyMatchConditions struct {
	PrefixLists []string `json:"prefix-lists,omitempty"`
	Protocol    string   `json:"protocol,omitempty"`
	Neighbor    string   `json:"neighbor,omitempty"`
	ASPath      string   `json:"as-path,omitempty"`
}

// PolicyActions represents policy actions.
type PolicyActions struct {
	Accept          *bool   `json:"accept,omitempty"`
	LocalPreference *uint32 `json:"local-preference,omitempty"`
	Community       string  `json:"community,omitempty"`
}

// SecurityConfig holds security settings.
type SecurityConfig struct {
	NETCONF   *NETCONFSecurityConfig `json:"netconf,omitempty"`
	Users     map[string]*UserConfig `json:"users,omitempty"`
	RateLimit *RateLimitConfig       `json:"rate-limit,omitempty"`
}

// NETCONFSecurityConfig holds NETCONF server security settings.
type NETCONFSecurityConfig struct {
	SSH *NETCONFSSHConfig `json:"ssh,omitempty"`
}

// NETCONFSSHConfig holds NETCONF SSH settings.
type NETCONFSSHConfig struct {
	Port int `json:"port,omitempty"`
}

// UserConfig represents a user account.
type UserConfig struct {
	Password string `json:"password,omitempty"`
	Role     string `json:"role,omitempty"`
	SSHKey   string `json:"ssh-key,omitempty"`
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	PerIP   int `json:"per-ip,omitempty"`
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
	Queue int `json:"queue"`
}

// TrafficControlProfile represents shaping and scheduler settings.
type TrafficControlProfile struct {
	ShapingRate  uint64 `json:"shaping-rate,omitempty"`
	SchedulerMap string `json:"scheduler-map,omitempty"`
}

// CoSInterface binds QoS profiles to interfaces.
type CoSInterface struct {
	OutputTrafficControlProfile string `json:"output-traffic-control-profile,omitempty"`
}

// NewRouterConfig creates an empty RouterConfig with initialized maps.
func NewRouterConfig() *RouterConfig {
	return &RouterConfig{
		Interfaces: make(map[string]*InterfaceConfig),
	}
}

// ConfigSnapshot is an immutable, versioned configuration snapshot.
type ConfigSnapshot struct {
	Version   uint64        `json:"version"`
	Config    *RouterConfig `json:"config"`
	Hash      [32]byte      `json:"hash"`
	Author    string        `json:"author"`
	Message   string        `json:"message,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
}

// NewSnapshot creates a new ConfigSnapshot from a RouterConfig.
func NewSnapshot(cfg *RouterConfig, version uint64, author, message string) *ConfigSnapshot {
	snapshotCfg := cfg.Clone()
	data, _ := json.Marshal(snapshotCfg)
	return &ConfigSnapshot{
		Version:   version,
		Config:    snapshotCfg,
		Hash:      sha256.Sum256(data),
		Author:    author,
		Message:   message,
		CreatedAt: time.Now(),
	}
}
