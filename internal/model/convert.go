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
		if old.System.Services != nil {
			services := &SystemServicesConfig{}
			if old.System.Services.WebUI != nil {
				services.WebUI = &WebUIConfig{
					Enabled:       old.System.Services.WebUI.Enabled,
					ListenAddress: old.System.Services.WebUI.ListenAddress,
					Port:          old.System.Services.WebUI.Port,
				}
			}
			if old.System.Services.Prometheus != nil {
				services.Prometheus = &PrometheusConfig{
					Enabled:       old.System.Services.Prometheus.Enabled,
					ListenAddress: old.System.Services.Prometheus.ListenAddress,
					Port:          old.System.Services.Prometheus.Port,
				}
			}
			if old.System.Services.SNMP != nil {
				services.SNMP = &SNMPConfig{
					Enabled:       old.System.Services.SNMP.Enabled,
					ListenAddress: old.System.Services.SNMP.ListenAddress,
					Port:          old.System.Services.SNMP.Port,
					Community:     old.System.Services.SNMP.Community,
				}
			}
			if services.WebUI != nil || services.Prometheus != nil || services.SNMP != nil {
				c.System.Services = services
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

		if old.Protocols.BFD != nil {
			c.Protocols.BFD = bfdFromLegacy(old.Protocols.BFD)
		}

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
						BFD:          n.BFD,
						BFDProfile:   n.BFDProfile,
					}
				}
				c.Protocols.BGP.Groups[gName] = bg
			}
		}

		if old.Protocols.OSPF != nil {
			c.Protocols.OSPF = ospfFromLegacy(old.Protocols.OSPF)
		}
		if old.Protocols.OSPF3 != nil {
			c.Protocols.OSPF3 = ospfFromLegacy(old.Protocols.OSPF3)
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
				VRFTargetImport:    append([]string{}, instance.VRFTargetImport...),
				VRFTargetExport:    append([]string{}, instance.VRFTargetExport...),
				VRFImport:          append([]string{}, instance.VRFImport...),
				VRFExport:          append([]string{}, instance.VRFExport...),
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

func ospfFromLegacy(old *config.OSPFConfig) *OSPFConfig {
	if old == nil {
		return nil
	}
	ospf := &OSPFConfig{
		RouterID: old.RouterID,
		Areas:    make(map[string]*OSPFArea),
	}
	for aID, a := range old.Areas {
		if a == nil {
			ospf.Areas[aID] = nil
			continue
		}
		area := &OSPFArea{
			Interfaces: make(map[string]*OSPFInterface),
		}
		for iName, i := range a.Interfaces {
			if i == nil {
				area.Interfaces[iName] = nil
				continue
			}
			oi := &OSPFInterface{
				Passive:    i.Passive,
				Metric:     i.Metric,
				BFD:        i.BFD,
				BFDProfile: i.BFDProfile,
			}
			if i.PrioritySet || i.Priority != 0 {
				p := i.Priority
				oi.Priority = &p
			}
			area.Interfaces[iName] = oi
		}
		ospf.Areas[aID] = area
	}
	return ospf
}

func bfdFromLegacy(old *config.BFDConfig) *BFDConfig {
	if old == nil {
		return nil
	}
	bfd := &BFDConfig{}
	if old.Profiles != nil {
		bfd.Profiles = make(map[string]*BFDProfile, len(old.Profiles))
		for name, profile := range old.Profiles {
			if profile == nil {
				bfd.Profiles[name] = nil
				continue
			}
			bfd.Profiles[name] = &BFDProfile{
				DetectMultiplier: profile.DetectMultiplier,
				ReceiveInterval:  profile.ReceiveInterval,
				TransmitInterval: profile.TransmitInterval,
				EchoMode:         profile.EchoMode,
				PassiveMode:      profile.PassiveMode,
			}
		}
	}
	if old.Peers != nil {
		bfd.Peers = make(map[string]*BFDPeer, len(old.Peers))
		for address, peer := range old.Peers {
			if peer == nil {
				bfd.Peers[address] = nil
				continue
			}
			bfd.Peers[address] = &BFDPeer{
				LocalAddress:     peer.LocalAddress,
				Interface:        peer.Interface,
				VRF:              peer.VRF,
				Multihop:         peer.Multihop,
				Profile:          peer.Profile,
				DetectMultiplier: peer.DetectMultiplier,
				ReceiveInterval:  peer.ReceiveInterval,
				TransmitInterval: peer.TransmitInterval,
				EchoMode:         peer.EchoMode,
				PassiveMode:      peer.PassiveMode,
				Shutdown:         peer.Shutdown,
			}
		}
	}
	return bfd
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
		if c.System.Services != nil {
			services := &config.SystemServicesConfig{}
			if c.System.Services.WebUI != nil {
				services.WebUI = &config.WebUIConfig{
					Enabled:       c.System.Services.WebUI.Enabled,
					ListenAddress: c.System.Services.WebUI.ListenAddress,
					Port:          c.System.Services.WebUI.Port,
				}
			}
			if c.System.Services.Prometheus != nil {
				services.Prometheus = &config.PrometheusConfig{
					Enabled:       c.System.Services.Prometheus.Enabled,
					ListenAddress: c.System.Services.Prometheus.ListenAddress,
					Port:          c.System.Services.Prometheus.Port,
				}
			}
			if c.System.Services.SNMP != nil {
				services.SNMP = &config.SNMPConfig{
					Enabled:       c.System.Services.SNMP.Enabled,
					ListenAddress: c.System.Services.SNMP.ListenAddress,
					Port:          c.System.Services.SNMP.Port,
					Community:     c.System.Services.SNMP.Community,
				}
			}
			if services.WebUI != nil || services.Prometheus != nil || services.SNMP != nil {
				old.System.Services = services
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
		if c.Protocols.BFD != nil {
			old.Protocols.BFD = bfdToLegacy(c.Protocols.BFD)
		}
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
						BFD:          n.BFD,
						BFDProfile:   n.BFDProfile,
					}
				}
				old.Protocols.BGP.Groups[gName] = bg
			}
		}
		if c.Protocols.OSPF != nil {
			old.Protocols.OSPF = ospfToLegacy(c.Protocols.OSPF)
		}
		if c.Protocols.OSPF3 != nil {
			old.Protocols.OSPF3 = ospfToLegacy(c.Protocols.OSPF3)
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
				VRFTargetImport:    append([]string{}, instance.VRFTargetImport...),
				VRFTargetExport:    append([]string{}, instance.VRFTargetExport...),
				VRFImport:          append([]string{}, instance.VRFImport...),
				VRFExport:          append([]string{}, instance.VRFExport...),
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

func ospfToLegacy(c *OSPFConfig) *config.OSPFConfig {
	if c == nil {
		return nil
	}
	ospf := &config.OSPFConfig{
		RouterID: c.RouterID,
		Areas:    make(map[string]*config.OSPFArea),
	}
	for aID, a := range c.Areas {
		if a == nil {
			ospf.Areas[aID] = nil
			continue
		}
		area := &config.OSPFArea{
			AreaID:     aID,
			Interfaces: make(map[string]*config.OSPFInterface),
		}
		for iName, i := range a.Interfaces {
			if i == nil {
				area.Interfaces[iName] = nil
				continue
			}
			oi := &config.OSPFInterface{
				Name:       iName,
				Passive:    i.Passive,
				Metric:     i.Metric,
				BFD:        i.BFD,
				BFDProfile: i.BFDProfile,
			}
			if i.Priority != nil {
				oi.Priority = *i.Priority
				oi.PrioritySet = true
			}
			area.Interfaces[iName] = oi
		}
		ospf.Areas[aID] = area
	}
	return ospf
}

func bfdToLegacy(c *BFDConfig) *config.BFDConfig {
	if c == nil {
		return nil
	}
	bfd := &config.BFDConfig{}
	if c.Profiles != nil {
		bfd.Profiles = make(map[string]*config.BFDProfile, len(c.Profiles))
		for name, profile := range c.Profiles {
			if profile == nil {
				bfd.Profiles[name] = nil
				continue
			}
			bfd.Profiles[name] = &config.BFDProfile{
				Name:             name,
				DetectMultiplier: profile.DetectMultiplier,
				ReceiveInterval:  profile.ReceiveInterval,
				TransmitInterval: profile.TransmitInterval,
				EchoMode:         profile.EchoMode,
				PassiveMode:      profile.PassiveMode,
			}
		}
	}
	if c.Peers != nil {
		bfd.Peers = make(map[string]*config.BFDPeer, len(c.Peers))
		for address, peer := range c.Peers {
			if peer == nil {
				bfd.Peers[address] = nil
				continue
			}
			bfd.Peers[address] = &config.BFDPeer{
				Address:          address,
				LocalAddress:     peer.LocalAddress,
				Interface:        peer.Interface,
				VRF:              peer.VRF,
				Multihop:         peer.Multihop,
				Profile:          peer.Profile,
				DetectMultiplier: peer.DetectMultiplier,
				ReceiveInterval:  peer.ReceiveInterval,
				TransmitInterval: peer.TransmitInterval,
				EchoMode:         peer.EchoMode,
				PassiveMode:      peer.PassiveMode,
				Shutdown:         peer.Shutdown,
			}
		}
	}
	return bfd
}
