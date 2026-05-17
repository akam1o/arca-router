package netconf

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/datastore"
)

// GetRequest represents <get> RPC for operational data
type GetRequest struct {
	XMLName xml.Name `xml:"get"`
	Filter  *Filter  `xml:"filter"`
}

func (r *GetRequest) SetInheritedNamespaceAttrs(attrs []xml.Attr) {
	if r == nil {
		return
	}
	if r.Filter != nil {
		r.Filter.InheritedAttrs = cloneXMLAttrs(attrs)
	}
}

// handleGet handles <get> RPC - retrieves operational data
func (s *Server) handleGet(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req GetRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Validate filter
	if err := req.Filter.Validate("get"); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Validate filter depth and size limits
	if err := ValidateFilterDepthAndSize("get", req.Filter); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	operationalData, err := s.getOperationalData(ctx, req.Filter)
	if err != nil {
		log.Printf("[NETCONF] Failed to get operational data: %v", err)
		if rpcErr, ok := err.(*RPCError); ok {
			return NewErrorReply(rpc.MessageID, rpcErr)
		}
		return NewErrorReply(rpc.MessageID, ErrOperationFailed(fmt.Sprintf("failed to retrieve operational data: %v", err)))
	}

	return NewDataReply(rpc.MessageID, operationalData)
}

func (s *Server) getOperationalData(ctx context.Context, filter *Filter) ([]byte, error) {
	cfg := config.NewConfig()
	if s != nil && s.datastore != nil {
		running, err := s.datastore.GetRunning(ctx)
		if err != nil {
			var dsErr *datastore.Error
			if !errors.As(err, &dsErr) || dsErr.Code != datastore.ErrCodeNotFound {
				return nil, err
			}
		} else if running != nil {
			cfg, err = TextToConfig(running.ConfigText)
			if err != nil {
				return nil, err
			}
		}
	}

	collectionFilter := filter
	if usesExperimentalXPathEngine(filter) {
		collectionFilter = nil
	}
	interfaceStates := s.collectInterfaceOperationalState(ctx, collectionFilter)
	routes := s.collectRouteOperationalState(ctx, collectionFilter)
	bgpNeighbors := s.collectBGPOperationalState(ctx, collectionFilter)
	ospfNeighbors := s.collectOSPFOperationalState(ctx, collectionFilter, false)
	ospf3Neighbors := s.collectOSPFOperationalState(ctx, collectionFilter, true)
	bfdStatus := s.collectBFDOperationalState(ctx, collectionFilter)
	data, err := buildOperationalData(cfg, collectionFilter, time.Now().UTC(), interfaceStates, routes, bgpNeighbors, ospfNeighbors, ospf3Neighbors, bfdStatus)
	if err != nil {
		return nil, err
	}
	if usesExperimentalXPathEngine(filter) {
		return applyExperimentalXPathFilter("get", data, filter)
	}
	return data, nil
}

// GetOperationalData builds operational state without a datastore-backed
// server. It is kept for tests and callers that only need local system state.
func GetOperationalData(ctx context.Context, filter *Filter) ([]byte, error) {
	_ = ctx
	outputFilter := filter
	if usesExperimentalXPathEngine(filter) {
		outputFilter = nil
	}
	data, err := buildOperationalData(config.NewConfig(), outputFilter, time.Now().UTC(), nil, nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if usesExperimentalXPathEngine(filter) {
		return applyExperimentalXPathFilter("get", data, filter)
	}
	return data, nil
}

// buildAllOperationalData builds operational data XML for the inside of <data>.
func buildAllOperationalData() string {
	data, err := buildOperationalData(config.NewConfig(), nil, time.Now().UTC(), nil, nil, nil, nil, nil, nil)
	if err != nil {
		return ""
	}
	return string(data)
}

func (s *Server) collectInterfaceOperationalState(ctx context.Context, filter *Filter) map[string]*InterfaceOperationalState {
	if s == nil || s.operationalProvider == nil || !includeOperationalSection(filter, "interfaces") {
		return nil
	}
	states, err := s.operationalProvider.InterfaceStates(ctx)
	if err != nil {
		log.Printf("[NETCONF] Failed to collect interface operational state: %v", err)
		return nil
	}
	return states
}

func (s *Server) collectRouteOperationalState(ctx context.Context, filter *Filter) []RouteOperationalState {
	if s == nil || s.operationalProvider == nil || !includeOperationalSection(filter, "state", "routes") {
		return nil
	}
	routes, err := s.operationalProvider.Routes(ctx)
	if err != nil {
		log.Printf("[NETCONF] Failed to collect route operational state: %v", err)
		return nil
	}
	return routes
}

func (s *Server) collectBGPOperationalState(ctx context.Context, filter *Filter) []BGPNeighborOperationalState {
	if s == nil || s.operationalProvider == nil || !includeOperationalSection(filter, "state", "protocols", "bgp") {
		return nil
	}
	neighbors, err := s.operationalProvider.BGPNeighbors(ctx)
	if err != nil {
		log.Printf("[NETCONF] Failed to collect BGP operational state: %v", err)
		return nil
	}
	return neighbors
}

func (s *Server) collectOSPFOperationalState(ctx context.Context, filter *Filter, ipv6 bool) []OSPFNeighborOperationalState {
	section := "ospf"
	logName := "OSPFv2"
	if ipv6 {
		section = "ospf3"
		logName = "OSPFv3"
	}
	if s == nil || s.operationalProvider == nil || !includeOperationalSection(filter, "state", "protocols", section) {
		return nil
	}
	neighbors, err := s.operationalProvider.OSPFNeighbors(ctx, ipv6)
	if err != nil {
		log.Printf("[NETCONF] Failed to collect %s operational state: %v", logName, err)
		return nil
	}
	return neighbors
}

func (s *Server) collectBFDOperationalState(ctx context.Context, filter *Filter) *BFDOperationalState {
	if s == nil || s.operationalProvider == nil || !includeOperationalSection(filter, "state", "protocols", "bfd") {
		return nil
	}
	status, err := s.operationalProvider.BFDStatus(ctx)
	if err != nil {
		log.Printf("[NETCONF] Failed to collect BFD operational state: %v", err)
		return nil
	}
	if !hasBFDOperationalState(status) {
		return nil
	}
	return status
}

func buildOperationalData(cfg *config.Config, filter *Filter, now time.Time, interfaceStates map[string]*InterfaceOperationalState, routes []RouteOperationalState, bgpNeighbors []BGPNeighborOperationalState, ospfNeighbors []OSPFNeighborOperationalState, ospf3Neighbors []OSPFNeighborOperationalState, bfdStatus *BFDOperationalState) ([]byte, error) {
	if cfg == nil {
		cfg = config.NewConfig()
	}
	xpathFilter := outputXPathFilter(filter)

	var buf bytes.Buffer
	if includeOperationalSection(filter, "system") {
		if err := writeSystemStateXML(&buf, cfg, now); err != nil {
			return nil, err
		}
	}
	if includeOperationalSection(filter, "interfaces") && (len(cfg.Interfaces) > 0 || len(interfaceStates) > 0) {
		if err := writeInterfaceStateXML(&buf, cfg.Interfaces, interfaceStates, xpathFilter); err != nil {
			return nil, err
		}
	}
	if includeOperationalSectionPaths(filter,
		[]string{"routing"},
		[]string{"routing", "routing-state"},
		[]string{"routing", "routing-state", "routes"},
		[]string{"routing", "routing-state", "routing-protocols"},
	) && hasRoutingState(cfg) {
		if err := writeRoutingStateXML(&buf, cfg, xpathFilter); err != nil {
			return nil, err
		}
	}
	if !includeOperationalSection(filter, "state", "routes") {
		routes = nil
	}
	if !includeOperationalSection(filter, "state", "protocols", "bgp") {
		bgpNeighbors = nil
	}
	if !includeOperationalSection(filter, "state", "protocols", "ospf") {
		ospfNeighbors = nil
	}
	if !includeOperationalSection(filter, "state", "protocols", "ospf3") {
		ospf3Neighbors = nil
	}
	if !includeOperationalSection(filter, "state", "protocols", "bfd") {
		bfdStatus = nil
	}
	routes = filterRouteOperationalStates(routes, xpathFilter)
	bgpNeighbors = filterBGPOperationalNeighbors(bgpNeighbors, xpathFilter)
	ospfNeighbors = filterOSPFOperationalNeighbors(ospfNeighbors, xpathFilter, "ospf")
	ospf3Neighbors = filterOSPFOperationalNeighbors(ospf3Neighbors, xpathFilter, "ospf3")
	bfdStatus = filterBFDOperationalState(bfdStatus, xpathFilter)
	routingInstances, err := collectRoutingInstanceOperationalState(cfg, filter)
	if err != nil {
		return nil, err
	}
	routingInstances = filterRoutingInstanceOperationalStates(routingInstances, xpathFilter)
	if hasArcaOperationalState(routes, routingInstances, bgpNeighbors, ospfNeighbors, ospf3Neighbors, bfdStatus) {
		if err := writeArcaStateXML(&buf, routes, routingInstances, bgpNeighbors, ospfNeighbors, ospf3Neighbors, bfdStatus); err != nil {
			return nil, err
		}
	}

	if buf.Len() > MaxXMLSize {
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("generated operational XML exceeds size limit (%d bytes)", MaxXMLSize)).
			WithPath("/rpc/get").
			WithAppTag("size-limit")
	}

	return buf.Bytes(), nil
}

func collectRoutingInstanceOperationalState(cfg *config.Config, filter *Filter) ([]RoutingInstanceOperationalState, error) {
	if cfg == nil || len(cfg.RoutingInstances) == 0 || !includeOperationalSection(filter, "state", "routing-instances") {
		return nil, nil
	}
	return routingInstanceOperationalStates(cfg)
}

func routingInstanceOperationalStates(cfg *config.Config) ([]RoutingInstanceOperationalState, error) {
	modelConfig := model.FromLegacyConfig(cfg)
	plans, err := model.RoutingInstanceTablePlans(modelConfig.RoutingInstances)
	if err != nil {
		return nil, err
	}

	instances := make([]RoutingInstanceOperationalState, 0, len(modelConfig.RoutingInstances))
	for _, name := range sortedModelRoutingInstanceNames(modelConfig.RoutingInstances) {
		instance := modelConfig.RoutingInstances[name]
		if instance == nil {
			continue
		}
		plan := plans[name]
		instances = append(instances, RoutingInstanceOperationalState{
			Name:               name,
			InstanceType:       modelRoutingInstanceType(instance),
			RouteDistinguisher: instance.RouteDistinguisher,
			IPv4TableID:        plan.TableID,
			IPv6TableID:        plan.TableID,
			ImportTargets:      modelRoutingInstanceImportTargets(instance),
			ExportTargets:      modelRoutingInstanceExportTargets(instance),
			ImportPolicies:     append([]string(nil), instance.VRFImport...),
			ExportPolicies:     append([]string(nil), instance.VRFExport...),
			Interfaces:         append([]string(nil), plan.Interfaces...),
		})
	}
	return instances, nil
}

func sortedModelRoutingInstanceNames(instances map[string]*model.RoutingInstance) []string {
	names := make([]string, 0, len(instances))
	for name := range instances {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func modelRoutingInstanceType(instance *model.RoutingInstance) string {
	if instance == nil || instance.InstanceType == "" {
		return "vrf"
	}
	return instance.InstanceType
}

func modelRoutingInstanceImportTargets(instance *model.RoutingInstance) []string {
	if instance == nil {
		return nil
	}
	targets := make([]string, 0, len(instance.VRFTargetImport)+1)
	if instance.VRFTarget != "" {
		targets = append(targets, instance.VRFTarget)
	}
	targets = append(targets, instance.VRFTargetImport...)
	return targets
}

func modelRoutingInstanceExportTargets(instance *model.RoutingInstance) []string {
	if instance == nil {
		return nil
	}
	targets := make([]string, 0, len(instance.VRFTargetExport)+1)
	if instance.VRFTarget != "" {
		targets = append(targets, instance.VRFTarget)
	}
	targets = append(targets, instance.VRFTargetExport...)
	return targets
}

func hasArcaOperationalState(routes []RouteOperationalState, routingInstances []RoutingInstanceOperationalState, bgpNeighbors []BGPNeighborOperationalState, ospfNeighbors []OSPFNeighborOperationalState, ospf3Neighbors []OSPFNeighborOperationalState, bfdStatus *BFDOperationalState) bool {
	return len(routes) > 0 ||
		len(routingInstances) > 0 ||
		len(bgpNeighbors) > 0 ||
		len(ospfNeighbors) > 0 ||
		len(ospf3Neighbors) > 0 ||
		hasBFDOperationalState(bfdStatus)
}

func hasBFDOperationalState(status *BFDOperationalState) bool {
	if status == nil {
		return false
	}
	return !status.LastRun.IsZero() ||
		status.ConfiguredPeers != 0 ||
		status.ObservedPeers != 0 ||
		status.UpPeers != 0 ||
		status.DownPeers != 0 ||
		status.SessionDownEvents != 0 ||
		status.RxFailPackets != 0 ||
		len(status.Peers) != 0 ||
		len(status.Issues) != 0 ||
		status.LastError != ""
}

func filterRouteOperationalStates(routes []RouteOperationalState, xpathFilter *XPathFilter) []RouteOperationalState {
	return filterOperationalList(routes, xpathFilter, []string{"state", "routes", "route"}, func(route RouteOperationalState, key string) []string {
		switch key {
		case "prefix":
			return nonEmptyValues(route.Prefix)
		case "next-hop":
			return nonEmptyValues(route.NextHop)
		case "protocol":
			return nonEmptyValues(route.Protocol)
		case "metric":
			return []string{fmt.Sprintf("%d", route.Metric)}
		case "interface":
			return nonEmptyValues(route.Interface)
		case "active":
			return []string{fmt.Sprintf("%t", route.Active)}
		default:
			return nil
		}
	})
}

func filterRoutingInstanceOperationalStates(instances []RoutingInstanceOperationalState, xpathFilter *XPathFilter) []RoutingInstanceOperationalState {
	return filterOperationalList(instances, xpathFilter, []string{"state", "routing-instances", "instance"}, func(instance RoutingInstanceOperationalState, key string) []string {
		switch key {
		case "name":
			return nonEmptyValues(instance.Name)
		case "instance-type":
			return nonEmptyValues(instance.InstanceType)
		case "route-distinguisher":
			return nonEmptyValues(instance.RouteDistinguisher)
		case "ipv4-table-id":
			return []string{fmt.Sprintf("%d", instance.IPv4TableID)}
		case "ipv6-table-id":
			return []string{fmt.Sprintf("%d", instance.IPv6TableID)}
		case "import-target":
			return instance.ImportTargets
		case "export-target":
			return instance.ExportTargets
		case "import-policy":
			return instance.ImportPolicies
		case "export-policy":
			return instance.ExportPolicies
		case "interface":
			return instance.Interfaces
		default:
			return nil
		}
	})
}

func filterBGPOperationalNeighbors(neighbors []BGPNeighborOperationalState, xpathFilter *XPathFilter) []BGPNeighborOperationalState {
	return filterOperationalList(neighbors, xpathFilter, []string{"state", "protocols", "bgp", "neighbor"}, func(neighbor BGPNeighborOperationalState, key string) []string {
		switch key {
		case "peer-address":
			return nonEmptyValues(neighbor.PeerAddress)
		case "peer-as":
			return []string{fmt.Sprintf("%d", neighbor.PeerAS)}
		case "state":
			return nonEmptyValues(neighbor.State)
		case "uptime-seconds":
			return []string{fmt.Sprintf("%d", neighbor.UptimeSecs)}
		case "prefix-received":
			return []string{fmt.Sprintf("%d", neighbor.PrefixReceived)}
		case "prefix-sent":
			return []string{fmt.Sprintf("%d", neighbor.PrefixSent)}
		default:
			return nil
		}
	})
}

func filterOSPFOperationalNeighbors(neighbors []OSPFNeighborOperationalState, xpathFilter *XPathFilter, protocol string) []OSPFNeighborOperationalState {
	return filterOperationalList(neighbors, xpathFilter, []string{"state", "protocols", protocol, "neighbor"}, func(neighbor OSPFNeighborOperationalState, key string) []string {
		switch key {
		case "router-id":
			return nonEmptyValues(neighbor.RouterID)
		case "address":
			return nonEmptyValues(neighbor.Address)
		case "interface":
			return nonEmptyValues(neighbor.Interface)
		case "state":
			return nonEmptyValues(neighbor.State)
		case "role":
			return nonEmptyValues(neighbor.Role)
		case "priority":
			return []string{fmt.Sprintf("%d", neighbor.Priority)}
		case "dead-time-seconds":
			return []string{fmt.Sprintf("%d", neighbor.DeadTimeSecs)}
		case "uptime-seconds":
			return []string{fmt.Sprintf("%d", neighbor.UptimeSecs)}
		default:
			return nil
		}
	})
}

func filterBFDOperationalState(status *BFDOperationalState, xpathFilter *XPathFilter) *BFDOperationalState {
	if status == nil {
		return nil
	}
	filteredPeers := filterOperationalList(status.Peers, xpathFilter, []string{"state", "protocols", "bfd", "peer"}, func(peer BFDPeerOperationalState, key string) []string {
		switch key {
		case "address":
			return nonEmptyValues(peer.Peer)
		case "local-address":
			return nonEmptyValues(peer.LocalAddress)
		case "interface":
			return nonEmptyValues(peer.Interface)
		case "vrf":
			return nonEmptyValues(peer.VRF)
		case "status":
			return nonEmptyValues(peer.Status)
		case "diagnostic":
			return nonEmptyValues(peer.Diagnostic)
		case "remote-diagnostic":
			return nonEmptyValues(peer.RemoteDiagnostic)
		case "observed":
			return []string{fmt.Sprintf("%t", peer.Observed)}
		case "up":
			return []string{fmt.Sprintf("%t", peer.Up)}
		case "session-down-events":
			return []string{fmt.Sprintf("%d", peer.SessionDownEvents)}
		case "rx-fail-packets":
			return []string{fmt.Sprintf("%d", peer.RxFailPackets)}
		default:
			return nil
		}
	})
	if len(filteredPeers) == len(status.Peers) {
		return status
	}
	filtered := *status
	filtered.Peers = filteredPeers
	return &filtered
}

func filterOperationalList[T any](items []T, xpathFilter *XPathFilter, path []string, values func(T, string) []string) []T {
	segmentIndex, ok := xpathListSegmentIndex(xpathFilter, path)
	if !ok {
		return items
	}
	predicates := xpathFilter.Predicates[segmentIndex]
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		if operationalItemMatchesPredicates(item, predicates, values) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func operationalItemMatchesPredicates[T any](item T, predicates map[string]string, values func(T, string) []string) bool {
	for key, want := range predicates {
		if !stringSliceContains(values(item, key), want) {
			return false
		}
	}
	return true
}

func nonEmptyValues(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func includeOperationalSection(filter *Filter, names ...string) bool {
	return includeOperationalSectionPaths(filter, names)
}

func includeOperationalSectionPaths(filter *Filter, paths ...[]string) bool {
	if filter == nil {
		return true
	}
	filterType := normalizedFilterType(filter)
	switch filterType {
	case "xpath":
		for _, path := range paths {
			if filterMatchesEnhanced(filter, path) {
				return true
			}
		}
		return false
	case "", "subtree":
	default:
		return false
	}
	if len(bytes.TrimSpace(filter.Content)) == 0 {
		return true
	}
	for _, path := range paths {
		for _, name := range path {
			if filterMatches(filter, name) {
				return true
			}
		}
	}
	return false
}

func hasRoutingState(cfg *config.Config) bool {
	return cfg.RoutingOptions != nil || cfg.Protocols != nil
}

func writeSystemStateXML(buf *bytes.Buffer, cfg *config.Config, now time.Time) error {
	hostname := ""
	if cfg.System != nil {
		hostname = cfg.System.HostName
	}
	if hostname == "" {
		if osHostname, err := os.Hostname(); err == nil {
			hostname = osHostname
		}
	}

	buf.WriteString(`  <system xmlns="` + IETFSystemNS + `">` + "\n")
	buf.WriteString("    <system-state>\n")
	if hostname != "" {
		if err := writeEscapedElement(buf, "      ", "hostname", hostname); err != nil {
			return err
		}
	}
	buf.WriteString("      <platform>\n")
	if err := writeEscapedElement(buf, "        ", "os-name", runtime.GOOS); err != nil {
		return err
	}
	if err := writeEscapedElement(buf, "        ", "machine", runtime.GOARCH); err != nil {
		return err
	}
	buf.WriteString("      </platform>\n")
	buf.WriteString("      <clock>\n")
	if err := writeEscapedElement(buf, "        ", "current-datetime", now.Format(time.RFC3339)); err != nil {
		return err
	}
	buf.WriteString("      </clock>\n")
	buf.WriteString("    </system-state>\n")
	buf.WriteString("  </system>\n")
	return nil
}

func writeInterfaceStateXML(buf *bytes.Buffer, interfaces map[string]*config.Interface, states map[string]*InterfaceOperationalState, xpathFilter *XPathFilter) error {
	buf.WriteString(`  <interfaces xmlns="` + IETFInterfacesNS + `">` + "\n")
	for _, name := range sortedInterfaceStateNames(interfaces, states) {
		iface := interfaces[name]
		state := states[name]
		if iface == nil && state == nil {
			continue
		}
		if !interfaceStateMatchesXPathPredicates(xpathFilter, name, iface, state) {
			continue
		}
		buf.WriteString("    <interface>\n")
		if err := writeEscapedElement(buf, "      ", "name", name); err != nil {
			return err
		}
		if err := writeEscapedElement(buf, "      ", "admin-status", interfaceAdminStatus(state)); err != nil {
			return err
		}
		if err := writeEscapedElement(buf, "      ", "oper-status", interfaceOperStatus(state)); err != nil {
			return err
		}
		if state != nil && state.MAC != "" {
			if err := writeEscapedElement(buf, "      ", "phys-address", state.MAC); err != nil {
				return err
			}
		}
		if state != nil && state.QoSProfile != "" {
			if err := writeEscapedElement(buf, "      ", "qos-profile", state.QoSProfile); err != nil {
				return err
			}
		}
		if state != nil && state.IPv4TableID != 0 {
			fmt.Fprintf(buf, "      <ipv4-table-id>%d</ipv4-table-id>\n", state.IPv4TableID)
		}
		if state != nil && state.IPv6TableID != 0 {
			fmt.Fprintf(buf, "      <ipv6-table-id>%d</ipv6-table-id>\n", state.IPv6TableID)
		}
		if state != nil && state.Counters != nil {
			writeInterfaceCountersXML(buf, state.Counters)
		}
		if state != nil && state.Queues != nil {
			if err := writeInterfaceQueuesXML(buf, state.Queues); err != nil {
				return err
			}
		}
		if iface != nil && len(iface.Units) > 0 {
			buf.WriteString("      <addresses>\n")
			for _, unitNum := range sortedUnitKeys(iface.Units) {
				unit := iface.Units[unitNum]
				if unit == nil {
					continue
				}
				for _, familyName := range sortedConfigKeys(unit.Family) {
					family := unit.Family[familyName]
					if family == nil {
						continue
					}
					for _, addr := range family.Addresses {
						buf.WriteString("        <address>\n")
						fmt.Fprintf(buf, "          <unit>%d</unit>\n", unitNum)
						if err := writeEscapedElement(buf, "          ", "family", familyName); err != nil {
							return err
						}
						if err := writeEscapedElement(buf, "          ", "ip", addr); err != nil {
							return err
						}
						buf.WriteString("        </address>\n")
					}
				}
			}
			buf.WriteString("      </addresses>\n")
		}
		buf.WriteString("    </interface>\n")
	}
	buf.WriteString("  </interfaces>\n")
	return nil
}

func sortedInterfaceStateNames(interfaces map[string]*config.Interface, states map[string]*InterfaceOperationalState) []string {
	seen := make(map[string]struct{}, len(interfaces)+len(states))
	names := make([]string, 0, len(interfaces)+len(states))
	for name := range interfaces {
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range states {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func interfaceAdminStatus(state *InterfaceOperationalState) string {
	if state != nil && state.AdminStatus != "" {
		return state.AdminStatus
	}
	return "configured"
}

func interfaceOperStatus(state *InterfaceOperationalState) string {
	if state != nil && state.OperStatus != "" {
		return state.OperStatus
	}
	return "unknown"
}

func writeInterfaceCountersXML(buf *bytes.Buffer, counters *InterfaceOperationalCounters) {
	buf.WriteString("      <statistics>\n")
	fmt.Fprintf(buf, "        <rx-packets>%d</rx-packets>\n", counters.RxPackets)
	fmt.Fprintf(buf, "        <tx-packets>%d</tx-packets>\n", counters.TxPackets)
	fmt.Fprintf(buf, "        <rx-bytes>%d</rx-bytes>\n", counters.RxBytes)
	fmt.Fprintf(buf, "        <tx-bytes>%d</tx-bytes>\n", counters.TxBytes)
	fmt.Fprintf(buf, "        <rx-errors>%d</rx-errors>\n", counters.RxErrors)
	fmt.Fprintf(buf, "        <tx-errors>%d</tx-errors>\n", counters.TxErrors)
	fmt.Fprintf(buf, "        <drops>%d</drops>\n", counters.Drops)
	buf.WriteString("      </statistics>\n")
}

func writeInterfaceQueuesXML(buf *bytes.Buffer, queues *InterfaceOperationalQueues) error {
	buf.WriteString("      <queue-placements>\n")
	if len(queues.Rx) > 0 {
		buf.WriteString("        <rx-queues>\n")
		for _, queue := range queues.Rx {
			buf.WriteString("          <rx-queue>\n")
			fmt.Fprintf(buf, "            <queue-id>%d</queue-id>\n", queue.QueueID)
			fmt.Fprintf(buf, "            <worker-id>%d</worker-id>\n", queue.WorkerID)
			if queue.Mode != "" {
				if err := writeEscapedElement(buf, "            ", "mode", queue.Mode); err != nil {
					return err
				}
			}
			buf.WriteString("          </rx-queue>\n")
		}
		buf.WriteString("        </rx-queues>\n")
	}
	if len(queues.Tx) > 0 {
		buf.WriteString("        <tx-queues>\n")
		for _, queue := range queues.Tx {
			buf.WriteString("          <tx-queue>\n")
			fmt.Fprintf(buf, "            <queue-id>%d</queue-id>\n", queue.QueueID)
			fmt.Fprintf(buf, "            <shared>%t</shared>\n", queue.Shared)
			if len(queue.Threads) > 0 {
				buf.WriteString("            <threads>\n")
				for _, thread := range queue.Threads {
					fmt.Fprintf(buf, "              <thread>%d</thread>\n", thread)
				}
				buf.WriteString("            </threads>\n")
			}
			buf.WriteString("          </tx-queue>\n")
		}
		buf.WriteString("        </tx-queues>\n")
	}
	buf.WriteString("      </queue-placements>\n")
	return nil
}

func writeRoutingStateXML(buf *bytes.Buffer, cfg *config.Config, xpathFilter *XPathFilter) error {
	buf.WriteString(`  <routing xmlns="` + IETFRoutingNS + `">` + "\n")
	buf.WriteString("    <routing-state>\n")
	if cfg.RoutingOptions != nil && len(cfg.RoutingOptions.StaticRoutes) > 0 {
		buf.WriteString("      <routes>\n")
		for _, route := range cfg.RoutingOptions.StaticRoutes {
			if route == nil {
				continue
			}
			if !routingStateRouteMatchesXPathPredicates(xpathFilter, route) {
				continue
			}
			buf.WriteString("        <route>\n")
			if err := writeEscapedElement(buf, "          ", "destination-prefix", route.Prefix); err != nil {
				return err
			}
			if err := writeEscapedElement(buf, "          ", "next-hop", route.NextHop); err != nil {
				return err
			}
			if err := writeEscapedElement(buf, "          ", "source-protocol", "static"); err != nil {
				return err
			}
			if route.Distance > 0 {
				fmt.Fprintf(buf, "          <metric>%d</metric>\n", route.Distance)
			}
			buf.WriteString("        </route>\n")
		}
		buf.WriteString("      </routes>\n")
	}
	if cfg.Protocols != nil {
		buf.WriteString("      <routing-protocols>\n")
		if cfg.Protocols.BGP != nil {
			name := "BGP"
			if cfg.RoutingOptions != nil && cfg.RoutingOptions.AutonomousSystem != 0 {
				name = fmt.Sprintf("BGP-%d", cfg.RoutingOptions.AutonomousSystem)
			}
			if err := writeRoutingProtocolXML(buf, xpathFilter, "bgp", name); err != nil {
				return err
			}
		}
		if cfg.Protocols.OSPF != nil {
			if err := writeRoutingProtocolXML(buf, xpathFilter, "ospf", "OSPF"); err != nil {
				return err
			}
		}
		buf.WriteString("      </routing-protocols>\n")
	}
	buf.WriteString("    </routing-state>\n")
	buf.WriteString("  </routing>\n")
	return nil
}

func routingStateRouteMatchesXPathPredicates(xpathFilter *XPathFilter, route *config.StaticRoute) bool {
	segmentIndex, ok := xpathListSegmentIndex(xpathFilter, []string{"routing", "routing-state", "routes", "route"})
	if !ok {
		return true
	}
	for key, want := range xpathFilter.Predicates[segmentIndex] {
		var got string
		switch key {
		case "destination-prefix":
			got = route.Prefix
		case "next-hop":
			got = route.NextHop
		case "source-protocol":
			got = "static"
		case "metric":
			if route.Distance == 0 {
				return false
			}
			got = strconv.Itoa(route.Distance)
		default:
			return false
		}
		if got != want {
			return false
		}
	}
	return true
}

func writeRoutingProtocolXML(buf *bytes.Buffer, xpathFilter *XPathFilter, protocolType, name string) error {
	const adminStatus = "configured"
	if !routingProtocolMatchesXPathPredicates(xpathFilter, protocolType, name, adminStatus) {
		return nil
	}
	buf.WriteString("        <routing-protocol>\n")
	if err := writeEscapedElement(buf, "          ", "type", protocolType); err != nil {
		return err
	}
	if err := writeEscapedElement(buf, "          ", "name", name); err != nil {
		return err
	}
	if err := writeEscapedElement(buf, "          ", "admin-status", adminStatus); err != nil {
		return err
	}
	buf.WriteString("        </routing-protocol>\n")
	return nil
}

func routingProtocolMatchesXPathPredicates(xpathFilter *XPathFilter, protocolType, name, adminStatus string) bool {
	segmentIndex, ok := xpathListSegmentIndex(xpathFilter, []string{"routing", "routing-state", "routing-protocols", "routing-protocol"})
	if !ok {
		return true
	}
	for key, want := range xpathFilter.Predicates[segmentIndex] {
		var got string
		switch key {
		case "type":
			got = protocolType
		case "name":
			got = name
		case "admin-status":
			got = adminStatus
		default:
			return false
		}
		if got != want {
			return false
		}
	}
	return true
}

func writeArcaStateXML(buf *bytes.Buffer, routes []RouteOperationalState, routingInstances []RoutingInstanceOperationalState, bgpNeighbors []BGPNeighborOperationalState, ospfNeighbors []OSPFNeighborOperationalState, ospf3Neighbors []OSPFNeighborOperationalState, bfdStatus *BFDOperationalState) error {
	buf.WriteString(`  <state xmlns="` + ArcaConfigNS + `">` + "\n")
	if len(routes) > 0 {
		if err := writeRouteOperationalStateXML(buf, routes); err != nil {
			return err
		}
	}
	if len(routingInstances) > 0 {
		if err := writeRoutingInstanceOperationalStateXML(buf, routingInstances); err != nil {
			return err
		}
	}
	if len(bgpNeighbors) > 0 || len(ospfNeighbors) > 0 || len(ospf3Neighbors) > 0 || hasBFDOperationalState(bfdStatus) {
		buf.WriteString("    <protocols>\n")
		if len(bgpNeighbors) > 0 {
			if err := writeBGPOperationalStateXML(buf, bgpNeighbors); err != nil {
				return err
			}
		}
		if len(ospfNeighbors) > 0 {
			if err := writeOSPFOperationalStateXML(buf, "ospf", ospfNeighbors); err != nil {
				return err
			}
		}
		if len(ospf3Neighbors) > 0 {
			if err := writeOSPFOperationalStateXML(buf, "ospf3", ospf3Neighbors); err != nil {
				return err
			}
		}
		if hasBFDOperationalState(bfdStatus) {
			if err := writeBFDOperationalStateXML(buf, bfdStatus); err != nil {
				return err
			}
		}
		buf.WriteString("    </protocols>\n")
	}
	buf.WriteString("  </state>\n")
	return nil
}

func writeRouteOperationalStateXML(buf *bytes.Buffer, routes []RouteOperationalState) error {
	buf.WriteString("    <routes>\n")
	for _, route := range routes {
		buf.WriteString("      <route>\n")
		if err := writeEscapedElement(buf, "        ", "prefix", route.Prefix); err != nil {
			return err
		}
		if route.NextHop != "" {
			if err := writeEscapedElement(buf, "        ", "next-hop", route.NextHop); err != nil {
				return err
			}
		}
		if route.Protocol != "" {
			if err := writeEscapedElement(buf, "        ", "protocol", route.Protocol); err != nil {
				return err
			}
		}
		fmt.Fprintf(buf, "        <metric>%d</metric>\n", route.Metric)
		if route.Interface != "" {
			if err := writeEscapedElement(buf, "        ", "interface", route.Interface); err != nil {
				return err
			}
		}
		fmt.Fprintf(buf, "        <active>%t</active>\n", route.Active)
		buf.WriteString("      </route>\n")
	}
	buf.WriteString("    </routes>\n")
	return nil
}

func writeRoutingInstanceOperationalStateXML(buf *bytes.Buffer, instances []RoutingInstanceOperationalState) error {
	buf.WriteString("    <routing-instances>\n")
	for _, instance := range instances {
		buf.WriteString("      <instance>\n")
		if err := writeEscapedElement(buf, "        ", "name", instance.Name); err != nil {
			return err
		}
		if err := writeEscapedElement(buf, "        ", "instance-type", instance.InstanceType); err != nil {
			return err
		}
		if instance.RouteDistinguisher != "" {
			if err := writeEscapedElement(buf, "        ", "route-distinguisher", instance.RouteDistinguisher); err != nil {
				return err
			}
		}
		fmt.Fprintf(buf, "        <ipv4-table-id>%d</ipv4-table-id>\n", instance.IPv4TableID)
		fmt.Fprintf(buf, "        <ipv6-table-id>%d</ipv6-table-id>\n", instance.IPv6TableID)
		for _, target := range instance.ImportTargets {
			if err := writeEscapedElement(buf, "        ", "import-target", target); err != nil {
				return err
			}
		}
		for _, target := range instance.ExportTargets {
			if err := writeEscapedElement(buf, "        ", "export-target", target); err != nil {
				return err
			}
		}
		for _, policy := range instance.ImportPolicies {
			if err := writeEscapedElement(buf, "        ", "import-policy", policy); err != nil {
				return err
			}
		}
		for _, policy := range instance.ExportPolicies {
			if err := writeEscapedElement(buf, "        ", "export-policy", policy); err != nil {
				return err
			}
		}
		for _, iface := range instance.Interfaces {
			if err := writeEscapedElement(buf, "        ", "interface", iface); err != nil {
				return err
			}
		}
		buf.WriteString("      </instance>\n")
	}
	buf.WriteString("    </routing-instances>\n")
	return nil
}

func writeBGPOperationalStateXML(buf *bytes.Buffer, neighbors []BGPNeighborOperationalState) error {
	buf.WriteString("      <bgp>\n")
	for _, neighbor := range neighbors {
		buf.WriteString("        <neighbor>\n")
		if err := writeEscapedElement(buf, "          ", "peer-address", neighbor.PeerAddress); err != nil {
			return err
		}
		fmt.Fprintf(buf, "          <peer-as>%d</peer-as>\n", neighbor.PeerAS)
		if neighbor.State != "" {
			if err := writeEscapedElement(buf, "          ", "state", neighbor.State); err != nil {
				return err
			}
		}
		fmt.Fprintf(buf, "          <uptime-seconds>%d</uptime-seconds>\n", neighbor.UptimeSecs)
		fmt.Fprintf(buf, "          <prefix-received>%d</prefix-received>\n", neighbor.PrefixReceived)
		fmt.Fprintf(buf, "          <prefix-sent>%d</prefix-sent>\n", neighbor.PrefixSent)
		buf.WriteString("        </neighbor>\n")
	}
	buf.WriteString("      </bgp>\n")
	return nil
}

func writeOSPFOperationalStateXML(buf *bytes.Buffer, element string, neighbors []OSPFNeighborOperationalState) error {
	fmt.Fprintf(buf, "      <%s>\n", element)
	for _, neighbor := range sortedOSPFOperationalNeighbors(neighbors) {
		buf.WriteString("        <neighbor>\n")
		if err := writeEscapedElement(buf, "          ", "router-id", neighbor.RouterID); err != nil {
			return err
		}
		if neighbor.Address != "" {
			if err := writeEscapedElement(buf, "          ", "address", neighbor.Address); err != nil {
				return err
			}
		}
		if neighbor.Interface != "" {
			if err := writeEscapedElement(buf, "          ", "interface", neighbor.Interface); err != nil {
				return err
			}
		}
		if neighbor.State != "" {
			if err := writeEscapedElement(buf, "          ", "state", neighbor.State); err != nil {
				return err
			}
		}
		if neighbor.Role != "" {
			if err := writeEscapedElement(buf, "          ", "role", neighbor.Role); err != nil {
				return err
			}
		}
		fmt.Fprintf(buf, "          <priority>%d</priority>\n", neighbor.Priority)
		fmt.Fprintf(buf, "          <dead-time-seconds>%d</dead-time-seconds>\n", neighbor.DeadTimeSecs)
		fmt.Fprintf(buf, "          <uptime-seconds>%d</uptime-seconds>\n", neighbor.UptimeSecs)
		buf.WriteString("        </neighbor>\n")
	}
	fmt.Fprintf(buf, "      </%s>\n", element)
	return nil
}

func writeBFDOperationalStateXML(buf *bytes.Buffer, status *BFDOperationalState) error {
	buf.WriteString("      <bfd>\n")
	if !status.LastRun.IsZero() {
		if err := writeEscapedElement(buf, "        ", "last-run", status.LastRun.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	fmt.Fprintf(buf, "        <configured-peers>%d</configured-peers>\n", status.ConfiguredPeers)
	fmt.Fprintf(buf, "        <observed-peers>%d</observed-peers>\n", status.ObservedPeers)
	fmt.Fprintf(buf, "        <up-peers>%d</up-peers>\n", status.UpPeers)
	fmt.Fprintf(buf, "        <down-peers>%d</down-peers>\n", status.DownPeers)
	fmt.Fprintf(buf, "        <session-down-events>%d</session-down-events>\n", status.SessionDownEvents)
	fmt.Fprintf(buf, "        <rx-fail-packets>%d</rx-fail-packets>\n", status.RxFailPackets)
	for _, peer := range sortedBFDOperationalPeers(status.Peers) {
		if err := writeBFDOperationalPeerXML(buf, peer); err != nil {
			return err
		}
	}
	for _, issue := range status.Issues {
		if err := writeEscapedElement(buf, "        ", "issue", issue); err != nil {
			return err
		}
	}
	if status.LastError != "" {
		if err := writeEscapedElement(buf, "        ", "last-error", status.LastError); err != nil {
			return err
		}
	}
	buf.WriteString("      </bfd>\n")
	return nil
}

func writeBFDOperationalPeerXML(buf *bytes.Buffer, peer BFDPeerOperationalState) error {
	buf.WriteString("        <peer>\n")
	if err := writeEscapedElement(buf, "          ", "address", peer.Peer); err != nil {
		return err
	}
	if peer.LocalAddress != "" {
		if err := writeEscapedElement(buf, "          ", "local-address", peer.LocalAddress); err != nil {
			return err
		}
	}
	if peer.Interface != "" {
		if err := writeEscapedElement(buf, "          ", "interface", peer.Interface); err != nil {
			return err
		}
	}
	if peer.VRF != "" {
		if err := writeEscapedElement(buf, "          ", "vrf", peer.VRF); err != nil {
			return err
		}
	}
	if peer.Status != "" {
		if err := writeEscapedElement(buf, "          ", "status", peer.Status); err != nil {
			return err
		}
	}
	if peer.Diagnostic != "" {
		if err := writeEscapedElement(buf, "          ", "diagnostic", peer.Diagnostic); err != nil {
			return err
		}
	}
	if peer.RemoteDiagnostic != "" {
		if err := writeEscapedElement(buf, "          ", "remote-diagnostic", peer.RemoteDiagnostic); err != nil {
			return err
		}
	}
	fmt.Fprintf(buf, "          <observed>%t</observed>\n", peer.Observed)
	fmt.Fprintf(buf, "          <up>%t</up>\n", peer.Up)
	fmt.Fprintf(buf, "          <session-down-events>%d</session-down-events>\n", peer.SessionDownEvents)
	fmt.Fprintf(buf, "          <rx-fail-packets>%d</rx-fail-packets>\n", peer.RxFailPackets)
	buf.WriteString("        </peer>\n")
	return nil
}

func sortedBFDOperationalPeers(peers []BFDPeerOperationalState) []BFDPeerOperationalState {
	sorted := append([]BFDPeerOperationalState(nil), peers...)
	sort.Slice(sorted, func(i, j int) bool {
		return bfdOperationalPeerSortKey(sorted[i]) < bfdOperationalPeerSortKey(sorted[j])
	})
	return sorted
}

func sortedOSPFOperationalNeighbors(neighbors []OSPFNeighborOperationalState) []OSPFNeighborOperationalState {
	sorted := append([]OSPFNeighborOperationalState(nil), neighbors...)
	sort.Slice(sorted, func(i, j int) bool {
		return ospfOperationalNeighborSortKey(sorted[i]) < ospfOperationalNeighborSortKey(sorted[j])
	})
	return sorted
}

func ospfOperationalNeighborSortKey(neighbor OSPFNeighborOperationalState) string {
	return neighbor.RouterID + "\x00" + neighbor.Interface + "\x00" + neighbor.Address
}

func bfdOperationalPeerSortKey(peer BFDPeerOperationalState) string {
	return peer.Peer + "\x00" + peer.LocalAddress + "\x00" + peer.Interface + "\x00" + peer.VRF
}

func writeEscapedElement(buf *bytes.Buffer, indent, name, value string) error {
	fmt.Fprintf(buf, "%s<%s>", indent, name)
	if err := xml.EscapeText(buf, []byte(value)); err != nil {
		return err
	}
	fmt.Fprintf(buf, "</%s>\n", name)
	return nil
}

func sortedConfigKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedUnitKeys(m map[int]*config.Unit) []int {
	keys := make([]int, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}
