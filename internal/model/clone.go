package model

// Clone returns a deep copy of the router configuration.
func (c *RouterConfig) Clone() *RouterConfig {
	if c == nil {
		return nil
	}
	clone := &RouterConfig{}
	if c.System != nil {
		clone.System = c.System.Clone()
	}
	if c.Chassis != nil {
		clone.Chassis = c.Chassis.Clone()
	}
	if c.Interfaces != nil {
		clone.Interfaces = make(map[string]*InterfaceConfig, len(c.Interfaces))
		for name, iface := range c.Interfaces {
			clone.Interfaces[name] = iface.Clone()
		}
	}
	if c.Protocols != nil {
		clone.Protocols = c.Protocols.Clone()
	}
	if c.Routing != nil {
		clone.Routing = c.Routing.Clone()
	}
	if c.RoutingInstances != nil {
		clone.RoutingInstances = make(map[string]*RoutingInstance, len(c.RoutingInstances))
		for name, instance := range c.RoutingInstances {
			clone.RoutingInstances[name] = instance.Clone()
		}
	}
	if c.Policy != nil {
		clone.Policy = c.Policy.Clone()
	}
	if c.ClassOfService != nil {
		clone.ClassOfService = c.ClassOfService.Clone()
	}
	if c.Security != nil {
		clone.Security = c.Security.Clone()
	}
	return clone
}

// Clone returns a deep copy of the system configuration.
func (c *SystemConfig) Clone() *SystemConfig {
	if c == nil {
		return nil
	}
	clone := &SystemConfig{HostName: c.HostName}
	if c.Services != nil {
		clone.Services = c.Services.Clone()
	}
	return clone
}

// Clone returns a deep copy of the system services configuration.
func (c *SystemServicesConfig) Clone() *SystemServicesConfig {
	if c == nil {
		return nil
	}
	return &SystemServicesConfig{
		WebUI:      c.WebUI.Clone(),
		Prometheus: c.Prometheus.Clone(),
		SNMP:       c.SNMP.Clone(),
	}
}

// Clone returns a deep copy of the Web UI configuration.
func (c *WebUIConfig) Clone() *WebUIConfig {
	if c == nil {
		return nil
	}
	clone := *c
	return &clone
}

// Clone returns a deep copy of the Prometheus configuration.
func (c *PrometheusConfig) Clone() *PrometheusConfig {
	if c == nil {
		return nil
	}
	clone := *c
	return &clone
}

// Clone returns a deep copy of the SNMP configuration.
func (c *SNMPConfig) Clone() *SNMPConfig {
	if c == nil {
		return nil
	}
	clone := *c
	return &clone
}

// Clone returns a deep copy of the chassis configuration.
func (c *ChassisConfig) Clone() *ChassisConfig {
	if c == nil {
		return nil
	}
	return &ChassisConfig{Cluster: c.Cluster.Clone()}
}

// Clone returns a deep copy of the cluster configuration.
func (c *ClusterConfig) Clone() *ClusterConfig {
	if c == nil {
		return nil
	}
	clone := &ClusterConfig{Enabled: c.Enabled}
	if c.Nodes != nil {
		clone.Nodes = make(map[string]*ClusterNode, len(c.Nodes))
		for name, node := range c.Nodes {
			if node == nil {
				clone.Nodes[name] = nil
				continue
			}
			n := *node
			clone.Nodes[name] = &n
		}
	}
	if c.Sync != nil {
		clone.Sync = c.Sync.Clone()
	}
	return clone
}

// Clone returns a deep copy of the cluster sync configuration.
func (c *ClusterSyncConfig) Clone() *ClusterSyncConfig {
	if c == nil {
		return nil
	}
	return &ClusterSyncConfig{Etcd: c.Etcd.Clone()}
}

// Clone returns a deep copy of the etcd sync configuration.
func (c *EtcdSyncConfig) Clone() *EtcdSyncConfig {
	if c == nil {
		return nil
	}
	return &EtcdSyncConfig{Endpoints: append([]string(nil), c.Endpoints...)}
}

// Clone returns a deep copy of the snapshot.
func (s *ConfigSnapshot) Clone() *ConfigSnapshot {
	if s == nil {
		return nil
	}
	clone := *s
	clone.Config = s.Config.Clone()
	return &clone
}

// Clone returns a deep copy of the interface configuration.
func (c *InterfaceConfig) Clone() *InterfaceConfig {
	if c == nil {
		return nil
	}
	clone := &InterfaceConfig{Description: c.Description}
	if c.Units != nil {
		clone.Units = make(map[int]*Unit, len(c.Units))
		for unitNum, unit := range c.Units {
			clone.Units[unitNum] = unit.Clone()
		}
	}
	return clone
}

// Clone returns a deep copy of the interface unit.
func (u *Unit) Clone() *Unit {
	if u == nil {
		return nil
	}
	clone := &Unit{}
	if u.Family != nil {
		clone.Family = make(map[string]*AddressFamily, len(u.Family))
		for name, family := range u.Family {
			clone.Family[name] = family.Clone()
		}
	}
	return clone
}

// Clone returns a deep copy of the address family.
func (a *AddressFamily) Clone() *AddressFamily {
	if a == nil {
		return nil
	}
	return &AddressFamily{Addresses: append([]string(nil), a.Addresses...)}
}

// Clone returns a deep copy of the protocol configuration.
func (c *ProtocolsConfig) Clone() *ProtocolsConfig {
	if c == nil {
		return nil
	}
	return &ProtocolsConfig{
		BGP:  c.BGP.Clone(),
		OSPF: c.OSPF.Clone(),
		MPLS: c.MPLS.Clone(),
		VRRP: c.VRRP.Clone(),
	}
}

// Clone returns a deep copy of the MPLS configuration.
func (c *MPLSConfig) Clone() *MPLSConfig {
	if c == nil {
		return nil
	}
	return &MPLSConfig{Interfaces: append([]string(nil), c.Interfaces...)}
}

// Clone returns a deep copy of the VRRP configuration.
func (c *VRRPConfig) Clone() *VRRPConfig {
	if c == nil {
		return nil
	}
	clone := &VRRPConfig{}
	if c.Groups != nil {
		clone.Groups = make(map[string]*VRRPGroup, len(c.Groups))
		for name, group := range c.Groups {
			if group == nil {
				clone.Groups[name] = nil
				continue
			}
			g := *group
			clone.Groups[name] = &g
		}
	}
	return clone
}

// Clone returns a deep copy of the BGP configuration.
func (c *BGPConfig) Clone() *BGPConfig {
	if c == nil {
		return nil
	}
	clone := &BGPConfig{}
	if c.Groups != nil {
		clone.Groups = make(map[string]*BGPGroup, len(c.Groups))
		for name, group := range c.Groups {
			clone.Groups[name] = group.Clone()
		}
	}
	return clone
}

// Clone returns a deep copy of the BGP group.
func (g *BGPGroup) Clone() *BGPGroup {
	if g == nil {
		return nil
	}
	clone := &BGPGroup{
		Type:   g.Type,
		Import: g.Import,
		Export: g.Export,
	}
	if g.Neighbors != nil {
		clone.Neighbors = make(map[string]*BGPNeighbor, len(g.Neighbors))
		for addr, neighbor := range g.Neighbors {
			if neighbor == nil {
				clone.Neighbors[addr] = nil
				continue
			}
			n := *neighbor
			clone.Neighbors[addr] = &n
		}
	}
	return clone
}

// Clone returns a deep copy of the OSPF configuration.
func (c *OSPFConfig) Clone() *OSPFConfig {
	if c == nil {
		return nil
	}
	clone := &OSPFConfig{RouterID: c.RouterID}
	if c.Areas != nil {
		clone.Areas = make(map[string]*OSPFArea, len(c.Areas))
		for name, area := range c.Areas {
			clone.Areas[name] = area.Clone()
		}
	}
	return clone
}

// Clone returns a deep copy of the OSPF area.
func (a *OSPFArea) Clone() *OSPFArea {
	if a == nil {
		return nil
	}
	clone := &OSPFArea{}
	if a.Interfaces != nil {
		clone.Interfaces = make(map[string]*OSPFInterface, len(a.Interfaces))
		for name, iface := range a.Interfaces {
			if iface == nil {
				clone.Interfaces[name] = nil
				continue
			}
			i := *iface
			if iface.Priority != nil {
				priority := *iface.Priority
				i.Priority = &priority
			}
			clone.Interfaces[name] = &i
		}
	}
	return clone
}

// Clone returns a deep copy of the routing configuration.
func (c *RoutingConfig) Clone() *RoutingConfig {
	if c == nil {
		return nil
	}
	clone := &RoutingConfig{
		AutonomousSystem: c.AutonomousSystem,
		RouterID:         c.RouterID,
	}
	if c.StaticRoutes != nil {
		clone.StaticRoutes = make([]*StaticRoute, len(c.StaticRoutes))
		for i, route := range c.StaticRoutes {
			if route == nil {
				continue
			}
			r := *route
			clone.StaticRoutes[i] = &r
		}
	}
	return clone
}

// Clone returns a deep copy of the routing instance.
func (c *RoutingInstance) Clone() *RoutingInstance {
	if c == nil {
		return nil
	}
	clone := *c
	clone.Interfaces = append([]string(nil), c.Interfaces...)
	return &clone
}

// Clone returns a deep copy of the policy configuration.
func (c *PolicyConfig) Clone() *PolicyConfig {
	if c == nil {
		return nil
	}
	clone := &PolicyConfig{}
	if c.PrefixLists != nil {
		clone.PrefixLists = make(map[string]*PrefixList, len(c.PrefixLists))
		for name, list := range c.PrefixLists {
			clone.PrefixLists[name] = list.Clone()
		}
	}
	if c.PolicyStatements != nil {
		clone.PolicyStatements = make(map[string]*PolicyStatement, len(c.PolicyStatements))
		for name, statement := range c.PolicyStatements {
			clone.PolicyStatements[name] = statement.Clone()
		}
	}
	return clone
}

// Clone returns a deep copy of the prefix list.
func (p *PrefixList) Clone() *PrefixList {
	if p == nil {
		return nil
	}
	return &PrefixList{Prefixes: append([]string(nil), p.Prefixes...)}
}

// Clone returns a deep copy of the policy statement.
func (p *PolicyStatement) Clone() *PolicyStatement {
	if p == nil {
		return nil
	}
	clone := &PolicyStatement{}
	if p.Terms != nil {
		clone.Terms = make([]*PolicyTerm, len(p.Terms))
		for i, term := range p.Terms {
			clone.Terms[i] = term.Clone()
		}
	}
	return clone
}

// Clone returns a deep copy of the policy term.
func (p *PolicyTerm) Clone() *PolicyTerm {
	if p == nil {
		return nil
	}
	return &PolicyTerm{
		Name: p.Name,
		From: p.From.Clone(),
		Then: p.Then.Clone(),
	}
}

// Clone returns a deep copy of the policy match conditions.
func (p *PolicyMatchConditions) Clone() *PolicyMatchConditions {
	if p == nil {
		return nil
	}
	return &PolicyMatchConditions{
		PrefixLists: append([]string(nil), p.PrefixLists...),
		Protocol:    p.Protocol,
		Neighbor:    p.Neighbor,
		ASPath:      p.ASPath,
	}
}

// Clone returns a deep copy of the policy actions.
func (p *PolicyActions) Clone() *PolicyActions {
	if p == nil {
		return nil
	}
	clone := &PolicyActions{Community: p.Community}
	if p.Accept != nil {
		accept := *p.Accept
		clone.Accept = &accept
	}
	if p.LocalPreference != nil {
		localPreference := *p.LocalPreference
		clone.LocalPreference = &localPreference
	}
	return clone
}

// Clone returns a deep copy of the security configuration.
func (c *SecurityConfig) Clone() *SecurityConfig {
	if c == nil {
		return nil
	}
	clone := &SecurityConfig{}
	if c.NETCONF != nil {
		clone.NETCONF = c.NETCONF.Clone()
	}
	if c.Users != nil {
		clone.Users = make(map[string]*UserConfig, len(c.Users))
		for name, user := range c.Users {
			if user == nil {
				clone.Users[name] = nil
				continue
			}
			u := *user
			clone.Users[name] = &u
		}
	}
	if c.RateLimit != nil {
		rateLimit := *c.RateLimit
		clone.RateLimit = &rateLimit
	}
	return clone
}

// Clone returns a deep copy of the NETCONF security configuration.
func (c *NETCONFSecurityConfig) Clone() *NETCONFSecurityConfig {
	if c == nil {
		return nil
	}
	clone := &NETCONFSecurityConfig{}
	if c.SSH != nil {
		ssh := *c.SSH
		clone.SSH = &ssh
	}
	return clone
}

// Clone returns a deep copy of the class-of-service configuration.
func (c *ClassOfServiceConfig) Clone() *ClassOfServiceConfig {
	if c == nil {
		return nil
	}
	clone := &ClassOfServiceConfig{}
	if c.ForwardingClasses != nil {
		clone.ForwardingClasses = make(map[string]*ForwardingClass, len(c.ForwardingClasses))
		for name, fc := range c.ForwardingClasses {
			if fc == nil {
				clone.ForwardingClasses[name] = nil
				continue
			}
			f := *fc
			clone.ForwardingClasses[name] = &f
		}
	}
	if c.TrafficControlProfiles != nil {
		clone.TrafficControlProfiles = make(map[string]*TrafficControlProfile, len(c.TrafficControlProfiles))
		for name, profile := range c.TrafficControlProfiles {
			if profile == nil {
				clone.TrafficControlProfiles[name] = nil
				continue
			}
			p := *profile
			clone.TrafficControlProfiles[name] = &p
		}
	}
	if c.Interfaces != nil {
		clone.Interfaces = make(map[string]*CoSInterface, len(c.Interfaces))
		for name, iface := range c.Interfaces {
			if iface == nil {
				clone.Interfaces[name] = nil
				continue
			}
			i := *iface
			clone.Interfaces[name] = &i
		}
	}
	return clone
}
