package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/akam1o/arca-router/internal/compat"
	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
)

func (sh *interactiveShell) cmdEdit(args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'edit' command only available in configuration mode")
	}
	if len(args) == 0 {
		return fmt.Errorf("'edit' requires a configuration path")
	}
	sh.editPath = append(sh.editPath, args...)
	fmt.Printf("Entering edit mode at [edit %s]\n", strings.Join(sh.editPath, " "))
	return nil
}

func (sh *interactiveShell) cmdUp() error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'up' command only available in configuration mode")
	}
	if len(sh.editPath) > 0 {
		sh.editPath = sh.editPath[:len(sh.editPath)-1]
	}
	if len(sh.editPath) == 0 {
		fmt.Println("At top level")
	} else {
		fmt.Printf("Now at [edit %s]\n", strings.Join(sh.editPath, " "))
	}
	return nil
}

func (sh *interactiveShell) cmdTop() error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'top' command only available in configuration mode")
	}
	sh.editPath = nil
	fmt.Println("At top level")
	return nil
}

func (sh *interactiveShell) showHelp() {
	fmt.Println("Available commands:")
	fmt.Println()
	if sh.mode == modeOperational {
		fmt.Println("Operational mode commands:")
		fmt.Println("  help                          Show this help message")
		fmt.Println("  backup configuration <path>   Save running configuration to a file")
		fmt.Println("  backup configuration rollback <N> <path> Save archived config to a file")
		fmt.Println("  check upgrade [backup <path>] Run upgrade preflight checks")
		fmt.Println("  configure                     Enter configuration mode")
		fmt.Println("  show configuration            Show running configuration")
		fmt.Println("  show configuration rollback <N> Show archived config N commits back")
		fmt.Println("  show interfaces [<name>]      Show interface status")
		fmt.Println("  show routing-instances [name] Show routing-instance table mapping")
		fmt.Println("  show routes [prefix <cidr>] [protocol <proto>] Show route status")
		fmt.Println("  show bgp neighbors            Show BGP neighbor status")
		fmt.Println("  show bgp summary              Show raw BGP summary")
		fmt.Println("  show bgp neighbor <ip>        Show raw BGP neighbor details")
		fmt.Println("  show ospf neighbor            Show OSPFv2 neighbors")
		fmt.Println("  show ospf3 neighbor           Show OSPFv3 neighbors")
		fmt.Println("  show vrrp                     Show VRRP status")
		fmt.Println("  show bfd status               Show BFD operational state")
		fmt.Println("  show bfd [brief|counters]     Show raw BFD status")
		fmt.Println("  show bfd peer <ip> [counters] Show BFD peer details")
		fmt.Println("  show evpn                     Show EVPN/VXLAN overlay intent")
		fmt.Println("  show telemetry [path <path>]... [interval <duration>] [count <events>]")
		fmt.Println("                                Show telemetry events as JSON lines")
		fmt.Println("  show lcp                      Show VPP LCP reconciliation status")
		fmt.Println("  show ha                       Show HA convergence status")
		fmt.Println("  show class-of-service         Show class-of-service intent")
		fmt.Println("  show route [inet|inet6]                 Show routing table")
		fmt.Println("  show route [inet|inet6] protocol <proto> Show routes by protocol")
		fmt.Println("  exit, quit                    Exit interactive CLI")
	} else {
		fmt.Println("Configuration mode commands:")
		fmt.Println("  help                      Show this help message")
		fmt.Println("  backup configuration <path> Save candidate configuration to a file")
		fmt.Println("  backup configuration rollback <N> <path> Save archived config to a file")
		fmt.Println("  set <config>              Add or modify configuration")
		fmt.Println("  delete <config>           Delete configuration")
		fmt.Println("  restore configuration <path> Replace candidate from a backup file")
		fmt.Println("  restore configuration rollback <N> Replace candidate from archived config")
		fmt.Println("  show                      Show candidate configuration")
		fmt.Println("  show configuration rollback <N> Show archived config N commits back")
		fmt.Println("  show | compare            Show differences from running config")
		fmt.Println("  commit                    Commit candidate configuration")
		fmt.Println("  commit check              Validate and preview impact without committing")
		fmt.Println("  commit and-quit           Commit and exit configuration mode")
		fmt.Println("  commit comment <msg>      Commit with custom message")
		fmt.Println("  rollback <N>              Roll back N commits")
		fmt.Println("  discard-changes           Discard all candidate changes")
		fmt.Println("  show history [N]          Show last N commits")
		fmt.Println("  edit <path>               Navigate to configuration hierarchy")
		fmt.Println("  up                        Move up one level in hierarchy")
		fmt.Println("  top                       Move to top level of hierarchy")
		fmt.Println("  exit, quit                Exit configuration mode")
		if len(sh.editPath) > 0 {
			fmt.Printf("\nCurrent edit path: [edit %s]\n", strings.Join(sh.editPath, " "))
		}
	}
	fmt.Println()
}

// --- Output formatters ---

func printInterfaces(ifaces []grpcclient.InterfaceInfo) {
	if len(ifaces) == 0 {
		fmt.Println("No interfaces found")
		return
	}
	fmt.Printf("%-20s %-8s %-8s %-6s %-18s %-10s %-12s %-12s %-16s %-15s %s\n",
		"Interface", "Admin", "Oper", "MTU", "MAC", "Speed", "RX-Packets", "TX-Packets", "QoS", "Tables", "Queues")
	fmt.Println(strings.Repeat("-", 159))
	for _, iface := range ifaces {
		fmt.Printf("%-20s %-8s %-8s %-6d %-18s %-10d %-12d %-12d %-16s %-15s %s\n",
			iface.Name, iface.AdminStatus, iface.OperStatus,
			iface.MTU, iface.MAC, iface.Speed, iface.RxPackets, iface.TxPackets, interfaceQoSProfile(iface), interfaceTableSummary(iface), interfaceQueueSummary(iface))
	}
}

func interfaceQoSProfile(iface grpcclient.InterfaceInfo) string {
	if iface.QoSProfile == "" {
		return "-"
	}
	return iface.QoSProfile
}

func interfaceTableSummary(iface grpcclient.InterfaceInfo) string {
	if iface.IPv4TableID == 0 && iface.IPv6TableID == 0 {
		return "-"
	}
	if iface.IPv4TableID == iface.IPv6TableID {
		return fmt.Sprintf("v4/v6:%d", iface.IPv4TableID)
	}
	return fmt.Sprintf("v4:%d v6:%d", iface.IPv4TableID, iface.IPv6TableID)
}

func interfaceQueueSummary(iface grpcclient.InterfaceInfo) string {
	var parts []string
	for _, queue := range iface.RxQueues {
		mode := queue.Mode
		if mode == "" {
			mode = "unknown"
		}
		parts = append(parts, fmt.Sprintf("rx%d:w%d/%s", queue.QueueID, queue.WorkerID, mode))
	}
	for _, queue := range iface.TxQueues {
		suffix := ""
		if queue.Shared {
			suffix = "*"
		}
		parts = append(parts, fmt.Sprintf("tx%d:%s%s", queue.QueueID, formatQueueThreads(queue.Threads), suffix))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func formatQueueThreads(threads []uint32) string {
	if len(threads) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(threads))
	for _, thread := range threads {
		parts = append(parts, strconv.FormatUint(uint64(thread), 10))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func printRoutingInstances(instances []grpcclient.RoutingInstanceInfo) {
	if len(instances) == 0 {
		fmt.Println("No routing instances found")
		return
	}
	fmt.Printf("%-24s %-8s %-18s %-15s %s\n", "Instance", "Type", "RD", "VPP tables", "Interfaces")
	fmt.Println(strings.Repeat("-", 98))
	for _, instance := range instances {
		fmt.Printf("%-24s %-8s %-18s %-15s %s\n",
			instance.Name,
			formatRoutingInstanceValue(instance.InstanceType),
			formatRoutingInstanceValue(instance.RouteDistinguisher),
			routingInstanceTableSummary(instance),
			formatRoutingInstanceList(instance.Interfaces),
		)
	}

	if !routingInstancesHavePolicy(instances) {
		return
	}
	fmt.Println()
	fmt.Println("Import/export")
	fmt.Printf("%-24s %-32s %-32s %-24s %-24s\n", "Instance", "Import RT", "Export RT", "Import policy", "Export policy")
	fmt.Println(strings.Repeat("-", 140))
	for _, instance := range instances {
		fmt.Printf("%-24s %-32s %-32s %-24s %-24s\n",
			instance.Name,
			formatRoutingInstanceList(instance.ImportTargets),
			formatRoutingInstanceList(instance.ExportTargets),
			formatRoutingInstanceList(instance.ImportPolicies),
			formatRoutingInstanceList(instance.ExportPolicies),
		)
	}
}

func routingInstanceTableSummary(instance grpcclient.RoutingInstanceInfo) string {
	if instance.IPv4TableID == 0 && instance.IPv6TableID == 0 {
		return "-"
	}
	if instance.IPv4TableID == instance.IPv6TableID {
		return fmt.Sprintf("v4/v6:%d", instance.IPv4TableID)
	}
	return fmt.Sprintf("v4:%d v6:%d", instance.IPv4TableID, instance.IPv6TableID)
}

func routingInstancesHavePolicy(instances []grpcclient.RoutingInstanceInfo) bool {
	for _, instance := range instances {
		if len(instance.ImportTargets) > 0 || len(instance.ExportTargets) > 0 ||
			len(instance.ImportPolicies) > 0 || len(instance.ExportPolicies) > 0 {
			return true
		}
	}
	return false
}

func formatRoutingInstanceValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatRoutingInstanceList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

func printRoutes(routes []grpcclient.RouteInfo) {
	if len(routes) == 0 {
		fmt.Println("No routes found")
		return
	}
	fmt.Printf("%-43s %-39s %-12s %-8s %-16s %-8s\n",
		"Prefix", "Next hop", "Protocol", "Metric", "Interface", "Active")
	fmt.Println(strings.Repeat("-", 131))
	for _, route := range routes {
		fmt.Printf("%-43s %-39s %-12s %-8d %-16s %-8s\n",
			formatRouteValue(route.Prefix),
			formatRouteValue(route.NextHop),
			formatRouteValue(route.Protocol),
			route.Metric,
			formatRouteValue(route.Interface),
			yesNo(route.Active),
		)
	}
}

func formatRouteValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func printBGPNeighbors(neighbors []grpcclient.BGPNeighborInfo) {
	if len(neighbors) == 0 {
		fmt.Println("No BGP neighbors found")
		return
	}
	fmt.Printf("%-39s %-10s %-16s %-14s %-12s %-12s\n",
		"Peer", "AS", "State", "Uptime", "Prefixes in", "Prefixes out")
	fmt.Println(strings.Repeat("-", 109))
	for _, neighbor := range neighbors {
		fmt.Printf("%-39s %-10d %-16s %-14s %-12d %-12d\n",
			formatBGPValue(neighbor.PeerAddress),
			neighbor.PeerAS,
			formatBGPValue(neighbor.State),
			formatBGPUptime(neighbor.UptimeSecs),
			neighbor.PrefixReceived,
			neighbor.PrefixSent,
		)
	}
}

func printOSPFNeighbors(neighbors []grpcclient.OSPFNeighborInfo) {
	if len(neighbors) == 0 {
		fmt.Println("No OSPF neighbors found")
		return
	}
	fmt.Printf("%-15s %-39s %-16s %-14s %-10s %-10s %-10s\n",
		"Router ID", "Address", "Interface", "State", "Role", "Dead", "Uptime")
	fmt.Println(strings.Repeat("-", 122))
	for _, neighbor := range neighbors {
		fmt.Printf("%-15s %-39s %-16s %-14s %-10s %-10s %-10s\n",
			formatBGPValue(neighbor.RouterID),
			formatBGPValue(neighbor.Address),
			formatBGPValue(neighbor.Interface),
			formatBGPValue(neighbor.State),
			formatBGPValue(neighbor.Role),
			formatBGPUptime(neighbor.DeadTimeSecs),
			formatBGPUptime(neighbor.UptimeSecs),
		)
	}
}

func formatBGPValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatBGPUptime(seconds uint64) string {
	if seconds == 0 {
		return "-"
	}
	days := seconds / 86400
	seconds %= 86400
	hours := seconds / 3600
	seconds %= 3600
	minutes := seconds / 60
	seconds %= 60
	if days > 0 {
		return fmt.Sprintf("%dd%02dh%02dm%02ds", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func printLCPReconciliation(info *grpcclient.LCPReconciliationInfo) {
	if info == nil {
		fmt.Println("No LCP reconciliation status found")
		return
	}
	fmt.Printf("%-18s %s\n", "State", lcpReconciliationState(info))
	fmt.Printf("%-18s %s\n", "Last check", formatOptionalTime(info.LastRun))
	fmt.Printf("%-18s %d\n", "Pairs", info.PairCount)
	if info.LastError != "" {
		fmt.Printf("%-18s %s\n", "Last error", info.LastError)
	}
	if len(info.Inconsistencies) == 0 {
		return
	}
	fmt.Println("Inconsistencies")
	for _, issue := range info.Inconsistencies {
		fmt.Printf("  - %s\n", issue)
	}
}

func printHAStatus(info *grpcclient.HAStatusInfo) {
	if info == nil {
		fmt.Println("No HA status found")
		return
	}
	fmt.Printf("%-18s %s\n", "State", haState(info))
	fmt.Printf("%-18s %s\n", "Configured", yesNo(info.Configured))
	fmt.Printf("%-18s %s\n", "Converged", yesNo(info.Converged))
	fmt.Printf("%-18s %d\n", "VRRP groups", info.VRRPGroups)
	fmt.Printf("%-18s %d\n", "Cluster nodes", info.ClusterNodes)
	fmt.Printf("%-18s %s\n", "Cluster sync", clusterSyncState(info))
	fmt.Printf("%-18s %d/%d\n", "FRR VRRP", info.FRRVRRPActiveGroups, info.FRRVRRPConfiguredGroups)
	fmt.Printf("%-18s %s\n", "FRR last check", formatOptionalTime(info.FRRVRRPLastCheck))
	fmt.Printf("%-18s %s\n", "FRR BFD", haBFDState(info))
	fmt.Printf("%-18s %s\n", "BFD last check", formatOptionalTime(info.FRRBFDLastCheck))
	fmt.Printf("%-18s %s\n", "VPP LCP", lcpReconciliationState(&grpcclient.LCPReconciliationInfo{
		LastRun:         info.VPPLCPLastCheck,
		PairCount:       info.VPPLCPPairs,
		Inconsistencies: info.VPPLCPInconsistencies,
		LastError:       info.VPPLCPLastError,
	}))
	fmt.Printf("%-18s %s\n", "LCP last check", formatOptionalTime(info.VPPLCPLastCheck))
	if len(info.Issues) == 0 {
		return
	}
	fmt.Println("Issues")
	for _, issue := range info.Issues {
		fmt.Printf("  - %s\n", issue)
	}
}

func printBFDStatus(info *grpcclient.BFDStatusInfo) {
	if !hasBFDStatus(info) {
		fmt.Println("No BFD operational status found")
		return
	}
	fmt.Printf("%-18s %s\n", "State", bfdOperationalState(info))
	fmt.Printf("%-18s %s\n", "Last check", formatOptionalTime(info.LastRun))
	fmt.Printf("%-18s %d\n", "Configured peers", info.ConfiguredPeers)
	fmt.Printf("%-18s %d\n", "Observed peers", info.ObservedPeers)
	fmt.Printf("%-18s %d\n", "Up peers", info.UpPeers)
	fmt.Printf("%-18s %d\n", "Down peers", info.DownPeers)
	fmt.Printf("%-18s %d\n", "Session down", info.SessionDownEvents)
	fmt.Printf("%-18s %d\n", "RX fail packets", info.RxFailPackets)
	if info.LastError != "" {
		fmt.Printf("%-18s %s\n", "Last error", info.LastError)
	}
	if len(info.Peers) > 0 {
		fmt.Println()
		fmt.Println("Peers")
		fmt.Printf("%-39s %-39s %-16s %-12s %-10s %-8s %-12s %-12s\n",
			"Peer", "Local", "Interface", "VRF", "Status", "Up", "Down events", "RX fails")
		fmt.Println(strings.Repeat("-", 158))
		for _, peer := range info.Peers {
			fmt.Printf("%-39s %-39s %-16s %-12s %-10s %-8s %-12d %-12d\n",
				formatBFDValue(peer.Peer),
				formatBFDValue(peer.LocalAddress),
				formatBFDValue(peer.Interface),
				formatBFDValue(peer.VRF),
				formatBFDValue(peer.Status),
				yesNo(peer.Up),
				peer.SessionDownEvents,
				peer.RxFailPackets,
			)
			if peer.Diagnostic != "" || peer.RemoteDiagnostic != "" || !peer.Observed {
				fmt.Printf("  diagnostic: %s remote: %s observed: %s\n",
					formatBFDValue(peer.Diagnostic),
					formatBFDValue(peer.RemoteDiagnostic),
					yesNo(peer.Observed),
				)
			}
		}
	}
	if len(info.Issues) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("Issues")
	for _, issue := range info.Issues {
		fmt.Printf("  - %s\n", issue)
	}
}

func printClassOfService(info *grpcclient.ClassOfServiceInfo) {
	if info == nil || (len(info.ForwardingClasses) == 0 && len(info.TrafficControlProfiles) == 0 && len(info.Interfaces) == 0 && info.Capabilities == nil) {
		fmt.Println("No class-of-service configuration found")
		return
	}
	if len(info.ForwardingClasses) == 0 && len(info.TrafficControlProfiles) == 0 && len(info.Interfaces) == 0 {
		fmt.Println("No class-of-service configuration found")
	} else {
		fmt.Printf("%-18s %s\n", "Enforcement", formatCoSValue(info.EnforcementStatus))
	}

	if len(info.ForwardingClasses) > 0 {
		fmt.Println()
		fmt.Println("Forwarding classes")
		fmt.Printf("%-32s %-8s\n", "Name", "Queue")
		fmt.Println(strings.Repeat("-", 41))
		for _, fc := range info.ForwardingClasses {
			fmt.Printf("%-32s %-8d\n", fc.Name, fc.Queue)
		}
	}

	if len(info.TrafficControlProfiles) > 0 {
		fmt.Println()
		fmt.Println("Traffic-control profiles")
		fmt.Printf("%-32s %-16s %-24s %-14s\n", "Name", "Shaping rate", "Scheduler map", "Enforcement")
		fmt.Println(strings.Repeat("-", 88))
		for _, profile := range info.TrafficControlProfiles {
			fmt.Printf("%-32s %-16s %-24s %-14s\n",
				profile.Name,
				formatCoSRate(profile.ShapingRate),
				formatCoSValue(profile.SchedulerMap),
				formatCoSValue(profile.EnforcementStatus),
			)
		}
	}

	if len(info.Interfaces) > 0 {
		fmt.Println()
		fmt.Println("Interfaces")
		fmt.Printf("%-24s %-32s %-14s\n", "Interface", "Output profile", "Enforcement")
		fmt.Println(strings.Repeat("-", 72))
		for _, iface := range info.Interfaces {
			fmt.Printf("%-24s %-32s %-14s\n",
				iface.Name,
				formatCoSValue(iface.OutputTrafficControlProfile),
				formatCoSValue(iface.EnforcementStatus),
			)
		}
	}

	if info.Capabilities != nil {
		fmt.Println()
		fmt.Println("VPP QoS capabilities")
		fmt.Printf("%-24s %s\n", "Metadata binding", yesNo(info.Capabilities.MetadataBindingSupported))
		fmt.Printf("%-24s %s\n", "Queue scheduler", yesNo(info.Capabilities.QueueSchedulerSupported))
		fmt.Printf("%-24s %s\n", "Policer", yesNo(info.Capabilities.PolicerSupported))
		fmt.Printf("%-24s %s\n", "Counters", yesNo(info.Capabilities.CountersSupported))
		fmt.Printf("%-24s %s\n", "Last check", formatOptionalTime(info.Capabilities.LastCheck))
		fmt.Printf("%-24s %s\n", "Last error", formatCoSValue(info.Capabilities.LastError))
		if len(info.Capabilities.Diagnostics) > 0 {
			fmt.Println()
			fmt.Println("VPP QoS diagnostics")
			for _, diagnostic := range info.Capabilities.Diagnostics {
				fmt.Printf("  - %s\n", diagnostic)
			}
		}
	}
}

type evpnTelemetrySnapshot struct {
	VNIs []evpnTelemetryVNI `json:"vnis"`
}

type evpnTelemetryVNI struct {
	VNI                int      `json:"vni"`
	Type               string   `json:"type,omitempty"`
	BridgeDomain       string   `json:"bridge_domain,omitempty"`
	VLANID             int      `json:"vlan_id,omitempty"`
	RoutingInstance    string   `json:"routing_instance,omitempty"`
	RouteDistinguisher string   `json:"route_distinguisher,omitempty"`
	VRFTarget          string   `json:"vrf_target,omitempty"`
	VRFTargetImport    []string `json:"vrf_target_import,omitempty"`
	VRFTargetExport    []string `json:"vrf_target_export,omitempty"`
	SourceInterface    string   `json:"source_interface,omitempty"`
	SourceAddress      string   `json:"source_address,omitempty"`
	MulticastGroup     string   `json:"multicast_group,omitempty"`
	RemoteVTEP         string   `json:"remote_vtep,omitempty"`
}

type evpnTelemetryCounts struct {
	total     int
	l2        int
	l3        int
	multicast int
}

func showEVPN(ctx context.Context, client showClient) error {
	snapshot, err := fetchEVPNTelemetrySnapshot(ctx, client)
	if err != nil {
		return err
	}
	printEVPN(snapshot)
	return nil
}

func fetchEVPNTelemetrySnapshot(ctx context.Context, client showClient) (*evpnTelemetrySnapshot, error) {
	stream, err := client.SubscribeTelemetry(ctx, []string{"/overlays/evpn"}, 0, true)
	if err != nil {
		return nil, err
	}
	event, err := stream.Recv()
	if err == io.EOF {
		return nil, fmt.Errorf("EVPN telemetry snapshot was empty")
	}
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, fmt.Errorf("EVPN telemetry snapshot was nil")
	}
	var snapshot evpnTelemetrySnapshot
	if err := json.Unmarshal([]byte(event.JSONPayload), &snapshot); err != nil {
		return nil, fmt.Errorf("decode EVPN telemetry snapshot: %w", err)
	}
	return &snapshot, nil
}

func printEVPN(snapshot *evpnTelemetrySnapshot) {
	if snapshot == nil || len(snapshot.VNIs) == 0 {
		fmt.Println("No EVPN/VXLAN VNI configuration found")
		return
	}
	counts := countEVPNVNIs(snapshot.VNIs)
	fmt.Printf("%-18s %s\n", "Configured", yesNo(counts.total > 0))
	fmt.Printf("%-18s %d\n", "VNIs", counts.total)
	fmt.Printf("%-18s %d\n", "L2 VNIs", counts.l2)
	fmt.Printf("%-18s %d\n", "L3 VNIs", counts.l3)
	fmt.Printf("%-18s %d\n", "Multicast VNIs", counts.multicast)

	fmt.Println()
	fmt.Println("VNIs")
	fmt.Printf("%-8s %-6s %-20s %-20s %-8s %-18s %-28s %-24s %s\n",
		"VNI", "Type", "Bridge domain", "Routing instance", "VLAN", "RD", "Route targets", "Source", "Endpoint")
	fmt.Println(strings.Repeat("-", 169))
	for _, vni := range snapshot.VNIs {
		fmt.Printf("%-8d %-6s %-20s %-20s %-8s %-18s %-28s %-24s %s\n",
			vni.VNI,
			formatEVPNValue(vni.Type),
			formatEVPNValue(vni.BridgeDomain),
			formatEVPNValue(vni.RoutingInstance),
			formatEVPNVLAN(vni.VLANID),
			formatEVPNValue(vni.RouteDistinguisher),
			formatEVPNRouteTargets(vni),
			formatEVPNSource(vni),
			formatEVPNEndpoint(vni),
		)
	}
}

func countEVPNVNIs(vnis []evpnTelemetryVNI) evpnTelemetryCounts {
	var counts evpnTelemetryCounts
	for _, vni := range vnis {
		counts.total++
		switch strings.ToLower(vni.Type) {
		case "l2":
			counts.l2++
		case "l3":
			counts.l3++
		}
		if vni.MulticastGroup != "" {
			counts.multicast++
		}
	}
	return counts
}

func formatEVPNValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatEVPNVLAN(vlanID int) string {
	if vlanID == 0 {
		return "-"
	}
	return strconv.Itoa(vlanID)
}

func formatEVPNRouteTargets(vni evpnTelemetryVNI) string {
	var parts []string
	if vni.VRFTarget != "" {
		parts = append(parts, vni.VRFTarget)
	}
	if len(vni.VRFTargetImport) > 0 {
		parts = append(parts, "import:"+strings.Join(vni.VRFTargetImport, ","))
	}
	if len(vni.VRFTargetExport) > 0 {
		parts = append(parts, "export:"+strings.Join(vni.VRFTargetExport, ","))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func formatEVPNSource(vni evpnTelemetryVNI) string {
	switch {
	case vni.SourceInterface != "" && vni.SourceAddress != "":
		return vni.SourceInterface + "@" + vni.SourceAddress
	case vni.SourceInterface != "":
		return vni.SourceInterface
	case vni.SourceAddress != "":
		return vni.SourceAddress
	default:
		return "-"
	}
}

func formatEVPNEndpoint(vni evpnTelemetryVNI) string {
	switch {
	case vni.MulticastGroup != "":
		return "multicast:" + vni.MulticastGroup
	case vni.RemoteVTEP != "":
		return "remote:" + vni.RemoteVTEP
	default:
		return "-"
	}
}

func formatCoSValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatCoSRate(value uint64) string {
	if value == 0 {
		return "-"
	}
	return strconv.FormatUint(value, 10)
}

func haState(info *grpcclient.HAStatusInfo) string {
	if info == nil || !info.Configured {
		return "not configured"
	}
	if info.Converged {
		return "converged"
	}
	return "issues"
}

func clusterSyncState(info *grpcclient.HAStatusInfo) string {
	if info == nil || !info.ClusterEtcdSync {
		return "not configured"
	}
	if info.ClusterSyncAligned {
		return "aligned"
	}
	return "mismatch"
}

func haBFDState(info *grpcclient.HAStatusInfo) string {
	if info == nil || (info.FRRBFDConfiguredPeers == 0 && info.FRRBFDObservedPeers == 0) {
		return "not configured"
	}
	totalPeers := info.FRRBFDConfiguredPeers
	if totalPeers == 0 {
		totalPeers = info.FRRBFDObservedPeers
	}
	state := fmt.Sprintf("%d/%d up", info.FRRBFDUpPeers, totalPeers)
	if info.FRRBFDLastError != "" || len(info.FRRBFDIssues) > 0 ||
		info.FRRBFDDownPeers > 0 || info.FRRBFDUpPeers < info.FRRBFDConfiguredPeers {
		return state + " (issues)"
	}
	if info.FRRBFDLastCheck.IsZero() {
		return state + " (unknown)"
	}
	return state
}

func hasBFDStatus(info *grpcclient.BFDStatusInfo) bool {
	if info == nil {
		return false
	}
	return !info.LastRun.IsZero() ||
		info.ConfiguredPeers != 0 ||
		info.ObservedPeers != 0 ||
		info.UpPeers != 0 ||
		info.DownPeers != 0 ||
		info.SessionDownEvents != 0 ||
		info.RxFailPackets != 0 ||
		len(info.Peers) != 0 ||
		len(info.Issues) != 0 ||
		info.LastError != ""
}

func bfdOperationalState(info *grpcclient.BFDStatusInfo) string {
	if !hasBFDStatus(info) {
		return "unknown"
	}
	if info.LastError != "" {
		return "check failed"
	}
	if len(info.Issues) > 0 || info.DownPeers > 0 {
		return "issues"
	}
	return "converged"
}

func formatBFDValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func lcpReconciliationState(info *grpcclient.LCPReconciliationInfo) string {
	if info == nil || info.LastRun.IsZero() {
		return "unknown"
	}
	if info.LastError != "" {
		return "check failed"
	}
	if len(info.Inconsistencies) > 0 {
		return "mismatch"
	}
	return "consistent"
}

func formatOptionalTime(ts time.Time) string {
	if ts.IsZero() {
		return "never"
	}
	return ts.Local().Format(time.RFC3339)
}

func printCommandOutput(output string) {
	fmt.Print(output)
	if output != "" && !strings.HasSuffix(output, "\n") {
		fmt.Println()
	}
}

func printCompatibilityPolicy() {
	policy := compat.CurrentPolicy()
	fmt.Println("compatibility policy:")
	fmt.Printf("  phase: %s\n", policy.Phase)
	fmt.Printf("  direct upgrade sources: %s\n", strings.Join(policy.SupportedDirectUpgradeSources, ", "))
	fmt.Printf("  unsupported direct upgrades: %s\n", policy.UnsupportedDirectUpgradeNote)
	fmt.Printf("  configuration: %s\n", policy.ConfigCompatibility)
	fmt.Printf("  CLI: %s\n", policy.CLICompatibility)
	fmt.Printf("  API: %s\n", policy.APIVersioning)
	fmt.Printf("  deprecation: %s\n", policy.DeprecationPolicy)
	fmt.Println("schema IDs:")
	fmt.Printf("  gRPC: %s\n", compat.GRPCAPIPackage)
	fmt.Printf("  telemetry events: %s\n", compat.TelemetryEventSchema)
	fmt.Printf("  NMS status: %s\n", compat.NMSOperationalSchema)
	fmt.Printf("  NMS telemetry catalog: %s\n", compat.NMSTelemetryCatalogSchema)
	fmt.Printf("  NMS telemetry schemas: %s\n", compat.NMSTelemetrySchemaCatalog)
	fmt.Printf("  NMS telemetry snapshot: %s\n", compat.NMSTelemetrySnapshot)
	fmt.Printf("  audit export: %s\n", compat.AuditSchema)
	fmt.Println("support matrix:")
	for _, item := range compat.ComponentMatrix() {
		fmt.Printf("  %s: %s\n", item.Component, item.Supported)
		fmt.Printf("    required: %s\n", item.Required)
		fmt.Printf("    notes: %s\n", item.Notes)
	}
	fmt.Printf("deferred gates (%s):\n", compat.DeferredGateDocument)
	for _, gate := range compat.DeferredCompatibilityGates() {
		fmt.Printf("  - %s\n", gate)
	}
}
