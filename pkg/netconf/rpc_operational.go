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

	interfaceStates := s.collectInterfaceOperationalState(ctx, filter)
	bfdStatus := s.collectBFDOperationalState(ctx, filter)
	return buildOperationalData(cfg, filter, time.Now().UTC(), interfaceStates, bfdStatus)
}

// GetOperationalData builds operational state without a datastore-backed
// server. It is kept for tests and callers that only need local system state.
func GetOperationalData(ctx context.Context, filter *Filter) ([]byte, error) {
	_ = ctx
	return buildOperationalData(config.NewConfig(), filter, time.Now().UTC(), nil, nil)
}

// buildAllOperationalData builds operational data XML for the inside of <data>.
func buildAllOperationalData() string {
	data, err := buildOperationalData(config.NewConfig(), nil, time.Now().UTC(), nil, nil)
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

func buildOperationalData(cfg *config.Config, filter *Filter, now time.Time, interfaceStates map[string]*InterfaceOperationalState, bfdStatus *BFDOperationalState) ([]byte, error) {
	if cfg == nil {
		cfg = config.NewConfig()
	}

	var buf bytes.Buffer
	if includeOperationalSection(filter, "system") {
		if err := writeSystemStateXML(&buf, cfg, now); err != nil {
			return nil, err
		}
	}
	if includeOperationalSection(filter, "interfaces") && (len(cfg.Interfaces) > 0 || len(interfaceStates) > 0) {
		if err := writeInterfaceStateXML(&buf, cfg.Interfaces, interfaceStates); err != nil {
			return nil, err
		}
	}
	if includeOperationalSection(filter, "routing", "routing-state", "routing-protocols", "routes") && hasRoutingState(cfg) {
		if err := writeRoutingStateXML(&buf, cfg); err != nil {
			return nil, err
		}
	}
	if !includeOperationalSection(filter, "state", "protocols", "bfd") {
		bfdStatus = nil
	}
	routingInstances, err := collectRoutingInstanceOperationalState(cfg, filter)
	if err != nil {
		return nil, err
	}
	if hasArcaOperationalState(routingInstances, bfdStatus) {
		if err := writeArcaStateXML(&buf, routingInstances, bfdStatus); err != nil {
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

func hasArcaOperationalState(routingInstances []RoutingInstanceOperationalState, bfdStatus *BFDOperationalState) bool {
	return len(routingInstances) > 0 || hasBFDOperationalState(bfdStatus)
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

func includeOperationalSection(filter *Filter, names ...string) bool {
	if filter == nil || len(bytes.TrimSpace(filter.Content)) == 0 {
		return true
	}
	for _, name := range names {
		if filterMatches(filter, name) {
			return true
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

	buf.WriteString(`  <system xmlns="urn:ietf:params:xml:ns:yang:ietf-system">` + "\n")
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

func writeInterfaceStateXML(buf *bytes.Buffer, interfaces map[string]*config.Interface, states map[string]*InterfaceOperationalState) error {
	buf.WriteString(`  <interfaces xmlns="` + IETFInterfacesNS + `">` + "\n")
	for _, name := range sortedInterfaceStateNames(interfaces, states) {
		iface := interfaces[name]
		state := states[name]
		if iface == nil && state == nil {
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

func writeRoutingStateXML(buf *bytes.Buffer, cfg *config.Config) error {
	buf.WriteString(`  <routing xmlns="` + IETFRoutingNS + `">` + "\n")
	buf.WriteString("    <routing-state>\n")
	if cfg.RoutingOptions != nil && len(cfg.RoutingOptions.StaticRoutes) > 0 {
		buf.WriteString("      <routes>\n")
		for _, route := range cfg.RoutingOptions.StaticRoutes {
			if route == nil {
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
			if err := writeRoutingProtocolXML(buf, "bgp", name); err != nil {
				return err
			}
		}
		if cfg.Protocols.OSPF != nil {
			if err := writeRoutingProtocolXML(buf, "ospf", "OSPF"); err != nil {
				return err
			}
		}
		buf.WriteString("      </routing-protocols>\n")
	}
	buf.WriteString("    </routing-state>\n")
	buf.WriteString("  </routing>\n")
	return nil
}

func writeRoutingProtocolXML(buf *bytes.Buffer, protocolType, name string) error {
	buf.WriteString("        <routing-protocol>\n")
	if err := writeEscapedElement(buf, "          ", "type", protocolType); err != nil {
		return err
	}
	if err := writeEscapedElement(buf, "          ", "name", name); err != nil {
		return err
	}
	if err := writeEscapedElement(buf, "          ", "admin-status", "configured"); err != nil {
		return err
	}
	buf.WriteString("        </routing-protocol>\n")
	return nil
}

func writeArcaStateXML(buf *bytes.Buffer, routingInstances []RoutingInstanceOperationalState, bfdStatus *BFDOperationalState) error {
	buf.WriteString(`  <state xmlns="` + ArcaConfigNS + `">` + "\n")
	if len(routingInstances) > 0 {
		if err := writeRoutingInstanceOperationalStateXML(buf, routingInstances); err != nil {
			return err
		}
	}
	if hasBFDOperationalState(bfdStatus) {
		buf.WriteString("    <protocols>\n")
		if err := writeBFDOperationalStateXML(buf, bfdStatus); err != nil {
			return err
		}
		buf.WriteString("    </protocols>\n")
	}
	buf.WriteString("  </state>\n")
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
