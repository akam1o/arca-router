package model

import (
	"github.com/akam1o/arca-router/pkg/config"
)

// FromLegacyConfig converts the legacy pkg/config.Config to the new internal model.
// This adapter enables incremental migration — existing parsers continue to produce
// the old type, and this function bridges to the new canonical model.
func FromLegacyConfig(old *config.Config) *RouterConfig {
	if old == nil {
		return NewRouterConfig()
	}

	c := NewRouterConfig()

	// System
	if old.System != nil {
		c.System = &SystemConfig{HostName: old.System.HostName}
		if old.System.Services != nil && old.System.Services.WebUI != nil {
			c.System.Services = &SystemServicesConfig{
				WebUI: &WebUIConfig{
					Enabled:       old.System.Services.WebUI.Enabled,
					ListenAddress: old.System.Services.WebUI.ListenAddress,
					Port:          old.System.Services.WebUI.Port,
				},
			}
		}
	}

	if old.Chassis != nil && old.Chassis.Cluster != nil {
		c.Chassis = &ChassisConfig{
			Cluster: &ClusterConfig{
				Enabled: old.Chassis.Cluster.Enabled,
				Nodes:   make(map[string]*ClusterNode),
			},
		}
		for name, node := range old.Chassis.Cluster.Nodes {
			if node == nil {
				continue
			}
			c.Chassis.Cluster.Nodes[name] = &ClusterNode{
				Address:  node.Address,
				Priority: node.Priority,
			}
		}
		if old.Chassis.Cluster.Sync != nil && old.Chassis.Cluster.Sync.Etcd != nil {
			c.Chassis.Cluster.Sync = &ClusterSyncConfig{
				Etcd: &EtcdSyncConfig{Endpoints: append([]string{}, old.Chassis.Cluster.Sync.Etcd.Endpoints...)},
			}
		}
	}

	// Interfaces
	for name, iface := range old.Interfaces {
		ic := &InterfaceConfig{
			Description: iface.Description,
			Units:       make(map[int]*Unit),
		}
		for unitNum, unit := range iface.Units {
			u := &Unit{Family: make(map[string]*AddressFamily)}
			for familyName, family := range unit.Family {
				af := &AddressFamily{
					Addresses: make([]string, len(family.Addresses)),
				}
				copy(af.Addresses, family.Addresses)
				u.Family[familyName] = af
			}
			ic.Units[unitNum] = u
		}
		c.Interfaces[name] = ic
	}

	// Routing options
	if old.RoutingOptions != nil {
		c.Routing = &RoutingConfig{
			AutonomousSystem: old.RoutingOptions.AutonomousSystem,
			RouterID:         old.RoutingOptions.RouterID,
		}
		for _, sr := range old.RoutingOptions.StaticRoutes {
			c.Routing.StaticRoutes = append(c.Routing.StaticRoutes, &StaticRoute{
				Prefix:   sr.Prefix,
				NextHop:  sr.NextHop,
				Distance: sr.Distance,
			})
		}
	}

	// Protocols
	if old.Protocols != nil {
		c.Protocols = &ProtocolsConfig{}

		if old.Protocols.BGP != nil {
			c.Protocols.BGP = &BGPConfig{
				Groups: make(map[string]*BGPGroup),
			}
			for gName, g := range old.Protocols.BGP.Groups {
				bg := &BGPGroup{
					Type:      g.Type,
					Import:    g.Import,
					Export:    g.Export,
					Neighbors: make(map[string]*BGPNeighbor),
				}
				for _, n := range g.Neighbors {
					bg.Neighbors[n.IP] = &BGPNeighbor{
						PeerAS:       n.PeerAS,
						Description:  n.Description,
						LocalAddress: n.LocalAddress,
					}
				}
				c.Protocols.BGP.Groups[gName] = bg
			}
		}

		if old.Protocols.OSPF != nil {
			c.Protocols.OSPF = &OSPFConfig{
				RouterID: old.Protocols.OSPF.RouterID,
				Areas:    make(map[string]*OSPFArea),
			}
			for aID, a := range old.Protocols.OSPF.Areas {
				area := &OSPFArea{
					Interfaces: make(map[string]*OSPFInterface),
				}
				for iName, i := range a.Interfaces {
					oi := &OSPFInterface{
						Passive: i.Passive,
						Metric:  i.Metric,
					}
					if i.PrioritySet || i.Priority != 0 {
						p := i.Priority
						oi.Priority = &p
					}
					area.Interfaces[iName] = oi
				}
				c.Protocols.OSPF.Areas[aID] = area
			}
		}
		if old.Protocols.MPLS != nil {
			c.Protocols.MPLS = &MPLSConfig{Interfaces: append([]string{}, old.Protocols.MPLS.Interfaces...)}
		}
		if old.Protocols.VRRP != nil {
			c.Protocols.VRRP = &VRRPConfig{Groups: make(map[string]*VRRPGroup)}
			for name, group := range old.Protocols.VRRP.Groups {
				if group == nil {
					continue
				}
				c.Protocols.VRRP.Groups[name] = &VRRPGroup{
					Interface:      group.Interface,
					VirtualAddress: group.VirtualAddress,
					Priority:       group.Priority,
					Preempt:        group.Preempt,
				}
			}
		}
	}

	if old.RoutingInstances != nil {
		c.RoutingInstances = make(map[string]*RoutingInstance, len(old.RoutingInstances))
		for name, instance := range old.RoutingInstances {
			if instance == nil {
				continue
			}
			c.RoutingInstances[name] = &RoutingInstance{
				InstanceType:       instance.InstanceType,
				RouteDistinguisher: instance.RouteDistinguisher,
				VRFTarget:          instance.VRFTarget,
				Interfaces:         append([]string{}, instance.Interfaces...),
			}
		}
	}

	// Policy
	if old.PolicyOptions != nil {
		c.Policy = &PolicyConfig{
			PrefixLists:      make(map[string]*PrefixList),
			PolicyStatements: make(map[string]*PolicyStatement),
		}
		for name, pl := range old.PolicyOptions.PrefixLists {
			c.Policy.PrefixLists[name] = &PrefixList{
				Prefixes: append([]string{}, pl.Prefixes...),
			}
		}
		for name, ps := range old.PolicyOptions.PolicyStatements {
			stmt := &PolicyStatement{}
			for _, t := range ps.Terms {
				term := &PolicyTerm{Name: t.Name}
				if t.From != nil {
					term.From = &PolicyMatchConditions{
						PrefixLists: append([]string{}, t.From.PrefixLists...),
						Protocol:    t.From.Protocol,
						Neighbor:    t.From.Neighbor,
						ASPath:      t.From.ASPath,
					}
				}
				if t.Then != nil {
					term.Then = &PolicyActions{
						Accept:          t.Then.Accept,
						LocalPreference: t.Then.LocalPreference,
						Community:       t.Then.Community,
					}
				}
				stmt.Terms = append(stmt.Terms, term)
			}
			c.Policy.PolicyStatements[name] = stmt
		}
	}

	// Security
	if old.Security != nil {
		c.Security = &SecurityConfig{}
		if old.Security.NETCONF != nil && old.Security.NETCONF.SSH != nil {
			c.Security.NETCONF = &NETCONFSecurityConfig{
				SSH: &NETCONFSSHConfig{Port: old.Security.NETCONF.SSH.Port},
			}
		}
		if old.Security.Users != nil {
			c.Security.Users = make(map[string]*UserConfig)
			for uname, u := range old.Security.Users {
				c.Security.Users[uname] = &UserConfig{
					Password: u.Password,
					Role:     u.Role,
					SSHKey:   u.SSHKey,
				}
			}
		}
		if old.Security.RateLimit != nil {
			c.Security.RateLimit = &RateLimitConfig{
				PerIP:   old.Security.RateLimit.PerIP,
				PerUser: old.Security.RateLimit.PerUser,
			}
		}
	}

	if old.ClassOfService != nil {
		c.ClassOfService = &ClassOfServiceConfig{
			ForwardingClasses:      make(map[string]*ForwardingClass),
			TrafficControlProfiles: make(map[string]*TrafficControlProfile),
			Interfaces:             make(map[string]*CoSInterface),
		}
		for name, fc := range old.ClassOfService.ForwardingClasses {
			if fc == nil {
				continue
			}
			c.ClassOfService.ForwardingClasses[name] = &ForwardingClass{Queue: fc.Queue}
		}
		for name, profile := range old.ClassOfService.TrafficControlProfiles {
			if profile == nil {
				continue
			}
			c.ClassOfService.TrafficControlProfiles[name] = &TrafficControlProfile{
				ShapingRate:  profile.ShapingRate,
				SchedulerMap: profile.SchedulerMap,
			}
		}
		for name, iface := range old.ClassOfService.Interfaces {
			if iface == nil {
				continue
			}
			c.ClassOfService.Interfaces[name] = &CoSInterface{OutputTrafficControlProfile: iface.OutputTrafficControlProfile}
		}
	}

	return c
}

// ToLegacyConfig converts the new internal model back to the legacy pkg/config.Config.
// This is used during the migration period when some subsystems still expect the old type.
func (c *RouterConfig) ToLegacyConfig() *config.Config {
	if c == nil {
		return config.NewConfig()
	}

	old := config.NewConfig()

	// System
	if c.System != nil {
		old.System = &config.SystemConfig{HostName: c.System.HostName}
		if c.System.Services != nil && c.System.Services.WebUI != nil {
			old.System.Services = &config.SystemServicesConfig{
				WebUI: &config.WebUIConfig{
					Enabled:       c.System.Services.WebUI.Enabled,
					ListenAddress: c.System.Services.WebUI.ListenAddress,
					Port:          c.System.Services.WebUI.Port,
				},
			}
		}
	}

	if c.Chassis != nil && c.Chassis.Cluster != nil {
		old.Chassis = &config.ChassisConfig{
			Cluster: &config.ClusterConfig{
				Enabled: c.Chassis.Cluster.Enabled,
				Nodes:   make(map[string]*config.ClusterNode),
			},
		}
		for name, node := range c.Chassis.Cluster.Nodes {
			if node == nil {
				continue
			}
			old.Chassis.Cluster.Nodes[name] = &config.ClusterNode{
				Name:     name,
				Address:  node.Address,
				Priority: node.Priority,
			}
		}
		if c.Chassis.Cluster.Sync != nil && c.Chassis.Cluster.Sync.Etcd != nil {
			old.Chassis.Cluster.Sync = &config.ClusterSyncConfig{
				Etcd: &config.EtcdSyncConfig{Endpoints: append([]string{}, c.Chassis.Cluster.Sync.Etcd.Endpoints...)},
			}
		}
	}

	// Interfaces
	for name, ic := range c.Interfaces {
		iface := old.GetOrCreateInterface(name)
		iface.Description = ic.Description
		for unitNum, u := range ic.Units {
			unit := iface.GetOrCreateUnit(unitNum)
			for familyName, af := range u.Family {
				family := unit.GetOrCreateFamily(familyName)
				family.Addresses = append(family.Addresses, af.Addresses...)
			}
		}
	}

	// Routing
	if c.Routing != nil {
		old.RoutingOptions = &config.RoutingOptions{
			AutonomousSystem: c.Routing.AutonomousSystem,
			RouterID:         c.Routing.RouterID,
		}
		for _, sr := range c.Routing.StaticRoutes {
			old.RoutingOptions.StaticRoutes = append(old.RoutingOptions.StaticRoutes, &config.StaticRoute{
				Prefix:   sr.Prefix,
				NextHop:  sr.NextHop,
				Distance: sr.Distance,
			})
		}
	}

	// Protocols
	if c.Protocols != nil {
		old.Protocols = &config.ProtocolConfig{}
		if c.Protocols.BGP != nil {
			old.Protocols.BGP = &config.BGPConfig{
				Groups: make(map[string]*config.BGPGroup),
			}
			for gName, g := range c.Protocols.BGP.Groups {
				bg := &config.BGPGroup{
					Type:      g.Type,
					Import:    g.Import,
					Export:    g.Export,
					Neighbors: make(map[string]*config.BGPNeighbor),
				}
				for ip, n := range g.Neighbors {
					bg.Neighbors[ip] = &config.BGPNeighbor{
						IP:           ip,
						PeerAS:       n.PeerAS,
						Description:  n.Description,
						LocalAddress: n.LocalAddress,
					}
				}
				old.Protocols.BGP.Groups[gName] = bg
			}
		}
		if c.Protocols.OSPF != nil {
			old.Protocols.OSPF = &config.OSPFConfig{
				RouterID: c.Protocols.OSPF.RouterID,
				Areas:    make(map[string]*config.OSPFArea),
			}
			for aID, a := range c.Protocols.OSPF.Areas {
				area := &config.OSPFArea{
					AreaID:     aID,
					Interfaces: make(map[string]*config.OSPFInterface),
				}
				for iName, i := range a.Interfaces {
					oi := &config.OSPFInterface{
						Name:    iName,
						Passive: i.Passive,
						Metric:  i.Metric,
					}
					if i.Priority != nil {
						oi.Priority = *i.Priority
						oi.PrioritySet = true
					}
					area.Interfaces[iName] = oi
				}
				old.Protocols.OSPF.Areas[aID] = area
			}
		}
		if c.Protocols.MPLS != nil {
			old.Protocols.MPLS = &config.MPLSConfig{Interfaces: append([]string{}, c.Protocols.MPLS.Interfaces...)}
		}
		if c.Protocols.VRRP != nil {
			old.Protocols.VRRP = &config.VRRPConfig{Groups: make(map[string]*config.VRRPGroup)}
			for name, group := range c.Protocols.VRRP.Groups {
				if group == nil {
					continue
				}
				old.Protocols.VRRP.Groups[name] = &config.VRRPGroup{
					Name:           name,
					Interface:      group.Interface,
					VirtualAddress: group.VirtualAddress,
					Priority:       group.Priority,
					Preempt:        group.Preempt,
				}
			}
		}
	}

	if c.RoutingInstances != nil {
		old.RoutingInstances = make(map[string]*config.RoutingInstance, len(c.RoutingInstances))
		for name, instance := range c.RoutingInstances {
			if instance == nil {
				continue
			}
			old.RoutingInstances[name] = &config.RoutingInstance{
				Name:               name,
				InstanceType:       instance.InstanceType,
				RouteDistinguisher: instance.RouteDistinguisher,
				VRFTarget:          instance.VRFTarget,
				Interfaces:         append([]string{}, instance.Interfaces...),
			}
		}
	}

	// Policy
	if c.Policy != nil {
		old.PolicyOptions = &config.PolicyOptions{
			PrefixLists:      make(map[string]*config.PrefixList),
			PolicyStatements: make(map[string]*config.PolicyStatement),
		}
		for name, pl := range c.Policy.PrefixLists {
			old.PolicyOptions.PrefixLists[name] = &config.PrefixList{
				Name:     name,
				Prefixes: append([]string{}, pl.Prefixes...),
			}
		}
		for name, ps := range c.Policy.PolicyStatements {
			stmt := &config.PolicyStatement{Name: name}
			for _, t := range ps.Terms {
				term := &config.PolicyTerm{Name: t.Name}
				if t.From != nil {
					term.From = &config.PolicyMatchConditions{
						PrefixLists: append([]string{}, t.From.PrefixLists...),
						Protocol:    t.From.Protocol,
						Neighbor:    t.From.Neighbor,
						ASPath:      t.From.ASPath,
					}
				}
				if t.Then != nil {
					term.Then = &config.PolicyActions{
						Accept:          t.Then.Accept,
						LocalPreference: t.Then.LocalPreference,
						Community:       t.Then.Community,
					}
				}
				stmt.Terms = append(stmt.Terms, term)
			}
			old.PolicyOptions.PolicyStatements[name] = stmt
		}
	}

	// Security
	if c.Security != nil {
		old.Security = &config.SecurityConfig{}
		if c.Security.NETCONF != nil {
			old.Security.NETCONF = &config.NETCONFConfig{}
			if c.Security.NETCONF.SSH != nil {
				old.Security.NETCONF.SSH = &config.NETCONFSSHConfig{
					Port: c.Security.NETCONF.SSH.Port,
				}
			}
		}
		if c.Security.Users != nil {
			old.Security.Users = make(map[string]*config.UserConfig)
			for uname, u := range c.Security.Users {
				old.Security.Users[uname] = &config.UserConfig{
					Username: uname,
					Password: u.Password,
					Role:     u.Role,
					SSHKey:   u.SSHKey,
				}
			}
		}
		if c.Security.RateLimit != nil {
			old.Security.RateLimit = &config.RateLimitConfig{
				PerIP:   c.Security.RateLimit.PerIP,
				PerUser: c.Security.RateLimit.PerUser,
			}
		}
	}

	if c.ClassOfService != nil {
		old.ClassOfService = &config.ClassOfServiceConfig{
			ForwardingClasses:      make(map[string]*config.ForwardingClass),
			TrafficControlProfiles: make(map[string]*config.TrafficControlProfile),
			Interfaces:             make(map[string]*config.CoSInterface),
		}
		for name, fc := range c.ClassOfService.ForwardingClasses {
			if fc == nil {
				continue
			}
			old.ClassOfService.ForwardingClasses[name] = &config.ForwardingClass{Name: name, Queue: fc.Queue}
		}
		for name, profile := range c.ClassOfService.TrafficControlProfiles {
			if profile == nil {
				continue
			}
			old.ClassOfService.TrafficControlProfiles[name] = &config.TrafficControlProfile{
				Name:         name,
				ShapingRate:  profile.ShapingRate,
				SchedulerMap: profile.SchedulerMap,
			}
		}
		for name, iface := range c.ClassOfService.Interfaces {
			if iface == nil {
				continue
			}
			old.ClassOfService.Interfaces[name] = &config.CoSInterface{
				Name:                        name,
				OutputTrafficControlProfile: iface.OutputTrafficControlProfile,
			}
		}
	}

	return old
}
