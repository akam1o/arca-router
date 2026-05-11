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
	System     *SystemConfig               `json:"system,omitempty"`
	Interfaces map[string]*InterfaceConfig `json:"interfaces,omitempty"`
	Protocols  *ProtocolsConfig            `json:"protocols,omitempty"`
	Routing    *RoutingConfig              `json:"routing-options,omitempty"`
	Policy     *PolicyConfig               `json:"policy-options,omitempty"`
	Security   *SecurityConfig             `json:"security,omitempty"`
}

// SystemConfig holds system-level settings.
type SystemConfig struct {
	HostName string `json:"host-name,omitempty"`
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
	BGP  *BGPConfig  `json:"bgp,omitempty"`
	OSPF *OSPFConfig `json:"ospf,omitempty"`
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
