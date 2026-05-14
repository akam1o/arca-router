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
	ops, err := BuildMgmtOperations(cfg)
	if err != nil {
		return err
	}
	if err := prepareVRRPSystem(ctx, a.vrrpPreparer, cfg); err != nil {
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
	if err := validateTransactionalBGP(cfg); err != nil {
		return nil, err
	}
	if err := validateTransactionalStaticRoutes(cfg); err != nil {
		return nil, err
	}
	if err := validateTransactionalBFDProtocolBindings(cfg); err != nil {
		return nil, err
	}
	if err := validateTransactionalOSPF(cfg); err != nil {
		return nil, err
	}
	if err := validateTransactionalStaticRouteBFDProfiles(cfg); err != nil {
		return nil, err
	}
	if err := validateTransactionalBFDVRFReferences(cfg); err != nil {
		return nil, err
	}
	if err := validateTransactionalRouteMapSupport(cfg); err != nil {
		return nil, err
	}
	if err := validateTransactionalPolicyObjects(cfg); err != nil {
		return nil, err
	}
	if err := validateTransactionalVRFVPN(cfg); err != nil {
		return nil, err
	}
	if err := validateTransactionalRouteMapReferences(cfg); err != nil {
		return nil, err
	}
	prefixLists, routeMaps, err := aggregateRouteMapPrefixListMatches(cfg.PrefixLists, cfg.RouteMaps)
	if err != nil {
		return nil, NewInvalidConfigError(err.Error())
	}
	var ops []MgmtOperation
	ops = append(ops,
		deleteOp(staticProtocolBase()),
		deleteOp(bgpProtocolDeleteBase()),
		deleteOp(ospfProtocolBase()),
		deleteOp(ospfInterfaceConfigBase()),
		deleteOp("/frr-vrf:lib"),
		deleteOp("/frr-filter:lib"),
		deleteOp("/frr-route-map:lib"),
		deleteOp("/frr-bfdd:bfdd"),
		deleteOp(vrrpConfigBase()),
	)
	ops = append(ops, buildStaticRouteOps(cfg.StaticRoutes)...)
	ops = append(ops, buildBGPOps(cfg.BGP)...)
	ops = append(ops, buildOSPFOps(cfg.OSPF)...)
	ops = append(ops, buildPrefixListOps(prefixLists)...)
	ops = append(ops, buildRouteMapOps(routeMaps, prefixLists)...)
	bfdOps, err := buildBFDConfigOps(cfg.BFD)
	if err != nil {
		return nil, err
	}
	ops = append(ops, bfdOps...)
	ops = append(ops, buildVRFOps(cfg.VRFs)...)
	vrrpOps, err := buildVRRPOps(cfg.VRRP)
	if err != nil {
		return nil, err
	}
	ops = append(ops, vrrpOps...)
	return ops, nil
}

func validateTransactionalBGP(cfg *Config) error {
	if cfg == nil || cfg.BGP == nil {
		return nil
	}
	return validateBGPConfig(cfg.BGP)
}

func validateTransactionalStaticRoutes(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	return validateStaticRoutes(cfg.StaticRoutes)
}

func validateTransactionalBFDProtocolBindings(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if cfg.BGP != nil {
		for _, neighbor := range cfg.BGP.Neighbors {
			if neighbor.BFDProfile != "" {
				return NewInvalidConfigError("BGP BFD profiles are not supported by the transactional FRR backend until BGP BFD profile management operations are implemented")
			}
		}
	}
	if cfg.OSPF != nil && ospfHasBFDProfiles(cfg.OSPF) {
		return NewInvalidConfigError("OSPF BFD profiles are not supported by the transactional FRR backend until OSPF BFD profile management operations are implemented")
	}
	if cfg.OSPF3 != nil && ospfHasBFDProtocolBindings(cfg.OSPF3) {
		return NewInvalidConfigError("OSPFv3 BFD protocol bindings are not supported by the transactional FRR backend until ospf6d management operations are implemented")
	}
	return nil
}

func validateTransactionalOSPF(cfg *Config) error {
	if cfg == nil || cfg.OSPF == nil {
		return nil
	}
	if cfg.OSPF.IsOSPFv3 {
		return NewInvalidConfigError("OSPFv3 is not supported by the transactional FRR backend because FRR does not expose core ospf6d YANG paths")
	}
	return validateOSPFConfig(cfg.OSPF)
}

func validateTransactionalStaticRouteBFDProfiles(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	profiles := make(map[string]struct{})
	if cfg.BFD != nil {
		for _, profile := range cfg.BFD.Profiles {
			profiles[profile.Name] = struct{}{}
		}
	}
	for _, route := range cfg.StaticRoutes {
		if route.BFDProfile == "" {
			continue
		}
		if _, ok := profiles[route.BFDProfile]; !ok {
			return NewInvalidConfigError(fmt.Sprintf("static route %s references unknown BFD profile %s", route.Prefix, route.BFDProfile))
		}
	}
	return nil
}

func validateTransactionalBFDVRFReferences(cfg *Config) error {
	if cfg == nil || cfg.BFD == nil {
		return nil
	}
	vrfs := make(map[string]struct{}, len(cfg.VRFs))
	for _, vrf := range cfg.VRFs {
		vrfs[vrf.Name] = struct{}{}
	}
	for _, peer := range cfg.BFD.Peers {
		if peer.VRF == "" || peer.VRF == defaultVRFName {
			continue
		}
		if _, ok := vrfs[peer.VRF]; !ok {
			return NewInvalidConfigError(fmt.Sprintf("BFD peer %s references unknown VRF %s", peer.Address, peer.VRF))
		}
	}
	return nil
}

func validateTransactionalPolicyObjects(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	return validatePolicyObjects(cfg.PrefixLists, cfg.RouteMaps)
}

func validPolicyAction(action string) bool {
	return action == "permit" || action == "deny"
}

func validateTransactionalRouteMapReferences(cfg *Config) error {
	return validateFRRRouteMapReferences(cfg)
}

func validateFRRRouteMapReferences(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	routeMaps := make(map[string]struct{}, len(cfg.RouteMaps))
	for _, routeMap := range cfg.RouteMaps {
		if strings.TrimSpace(routeMap.Name) == "" {
			return NewInvalidConfigError("route-map name is required")
		}
		routeMaps[routeMap.Name] = struct{}{}
	}
	if cfg.BGP != nil {
		for _, neighbor := range cfg.BGP.Neighbors {
			if err := validateRouteMapReference(routeMaps, fmt.Sprintf("BGP neighbor %s import", neighbor.IP), neighbor.RouteMapIn); err != nil {
				return err
			}
			if err := validateRouteMapReference(routeMaps, fmt.Sprintf("BGP neighbor %s export", neighbor.IP), neighbor.RouteMapOut); err != nil {
				return err
			}
		}
	}
	for _, vrf := range cfg.VRFs {
		if err := validateRouteMapReference(routeMaps, fmt.Sprintf("VRF %s import", vrf.Name), vrf.ImportRouteMap); err != nil {
			return err
		}
		if err := validateRouteMapReference(routeMaps, fmt.Sprintf("VRF %s export", vrf.Name), vrf.ExportRouteMap); err != nil {
			return err
		}
	}
	return nil
}

func validateRouteMapReference(routeMaps map[string]struct{}, context, name string) error {
	if name == "" {
		return nil
	}
	if _, ok := routeMaps[name]; !ok {
		return NewInvalidConfigError(fmt.Sprintf("%s references unknown route-map %s", context, name))
	}
	return nil
}

func validateTransactionalVRFVPN(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	for _, vrf := range cfg.VRFs {
		if strings.TrimSpace(vrf.Name) == "" {
			return NewInvalidConfigError("VRF name is required")
		}
		importTargetCount := len(vrf.ImportTargets)
		exportTargetCount := len(vrf.ExportTargets)
		for _, target := range vrf.ImportTargets {
			if !validRouteTargetValue(target) {
				return NewInvalidConfigError(fmt.Sprintf("VRF %s: invalid import route-target %s", vrf.Name, target))
			}
		}
		for _, target := range vrf.ExportTargets {
			if !validRouteTargetValue(target) {
				return NewInvalidConfigError(fmt.Sprintf("VRF %s: invalid export route-target %s", vrf.Name, target))
			}
		}
		if vrf.ImportRouteMap != "" && importTargetCount == 0 {
			return NewInvalidConfigError(fmt.Sprintf("VRF %s: route-map import requires an import route-target", vrf.Name))
		}
		if vrf.ExportRouteMap != "" && exportTargetCount == 0 {
			return NewInvalidConfigError(fmt.Sprintf("VRF %s: route-map export requires an export route-target", vrf.Name))
		}
		if exportTargetCount > 0 {
			if vrf.RouteDistinguisher == "" {
				return NewInvalidConfigError(fmt.Sprintf("VRF %s: route-distinguisher is required for VPN export", vrf.Name))
			}
			if !validRouteDistinguisher(vrf.RouteDistinguisher) {
				return NewInvalidConfigError(fmt.Sprintf("VRF %s: invalid route-distinguisher %s", vrf.Name, vrf.RouteDistinguisher))
			}
		}
		if vrfHasVPNConfig(vrf) && vrf.ASN == 0 {
			return NewInvalidConfigError(fmt.Sprintf("VRF %s: BGP ASN is required for VPN import/export", vrf.Name))
		}
	}
	return nil
}

func validRouteTargetValue(target string) bool {
	return validColonUintPair(strings.TrimPrefix(target, "target:"))
}

func validRouteDistinguisher(rd string) bool {
	return validColonUintPair(rd)
}

func validColonUintPair(value string) bool {
	left, right, ok := strings.Cut(value, ":")
	if !ok || left == "" || right == "" {
		return false
	}
	if _, err := strconv.ParseUint(left, 10, 32); err != nil {
		return false
	}
	if _, err := strconv.ParseUint(right, 10, 32); err != nil {
		return false
	}
	return true
}

func validateTransactionalRouteMapSupport(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	for _, routeMap := range cfg.RouteMaps {
		for _, entry := range routeMap.Entries {
			if entry.MatchProtocol != "" {
				return NewInvalidConfigError(fmt.Sprintf("route-map %s entry %d match source-protocol is not supported by the transactional FRR backend until route-map source-protocol management operations are implemented", routeMap.Name, entry.Seq))
			}
			if entry.MatchNeighbor != "" {
				return NewInvalidConfigError(fmt.Sprintf("route-map %s entry %d match peer is not supported by the transactional FRR backend until route-map peer management operations are implemented", routeMap.Name, entry.Seq))
			}
			if entry.MatchASPath != "" {
				return NewInvalidConfigError(fmt.Sprintf("route-map %s entry %d match as-path is not supported by the transactional FRR backend until AS-path route-map management operations are implemented", routeMap.Name, entry.Seq))
			}
		}
	}
	if len(cfg.ASPathAccessLists) > 0 {
		return NewInvalidConfigError("AS-path access-lists are not supported by the transactional FRR backend until AS-path management operations are implemented")
	}
	return nil
}

func ospfHasBFDProtocolBindings(cfg *OSPFConfig) bool {
	if cfg == nil {
		return false
	}
	for _, iface := range cfg.Interfaces {
		if iface.BFD || iface.BFDProfile != "" {
			return true
		}
	}
	return false
}

func ospfHasBFDProfiles(cfg *OSPFConfig) bool {
	if cfg == nil {
		return false
	}
	for _, iface := range cfg.Interfaces {
		if iface.BFDProfile != "" {
			return true
		}
	}
	return false
}

func buildBFDConfigOps(cfg *BFDConfig) ([]MgmtOperation, error) {
	if cfg == nil || (len(cfg.Profiles) == 0 && len(cfg.Peers) == 0) {
		return nil, nil
	}
	if err := validateBFDConfig(cfg); err != nil {
		return nil, err
	}

	base := "/frr-bfdd:bfdd/bfd"
	var ops []MgmtOperation

	profiles := append([]BFDProfile(nil), cfg.Profiles...)
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	for _, profile := range profiles {
		profileBase := base + "/profile" + keyPred("name", profile.Name)
		ops = append(ops, setOp(profileBase+"/name", profile.Name))
		ops = appendBFDSessionCommonOps(ops, profileBase, profile.DetectMultiplier, profile.ReceiveInterval, profile.TransmitInterval, profile.PassiveMode, false)
		ops = appendBFDSessionEchoOps(ops, profileBase, profile.EchoMode)
	}

	peers := append([]BFDPeer(nil), cfg.Peers...)
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Address != peers[j].Address {
			return peers[i].Address < peers[j].Address
		}
		if peers[i].VRF != peers[j].VRF {
			return peers[i].VRF < peers[j].VRF
		}
		return peers[i].Interface < peers[j].Interface
	})
	for _, peer := range peers {
		peerOps, err := buildBFDPeerOps(base, peer)
		if err != nil {
			return nil, err
		}
		ops = append(ops, peerOps...)
	}

	return ops, nil
}

func buildBFDPeerOps(base string, peer BFDPeer) ([]MgmtOperation, error) {
	vrf := peer.VRF
	if vrf == "" {
		vrf = "default"
	}

	if peer.Multihop {
		if peer.LocalAddress == "" {
			return nil, NewInvalidConfigError(fmt.Sprintf("BFD multihop peer %s requires local-address for transactional apply", peer.Address))
		}
		peerBase := base + "/sessions/multi-hop" +
			keyPred("source-addr", peer.LocalAddress) +
			keyPred("dest-addr", peer.Address) +
			keyPred("vrf", vrf)
		ops := []MgmtOperation{
			setOp(peerBase+"/source-addr", peer.LocalAddress),
			setOp(peerBase+"/dest-addr", peer.Address),
			setOp(peerBase+"/vrf", vrf),
		}
		ops = appendBFDPeerCommonOps(ops, peerBase, peer)
		return ops, nil
	}

	if peer.Interface == "" {
		return nil, NewInvalidConfigError(fmt.Sprintf("BFD single-hop peer %s requires interface for transactional apply", peer.Address))
	}
	peerBase := base + "/sessions/single-hop" +
		keyPred("dest-addr", peer.Address) +
		keyPred("interface", peer.Interface) +
		keyPred("vrf", vrf)
	ops := []MgmtOperation{
		setOp(peerBase+"/dest-addr", peer.Address),
		setOp(peerBase+"/interface", peer.Interface),
		setOp(peerBase+"/vrf", vrf),
	}
	if peer.LocalAddress != "" {
		ops = append(ops, setOp(peerBase+"/source-addr", peer.LocalAddress))
	}
	ops = appendBFDPeerCommonOps(ops, peerBase, peer)
	ops = appendBFDSessionEchoOps(ops, peerBase, peer.EchoMode)
	return ops, nil
}

func appendBFDPeerCommonOps(ops []MgmtOperation, base string, peer BFDPeer) []MgmtOperation {
	if peer.Profile != "" {
		ops = append(ops, setOp(base+"/profile", peer.Profile))
	}
	return appendBFDSessionCommonOps(ops, base, peer.DetectMultiplier, peer.ReceiveInterval, peer.TransmitInterval, peer.PassiveMode, peer.Shutdown)
}

func appendBFDSessionCommonOps(ops []MgmtOperation, base string, detectMultiplier, receiveInterval, transmitInterval int, passiveMode, administrativeDown bool) []MgmtOperation {
	if detectMultiplier != 0 {
		ops = append(ops, setOp(base+"/detection-multiplier", strconv.Itoa(detectMultiplier)))
	}
	if transmitInterval != 0 {
		ops = append(ops, setOp(base+"/desired-transmission-interval", strconv.Itoa(millisecondsToMicroseconds(transmitInterval))))
	}
	if receiveInterval != 0 {
		ops = append(ops, setOp(base+"/required-receive-interval", strconv.Itoa(millisecondsToMicroseconds(receiveInterval))))
	}
	if passiveMode {
		ops = append(ops, setOp(base+"/passive-mode", "true"))
	}
	if administrativeDown {
		ops = append(ops, setOp(base+"/administrative-down", "true"))
	}
	return ops
}

func appendBFDSessionEchoOps(ops []MgmtOperation, base string, echoMode bool) []MgmtOperation {
	if echoMode {
		ops = append(ops, setOp(base+"/echo-mode", "true"))
	}
	return ops
}

func millisecondsToMicroseconds(value int) int {
	return value * 1000
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
		if staticRouteBFDConfigured(route) {
			ops = append(ops, buildStaticRouteBFDMonitoringOps(pathBase, route)...)
		}
	}
	return ops
}

func staticRouteBFDConfigured(route StaticRoute) bool {
	return route.BFD || route.BFDProfile != "" || route.BFDSource != "" || route.BFDMultihop
}

func buildStaticRouteBFDMonitoringOps(pathBase string, route StaticRoute) []MgmtOperation {
	base := pathBase + "/bfd-monitoring"
	ops := []MgmtOperation{setOp(base+"/multi-hop", strconv.FormatBool(route.BFDMultihop))}
	if route.BFDSource != "" {
		ops = append(ops, setOp(base+"/source", route.BFDSource))
	}
	if route.BFDProfile != "" {
		ops = append(ops, setOp(base+"/profile", route.BFDProfile))
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
		if neighbor.BFD {
			ops = append(ops, setOp(base+"/bfd-options/enable", "true"))
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
		if ospfInterfaceMgmtConfigured(iface) {
			ops = append(ops, buildOSPFInterfaceOps(iface)...)
		}
	}
	return ops
}

func ospfInterfaceMgmtConfigured(iface OSPFInterface) bool {
	return iface.Metric > 0 || iface.Priority != nil || iface.BFD
}

func buildOSPFInterfaceOps(iface OSPFInterface) []MgmtOperation {
	base := ospfInterfaceInstanceBase(iface.Name)
	ops := []MgmtOperation{
		setOp(interfaceBase(iface.Name)+"/name", iface.Name),
		setOp(base+"/id", defaultOSPFInterfaceInstanceID),
	}
	if iface.Metric > 0 {
		ops = append(ops, setOp(base+"/cost", strconv.Itoa(iface.Metric)))
	}
	if iface.Priority != nil {
		ops = append(ops, setOp(base+"/priority", strconv.Itoa(*iface.Priority)))
	}
	if iface.BFD {
		ops = append(ops, setOp(base+"/bfd", "true"))
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

func buildRouteMapOps(routeMaps []RouteMap, prefixLists []PrefixList) []MgmtOperation {
	if len(routeMaps) == 0 {
		return nil
	}
	ipv6PrefixLists := ipv6PrefixListSet(prefixLists)
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
				condition := "frr-route-map:ipv4-prefix-list"
				if ipv6PrefixLists[prefixList] {
					condition = "frr-route-map:ipv6-prefix-list"
				}
				matchBase := entryBase + "/match-condition" + keyPred("condition", condition)
				ops = append(ops,
					setOp(matchBase+"/condition", condition),
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

func ipv6PrefixListSet(prefixLists []PrefixList) map[string]bool {
	ipv6Lists := make(map[string]bool, len(prefixLists))
	for _, prefixList := range prefixLists {
		if prefixList.IsIPv6 {
			ipv6Lists[prefixList.Name] = true
		}
	}
	return ipv6Lists
}

func buildVRRPOps(cfg *VRRPConfig) ([]MgmtOperation, error) {
	if cfg == nil || len(cfg.Groups) == 0 {
		return nil, nil
	}
	if err := validateVRRPConfig(cfg); err != nil {
		return nil, err
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
		virtualAddress := net.ParseIP(group.VirtualAddress)
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
const defaultOSPFInterfaceInstanceID = "0"

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

func ospfInterfaceConfigBase() string {
	return "/frr-interface:lib/interface/frr-ospfd:ospf"
}

func ospfInterfaceInstanceBase(interfaceName string) string {
	return interfaceBase(interfaceName) + "/frr-ospfd:ospf/instance" + keyPred("id", defaultOSPFInterfaceInstanceID)
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
