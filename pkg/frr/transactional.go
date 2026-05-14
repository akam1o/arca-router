package frr

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// MgmtOperationType is a single FRR management operation.
type MgmtOperationType string

const (
	MgmtOperationSet    MgmtOperationType = "set"
	MgmtOperationDelete MgmtOperationType = "delete"
)

// MgmtOperation represents one operation against the FRR management candidate datastore.
type MgmtOperation struct {
	Type  MgmtOperationType
	XPath string
	Value string
}

// Command renders the operation as a vtysh management command.
func (o MgmtOperation) Command() string {
	switch o.Type {
	case MgmtOperationDelete:
		return "mgmt delete-config " + o.XPath
	default:
		return "mgmt set-config " + o.XPath + " " + quoteMgmtValue(o.Value)
	}
}

// MgmtClient applies management operations transactionally.
type MgmtClient interface {
	Apply(ctx context.Context, ops []MgmtOperation) error
}

// VtyshRunner executes one vtysh command.
type VtyshRunner func(ctx context.Context, command string) ([]byte, error)

// VtyshMgmtClient uses vtysh's mgmt commands as the first transactional backend.
type VtyshMgmtClient struct {
	run VtyshRunner
}

// NewVtyshMgmtClient creates a vtysh-backed management client.
func NewVtyshMgmtClient() *VtyshMgmtClient {
	return &VtyshMgmtClient{run: runVtyshMgmtCommand}
}

// NewVtyshMgmtClientWithRunner creates a client with a test runner.
func NewVtyshMgmtClientWithRunner(run VtyshRunner) *VtyshMgmtClient {
	return &VtyshMgmtClient{run: run}
}

// Apply loads all operations into FRR candidate, checks them, and commits them.
func (c *VtyshMgmtClient) Apply(ctx context.Context, ops []MgmtOperation) error {
	if c.run == nil {
		c.run = runVtyshMgmtCommand
	}
	if _, err := c.run(ctx, "mgmt commit abort"); err != nil {
		return NewApplyError("abort stale FRR management candidate", err)
	}
	for _, op := range ops {
		if _, err := c.run(ctx, op.Command()); err != nil {
			_, _ = c.run(ctx, "mgmt commit abort")
			return NewApplyError(fmt.Sprintf("apply FRR management operation %q", op.Command()), err)
		}
	}
	if _, err := c.run(ctx, "mgmt commit check"); err != nil {
		_, _ = c.run(ctx, "mgmt commit abort")
		return NewValidateError("FRR management commit check failed", err)
	}
	if _, err := c.run(ctx, "mgmt commit apply"); err != nil {
		_, _ = c.run(ctx, "mgmt commit abort")
		return NewApplyError("FRR management commit apply failed", err)
	}
	if _, err := c.run(ctx, "write memory"); err != nil {
		return NewApplyError("persist FRR running configuration", err)
	}
	return nil
}

func runVtyshMgmtCommand(ctx context.Context, command string) ([]byte, error) {
	vtyshPath, err := exec.LookPath("vtysh")
	if err != nil {
		return nil, NewToolNotFoundError("vtysh")
	}
	cmd := exec.CommandContext(ctx, vtyshPath, "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return output, nil
}

// TransactionalApplier applies FRR config through the management candidate datastore.
type TransactionalApplier struct {
	client       MgmtClient
	vrrpPreparer VRRPSystemPreparer
}

// NewTransactionalApplier creates a transactional FRR applier.
func NewTransactionalApplier(client MgmtClient) *TransactionalApplier {
	return NewTransactionalApplierWithPreparer(client, NewIPVRRPSystemPreparer(nil))
}

// NewTransactionalApplierWithPreparer creates an applier with a custom VRRP preparer.
func NewTransactionalApplierWithPreparer(client MgmtClient, preparer VRRPSystemPreparer) *TransactionalApplier {
	if client == nil {
		client = NewVtyshMgmtClient()
	}
	return &TransactionalApplier{client: client, vrrpPreparer: preparer}
}

// ApplyConfig converts the generated FRR config into management operations and commits them.
func (a *TransactionalApplier) ApplyConfig(ctx context.Context, _ string, cfg *Config) error {
	if err := prepareVRRPSystem(ctx, a.vrrpPreparer, cfg); err != nil {
		return err
	}
	ops, err := BuildMgmtOperations(cfg)
	if err != nil {
		return err
	}
	return a.client.Apply(ctx, ops)
}

// BuildMgmtOperations converts a complete generated FRR config into a deterministic
// subtree-replace operation set for FRR's management candidate datastore.
func BuildMgmtOperations(cfg *Config) ([]MgmtOperation, error) {
	if cfg == nil {
		return nil, NewInvalidConfigError("FRR config is nil")
	}
	if cfg.OSPF3 != nil {
		return nil, NewInvalidConfigError("OSPFv3 is not supported by the transactional FRR backend because FRR does not expose core ospf6d YANG paths")
	}
	if cfg.BFD != nil {
		return nil, NewInvalidConfigError("BFD is not supported by the transactional FRR backend until frr-bfdd management operations are implemented")
	}
	var ops []MgmtOperation
	ops = append(ops,
		deleteOp(staticProtocolBase()),
		deleteOp(bgpProtocolDeleteBase()),
		deleteOp(ospfProtocolBase()),
		deleteOp("/frr-vrf:lib"),
		deleteOp("/frr-filter:lib"),
		deleteOp("/frr-route-map:lib"),
		deleteOp(vrrpConfigBase()),
	)
	ops = append(ops, buildStaticRouteOps(cfg.StaticRoutes)...)
	ops = append(ops, buildBGPOps(cfg.BGP)...)
	ops = append(ops, buildOSPFOps(cfg.OSPF)...)
	ops = append(ops, buildPrefixListOps(cfg.PrefixLists)...)
	ops = append(ops, buildRouteMapOps(cfg.RouteMaps)...)
	ops = append(ops, buildVRFOps(cfg.VRFs)...)
	vrrpOps, err := buildVRRPOps(cfg.VRRP)
	if err != nil {
		return nil, err
	}
	ops = append(ops, vrrpOps...)
	return ops, nil
}

func buildStaticRouteOps(routes []StaticRoute) []MgmtOperation {
	if len(routes) == 0 {
		return nil
	}
	ops := protocolCreateOps(staticProtocolBase(), "frr-staticd:staticd", "staticd")
	sortedRoutes := append([]StaticRoute(nil), routes...)
	sort.Slice(sortedRoutes, func(i, j int) bool {
		if sortedRoutes[i].Prefix != sortedRoutes[j].Prefix {
			return sortedRoutes[i].Prefix < sortedRoutes[j].Prefix
		}
		return sortedRoutes[i].NextHop < sortedRoutes[j].NextHop
	})
	for _, route := range sortedRoutes {
		afiSafi := "frr-routing:ipv4-unicast"
		nhType := "ip4"
		srcPrefix := "::/0"
		if route.IsIPv6 {
			afiSafi = "frr-routing:ipv6-unicast"
			nhType = "ip6"
		}
		routeBase := staticProtocolBase() + "/frr-staticd:staticd/route-list" +
			keyPred("prefix", route.Prefix) + keyPred("src-prefix", srcPrefix) + keyPred("afi-safi", afiSafi)
		ops = append(ops,
			setOp(routeBase+"/prefix", route.Prefix),
			setOp(routeBase+"/src-prefix", srcPrefix),
			setOp(routeBase+"/afi-safi", afiSafi),
		)
		pathBase := routeBase + "/path-list" +
			keyPred("table-id", "0") +
			keyPred("nh-type", nhType) +
			keyPred("vrf", defaultVRFName) +
			keyPred("gateway", route.NextHop) +
			keyPred("interface", "")
		distance := route.Distance
		if distance == 0 {
			distance = 1
		}
		ops = append(ops,
			setOp(pathBase+"/table-id", "0"),
			setOp(pathBase+"/nh-type", nhType),
			setOp(pathBase+"/vrf", defaultVRFName),
			setOp(pathBase+"/gateway", route.NextHop),
			setOp(pathBase+"/interface", ""),
			setOp(pathBase+"/distance", strconv.Itoa(distance)),
		)
	}
	return ops
}

func buildBGPOps(cfg *BGPConfig) []MgmtOperation {
	if cfg == nil {
		return nil
	}
	ops := protocolCreateOps(bgpProtocolBase(), "frr-bgp:bgp", "bgp")
	ops = append(ops, setOp(bgpProtocolBase()+"/frr-bgp:bgp/global/local-as", strconv.FormatUint(uint64(cfg.ASN), 10)))
	if cfg.RouterID != "" {
		ops = append(ops, setOp(bgpProtocolBase()+"/frr-bgp:bgp/global/router-id", cfg.RouterID))
	}
	neighbors := append([]BGPNeighbor(nil), cfg.Neighbors...)
	sort.Slice(neighbors, func(i, j int) bool { return neighbors[i].IP < neighbors[j].IP })
	for _, neighbor := range neighbors {
		base := bgpProtocolBase() + "/frr-bgp:bgp/neighbors/neighbor" + keyPred("remote-address", neighbor.IP)
		ops = append(ops,
			setOp(base+"/remote-address", neighbor.IP),
			setOp(base+"/neighbor-remote-as/remote-as-type", "as-specified"),
			setOp(base+"/neighbor-remote-as/remote-as", strconv.FormatUint(uint64(neighbor.RemoteAS), 10)),
		)
		if neighbor.Description != "" {
			ops = append(ops, setOp(base+"/description", neighbor.Description))
		}
		if neighbor.UpdateSource != "" {
			if net.ParseIP(neighbor.UpdateSource) != nil {
				ops = append(ops, setOp(base+"/update-source/ip", neighbor.UpdateSource))
			} else {
				ops = append(ops, setOp(base+"/update-source/interface", neighbor.UpdateSource))
			}
		}
		afi := "frr-routing:ipv4-unicast"
		afiContainer := "ipv4-unicast"
		if neighbor.IsIPv6 {
			afi = "frr-routing:ipv6-unicast"
			afiContainer = "ipv6-unicast"
		}
		afiBase := base + "/afi-safis/afi-safi" + keyPred("afi-safi-name", afi)
		ops = append(ops,
			setOp(afiBase+"/afi-safi-name", afi),
			setOp(afiBase+"/enabled", "true"),
		)
		if neighbor.RouteMapIn != "" {
			ops = append(ops, setOp(afiBase+"/"+afiContainer+"/filter-config/rmap-import", neighbor.RouteMapIn))
		}
		if neighbor.RouteMapOut != "" {
			ops = append(ops, setOp(afiBase+"/"+afiContainer+"/filter-config/rmap-export", neighbor.RouteMapOut))
		}
	}
	return ops
}

func buildOSPFOps(cfg *OSPFConfig) []MgmtOperation {
	if cfg == nil {
		return nil
	}
	ops := protocolCreateOps(ospfProtocolBase(), "frr-ospfd:ospf", "ospf")
	if cfg.RouterID != "" {
		ops = append(ops, setOp(ospfProtocolBase()+"/frr-ospfd:ospf/router-id", cfg.RouterID))
	}
	networks := append([]OSPFNetwork(nil), cfg.Networks...)
	sort.Slice(networks, func(i, j int) bool { return networks[i].Prefix < networks[j].Prefix })
	for _, network := range networks {
		base := ospfProtocolBase() + "/frr-ospfd:ospf/network" + keyPred("prefix", network.Prefix)
		ops = append(ops, setOp(base+"/prefix", network.Prefix), setOp(base+"/area", network.AreaID))
	}
	interfaces := append([]OSPFInterface(nil), cfg.Interfaces...)
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Name < interfaces[j].Name })
	for _, iface := range interfaces {
		if iface.Passive {
			base := ospfProtocolBase() + "/frr-ospfd:ospf/passive-interface" + keyPred("interface", iface.Name)
			ops = append(ops, setOp(base+"/interface", iface.Name))
		}
	}
	return ops
}

func buildPrefixListOps(prefixLists []PrefixList) []MgmtOperation {
	if len(prefixLists) == 0 {
		return nil
	}
	var ops []MgmtOperation
	lists := append([]PrefixList(nil), prefixLists...)
	sort.Slice(lists, func(i, j int) bool {
		if lists[i].IsIPv6 != lists[j].IsIPv6 {
			return !lists[i].IsIPv6
		}
		return lists[i].Name < lists[j].Name
	})
	for _, list := range lists {
		listType := "ipv4"
		prefixLeaf := "ipv4-prefix"
		if list.IsIPv6 {
			listType = "ipv6"
			prefixLeaf = "ipv6-prefix"
		}
		base := "/frr-filter:lib/prefix-list" + keyPred("type", listType) + keyPred("name", list.Name)
		ops = append(ops, setOp(base+"/type", listType), setOp(base+"/name", list.Name))
		entries := append([]PrefixListEntry(nil), list.Entries...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
		for _, entry := range entries {
			entryBase := base + "/entry" + keyPred("sequence", strconv.Itoa(entry.Seq))
			ops = append(ops,
				setOp(entryBase+"/sequence", strconv.Itoa(entry.Seq)),
				setOp(entryBase+"/action", entry.Action),
				setOp(entryBase+"/"+prefixLeaf, entry.Prefix),
			)
		}
	}
	return ops
}

func buildRouteMapOps(routeMaps []RouteMap) []MgmtOperation {
	if len(routeMaps) == 0 {
		return nil
	}
	var ops []MgmtOperation
	maps := append([]RouteMap(nil), routeMaps...)
	sort.Slice(maps, func(i, j int) bool { return maps[i].Name < maps[j].Name })
	for _, routeMap := range maps {
		base := "/frr-route-map:lib/route-map" + keyPred("name", routeMap.Name)
		ops = append(ops, setOp(base+"/name", routeMap.Name))
		entries := append([]RouteMapEntry(nil), routeMap.Entries...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
		for _, entry := range entries {
			entryBase := base + "/entry" + keyPred("sequence", strconv.Itoa(entry.Seq))
			ops = append(ops,
				setOp(entryBase+"/sequence", strconv.Itoa(entry.Seq)),
				setOp(entryBase+"/action", entry.Action),
			)
			for _, prefixList := range entry.MatchPrefixLists {
				matchBase := entryBase + "/match-condition" + keyPred("condition", "frr-route-map:ipv4-prefix-list")
				ops = append(ops,
					setOp(matchBase+"/condition", "frr-route-map:ipv4-prefix-list"),
					setOp(matchBase+"/rmap-match-condition/list-name", prefixList),
				)
			}
			if entry.SetLocalPreference != nil {
				setBase := entryBase + "/set-action" + keyPred("action", "frr-bgp-route-map:set-local-preference")
				ops = append(ops,
					setOp(setBase+"/action", "frr-bgp-route-map:set-local-preference"),
					setOp(setBase+"/rmap-set-action/local-pref", strconv.FormatUint(uint64(*entry.SetLocalPreference), 10)),
				)
			}
			if entry.SetCommunity != "" {
				setBase := entryBase + "/set-action" + keyPred("action", "frr-bgp-route-map:set-community")
				ops = append(ops,
					setOp(setBase+"/action", "frr-bgp-route-map:set-community"),
					setOp(setBase+"/rmap-set-action/community-string", entry.SetCommunity),
				)
			}
		}
	}
	return ops
}

func buildVRRPOps(cfg *VRRPConfig) ([]MgmtOperation, error) {
	if cfg == nil || len(cfg.Groups) == 0 {
		return nil, nil
	}
	groups := append([]VRRPGroup(nil), cfg.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Interface != groups[j].Interface {
			return groups[i].Interface < groups[j].Interface
		}
		return groups[i].ID < groups[j].ID
	})

	var ops []MgmtOperation
	createdInterfaces := make(map[string]bool)
	for _, group := range groups {
		if group.Interface == "" {
			return nil, NewInvalidConfigError(fmt.Sprintf("VRRP group %d missing interface", group.ID))
		}
		if group.ID < 1 || group.ID > 255 {
			return nil, NewInvalidConfigError(fmt.Sprintf("VRRP group id must be 1-255, got %d", group.ID))
		}
		virtualAddress := net.ParseIP(group.VirtualAddress)
		if virtualAddress == nil {
			return nil, NewInvalidConfigError(fmt.Sprintf("VRRP group %d has invalid virtual address %q", group.ID, group.VirtualAddress))
		}
		if group.Priority < 0 || group.Priority > 254 {
			return nil, NewInvalidConfigError(fmt.Sprintf("VRRP group %d priority must be 1-254 when configured, got %d", group.ID, group.Priority))
		}
		if !createdInterfaces[group.Interface] {
			ops = append(ops, setOp(interfaceBase(group.Interface)+"/name", group.Interface))
			createdInterfaces[group.Interface] = true
		}
		id := strconv.Itoa(group.ID)
		groupBase := vrrpGroupBase(group.Interface, id)
		ops = append(ops,
			setOp(groupBase+"/virtual-router-id", id),
			setOp(groupBase+"/version", "3"),
		)
		if group.Priority != 0 {
			ops = append(ops, setOp(groupBase+"/priority", strconv.Itoa(group.Priority)))
		}
		if group.Preempt {
			ops = append(ops, setOp(groupBase+"/preempt", "true"))
		}
		addressFamily := "v4"
		if virtualAddress.To4() == nil {
			addressFamily = "v6"
		}
		ops = append(ops, setOp(groupBase+"/"+addressFamily+"/virtual-address", group.VirtualAddress))
	}
	return ops, nil
}

const defaultVRFName = "default"

func buildVRFOps(vrfs []VRFConfig) []MgmtOperation {
	if len(vrfs) == 0 {
		return nil
	}
	sorted := append([]VRFConfig(nil), vrfs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var ops []MgmtOperation
	for _, vrf := range sorted {
		if vrf.Name == "" {
			continue
		}
		vrfBase := "/frr-vrf:lib/vrf" + keyPred("name", vrf.Name)
		ops = append(ops, setOp(vrfBase+"/name", vrf.Name))
		if !vrfHasVPNConfig(vrf) {
			continue
		}
		base := bgpProtocolBaseForVRF(vrf.Name)
		ops = append(ops, protocolCreateOpsForVRF(base, "frr-bgp:bgp", "bgp", vrf.Name)...)
		ops = append(ops, setOp(base+"/frr-bgp:bgp/global/local-as", strconv.FormatUint(uint64(vrf.ASN), 10)))
		ops = append(ops, buildVRFAFIOps(base, "frr-routing:ipv4-unicast", "ipv4-unicast", vrf)...)
		ops = append(ops, buildVRFAFIOps(base, "frr-routing:ipv6-unicast", "ipv6-unicast", vrf)...)
	}
	return ops
}

func buildVRFAFIOps(base, afiName, afiContainer string, vrf VRFConfig) []MgmtOperation {
	afiBase := base + "/frr-bgp:bgp/global/afi-safis/afi-safi" + keyPred("afi-safi-name", afiName)
	vpnBase := afiBase + "/" + afiContainer + "/vpn-config"
	ops := []MgmtOperation{
		setOp(afiBase+"/afi-safi-name", afiName),
		setOp(afiBase+"/enabled", "true"),
	}
	if len(vrf.ExportTargets) > 0 {
		ops = append(ops,
			setOp(vpnBase+"/rd", vrf.RouteDistinguisher),
			setOp(vpnBase+"/export-vpn", "true"),
			setOp(vpnBase+"/label-auto", "true"),
		)
		for _, target := range vrf.ExportTargets {
			ops = append(ops, setOp(vpnBase+"/export-rt-list", routeTargetYANGValue(target)))
		}
	}
	if len(vrf.ImportTargets) > 0 {
		ops = append(ops, setOp(vpnBase+"/import-vpn", "true"))
		for _, target := range vrf.ImportTargets {
			ops = append(ops, setOp(vpnBase+"/import-rt-list", routeTargetYANGValue(target)))
		}
	}
	if vrf.ImportRouteMap != "" {
		ops = append(ops, setOp(vpnBase+"/rmap-import", vrf.ImportRouteMap))
	}
	if vrf.ExportRouteMap != "" {
		ops = append(ops, setOp(vpnBase+"/rmap-export", vrf.ExportRouteMap))
	}
	return ops
}

func routeTargetYANGValue(target string) string {
	if strings.HasPrefix(target, "target:") {
		return target
	}
	return "target:" + target
}

func staticProtocolBase() string {
	return protocolBase("frr-staticd:staticd", "staticd")
}

func bgpProtocolBase() string {
	return protocolBase("frr-bgp:bgp", "bgp")
}

func bgpProtocolBaseForVRF(vrf string) string {
	return protocolBaseForVRF("frr-bgp:bgp", "bgp", vrf)
}

func bgpProtocolDeleteBase() string {
	return "/frr-routing:routing/control-plane-protocols/control-plane-protocol" +
		keyPred("type", "frr-bgp:bgp")
}

func ospfProtocolBase() string {
	return protocolBase("frr-ospfd:ospf", "ospf")
}

func protocolBase(protocolType, name string) string {
	return protocolBaseForVRF(protocolType, name, defaultVRFName)
}

func protocolBaseForVRF(protocolType, name, vrf string) string {
	return "/frr-routing:routing/control-plane-protocols/control-plane-protocol" +
		keyPred("type", protocolType) + keyPred("name", name) + keyPred("vrf", vrf)
}

func protocolCreateOps(base, protocolType, name string) []MgmtOperation {
	return protocolCreateOpsForVRF(base, protocolType, name, defaultVRFName)
}

func protocolCreateOpsForVRF(base, protocolType, name, vrf string) []MgmtOperation {
	return []MgmtOperation{
		setOp(base+"/type", protocolType),
		setOp(base+"/name", name),
		setOp(base+"/vrf", vrf),
	}
}

func interfaceBase(name string) string {
	return "/frr-interface:lib/interface" + keyPred("name", name)
}

func vrrpConfigBase() string {
	return "/frr-interface:lib/interface/frr-vrrpd:vrrp"
}

func vrrpGroupBase(interfaceName, groupID string) string {
	return interfaceBase(interfaceName) + "/frr-vrrpd:vrrp/vrrp-group" + keyPred("virtual-router-id", groupID)
}

func setOp(xpath, value string) MgmtOperation {
	return MgmtOperation{Type: MgmtOperationSet, XPath: xpath, Value: value}
}

func deleteOp(xpath string) MgmtOperation {
	return MgmtOperation{Type: MgmtOperationDelete, XPath: xpath}
}

func keyPred(key, value string) string {
	return "[" + key + "='" + escapeXPathValue(value) + "']"
}

func escapeXPathValue(value string) string {
	return strings.ReplaceAll(value, "'", "&apos;")
}

func quoteMgmtValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\n\"'") {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return value
}
