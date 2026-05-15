// Package grpc provides the internal gRPC client for arca to communicate
// with the arca-routerd engine over a Unix domain socket.
package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	apiv1 "github.com/akam1o/arca-router/api/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is the gRPC client that arca uses to talk to arca-routerd.
// It provides high-level methods for config management, session control,
// and operational state queries.
type Client struct {
	conn      *googlegrpc.ClientConn
	config    apiv1.ConfigServiceClient
	session   apiv1.SessionServiceClient
	state     apiv1.StateServiceClient
	telemetry apiv1.TelemetryServiceClient
}

// Dial connects to the arca-routerd gRPC server via Unix socket.
func Dial(socketPath string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := googlegrpc.NewClient("unix://"+socketPath,
		googlegrpc.WithTransportCredentials(insecure.NewCredentials()),
		googlegrpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial arca-routerd at %s: %w", socketPath, err)
	}
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}
		if !conn.WaitForStateChange(ctx, state) {
			_ = conn.Close()
			return nil, fmt.Errorf("dial arca-routerd at %s: %w", socketPath, ctx.Err())
		}
	}

	return &Client{
		conn:      conn,
		config:    apiv1.NewConfigServiceClient(conn),
		session:   apiv1.NewSessionServiceClient(conn),
		state:     apiv1.NewStateServiceClient(conn),
		telemetry: apiv1.NewTelemetryServiceClient(conn),
	}, nil
}

// Close closes the gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// --- Config operations ---

// GetRunning returns the running configuration text and version.
func (c *Client) GetRunning(ctx context.Context) (configText string, version uint64, err error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.GetRunning(ctx, &apiv1.GetRunningRequest{})
	if err != nil {
		return "", 0, err
	}
	return resp.GetConfigText(), resp.GetVersion(), nil
}

// GetCandidate returns a session's candidate configuration text.
func (c *Client) GetCandidate(ctx context.Context, sessionID string) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.GetCandidate(ctx, &apiv1.GetCandidateRequest{SessionId: sessionID})
	if err != nil {
		return "", err
	}
	return resp.GetConfigText(), nil
}

// EditCandidate sends set-command text to the candidate config.
func (c *Client) EditCandidate(ctx context.Context, sessionID, configText string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.config.EditCandidate(ctx, &apiv1.EditCandidateRequest{
		SessionId:  sessionID,
		ConfigText: configText,
	})
	return err
}

// Commit commits the candidate configuration.
func (c *Client) Commit(ctx context.Context, sessionID, user, message string) (commitID string, version uint64, err error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.Commit(ctx, &apiv1.CommitRequest{
		SessionId: sessionID,
		User:      user,
		Message:   message,
	})
	if err != nil {
		return "", 0, err
	}
	return resp.GetCommitId(), resp.GetVersion(), nil
}

// ValidateCandidate validates a session's candidate configuration without committing it.
func (c *Client) ValidateCandidate(ctx context.Context, sessionID string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.config.ValidateCandidate(ctx, &apiv1.ValidateCandidateRequest{SessionId: sessionID})
	return err
}

// Discard discards candidate changes.
func (c *Client) Discard(ctx context.Context, sessionID string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.config.Discard(ctx, &apiv1.DiscardRequest{SessionId: sessionID})
	return err
}

// Rollback rolls back running configuration to a previous commit.
func (c *Client) Rollback(ctx context.Context, sessionID, commitID, user, message string) (newCommitID string, version uint64, err error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.Rollback(ctx, &apiv1.RollbackRequest{
		SessionId: sessionID,
		CommitId:  commitID,
		User:      user,
		Message:   message,
	})
	if err != nil {
		return "", 0, err
	}
	return resp.GetNewCommitId(), resp.GetVersion(), nil
}

// Diff returns the diff between candidate and running.
func (c *Client) Diff(ctx context.Context, sessionID string) (diffText string, hasChanges bool, err error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.Diff(ctx, &apiv1.DiffRequest{SessionId: sessionID})
	if err != nil {
		return "", false, err
	}
	return resp.GetDiffText(), resp.GetHasChanges(), nil
}

// ListHistory returns commit history.
func (c *Client) ListHistory(ctx context.Context, limit, offset int) ([]CommitInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.ListHistory(ctx, &apiv1.ListHistoryRequest{Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, err
	}
	return commitInfosFromProto(resp.GetEntries()), nil
}

// --- Session operations ---

// CreateSession creates a new configuration session.
func (c *Client) CreateSession(ctx context.Context, user string) (sessionID string, err error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.session.CreateSession(ctx, &apiv1.CreateSessionRequest{User: user})
	if err != nil {
		return "", err
	}
	return resp.GetSessionId(), nil
}

// CloseSession closes a configuration session.
func (c *Client) CloseSession(ctx context.Context, sessionID string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.session.CloseSession(ctx, &apiv1.CloseSessionRequest{SessionId: sessionID})
	return err
}

// AcquireLock acquires the candidate lock.
func (c *Client) AcquireLock(ctx context.Context, sessionID, user string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.session.AcquireLock(ctx, &apiv1.AcquireLockRequest{
		SessionId: sessionID,
		User:      user,
	})
	return err
}

// ReleaseLock releases the candidate lock.
func (c *Client) ReleaseLock(ctx context.Context, sessionID string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.session.ReleaseLock(ctx, &apiv1.ReleaseLockRequest{SessionId: sessionID})
	return err
}

// --- State queries ---

// GetInterfaces returns interface operational state.
func (c *Client) GetInterfaces(ctx context.Context, nameFilter string) ([]InterfaceInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetInterfaces(ctx, &apiv1.GetInterfacesRequest{NameFilter: nameFilter})
	if err != nil {
		return nil, err
	}
	return interfaceInfosFromProto(resp.GetInterfaces()), nil
}

// GetRoutes returns routing table entries.
func (c *Client) GetRoutes(ctx context.Context, prefixFilter, protoFilter string) ([]RouteInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetRoutes(ctx, &apiv1.GetRoutesRequest{
		PrefixFilter:   prefixFilter,
		ProtocolFilter: protoFilter,
	})
	if err != nil {
		return nil, err
	}
	return routeInfosFromProto(resp.GetRoutes()), nil
}

// GetBGPNeighbors returns BGP neighbor state.
func (c *Client) GetBGPNeighbors(ctx context.Context) ([]BGPNeighborInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetBGPNeighbors(ctx, &apiv1.GetBGPNeighborsRequest{})
	if err != nil {
		return nil, err
	}
	return bgpNeighborInfosFromProto(resp.GetNeighbors()), nil
}

// GetOSPFNeighbors returns OSPFv2 or OSPFv3 neighbor state.
func (c *Client) GetOSPFNeighbors(ctx context.Context, addressFamily string) ([]OSPFNeighborInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetOSPFNeighbors(ctx, &apiv1.GetOSPFNeighborsRequest{AddressFamily: addressFamily})
	if err != nil {
		return nil, err
	}
	return ospfNeighborInfosFromProto(resp.GetNeighbors()), nil
}

// GetRouteText returns FRR routing table output.
func (c *Client) GetRouteText(ctx context.Context, protoFilter, addressFamily string) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetRouteText(ctx, &apiv1.GetRouteTextRequest{
		ProtocolFilter: protoFilter,
		AddressFamily:  addressFamily,
	})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetBGPSummaryText returns FRR BGP summary output.
func (c *Client) GetBGPSummaryText(ctx context.Context) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetBGPSummaryText(ctx, &apiv1.GetBGPSummaryTextRequest{})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetBGPNeighborText returns FRR BGP neighbor detail output.
func (c *Client) GetBGPNeighborText(ctx context.Context, peerAddress string) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetBGPNeighborText(ctx, &apiv1.GetBGPNeighborTextRequest{PeerAddress: peerAddress})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetOSPFNeighborsText returns FRR OSPF neighbor output.
func (c *Client) GetOSPFNeighborsText(ctx context.Context, addressFamily string) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetOSPFNeighborsText(ctx, &apiv1.GetOSPFNeighborsTextRequest{AddressFamily: addressFamily})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetVRRPText returns FRR VRRP output.
func (c *Client) GetVRRPText(ctx context.Context) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetVRRPText(ctx, &apiv1.GetVRRPTextRequest{})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetBFDText returns FRR BFD output.
func (c *Client) GetBFDText(ctx context.Context, peerAddress string, brief, counters bool) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetBFDText(ctx, &apiv1.GetBFDTextRequest{
		PeerAddress: peerAddress,
		Brief:       brief,
		Counters:    counters,
	})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetBFDStatus returns cached FRR BFD operational state.
func (c *Client) GetBFDStatus(ctx context.Context) (*BFDStatusInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetBFDStatus(ctx, &apiv1.GetBFDStatusRequest{})
	if err != nil {
		return nil, err
	}
	info := &BFDStatusInfo{
		ConfiguredPeers:   int(resp.GetConfiguredPeers()),
		ObservedPeers:     int(resp.GetObservedPeers()),
		UpPeers:           int(resp.GetUpPeers()),
		DownPeers:         int(resp.GetDownPeers()),
		SessionDownEvents: resp.GetSessionDownEvents(),
		RxFailPackets:     resp.GetRxFailPackets(),
		Issues:            append([]string(nil), resp.GetIssues()...),
		LastError:         resp.GetLastError(),
		Peers:             bfdPeerInfosFromProto(resp.GetPeers()),
	}
	if rawLastRun := resp.GetLastRun(); rawLastRun != "" {
		parsed, err := time.Parse(time.RFC3339Nano, rawLastRun)
		if err == nil {
			info.LastRun = parsed
		}
	}
	return info, nil
}

// GetLCPReconciliation returns cached VPP LCP reconciliation state.
func (c *Client) GetLCPReconciliation(ctx context.Context) (*LCPReconciliationInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetLCPReconciliation(ctx, &apiv1.GetLCPReconciliationRequest{})
	if err != nil {
		return nil, err
	}
	info := &LCPReconciliationInfo{
		PairCount:       int(resp.GetPairCount()),
		Inconsistencies: append([]string(nil), resp.GetInconsistencies()...),
		LastError:       resp.GetLastError(),
	}
	if rawLastRun := resp.GetLastRun(); rawLastRun != "" {
		parsed, err := time.Parse(time.RFC3339Nano, rawLastRun)
		if err == nil {
			info.LastRun = parsed
		}
	}
	return info, nil
}

// GetHAStatus returns control-plane HA convergence state.
func (c *Client) GetHAStatus(ctx context.Context) (*HAStatusInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetHAStatus(ctx, &apiv1.GetHAStatusRequest{})
	if err != nil {
		return nil, err
	}
	info := &HAStatusInfo{
		Configured:              resp.GetConfigured(),
		Converged:               resp.GetConverged(),
		VRRPGroups:              int(resp.GetVrrpGroups()),
		Issues:                  append([]string(nil), resp.GetIssues()...),
		ClusterEnabled:          resp.GetClusterEnabled(),
		ClusterNodes:            int(resp.GetClusterNodes()),
		ClusterEtcdSync:         resp.GetClusterEtcdSync(),
		ClusterSyncAligned:      resp.GetClusterSyncAligned(),
		FRRVRRPConfiguredGroups: int(resp.GetFrrVrrpConfiguredGroups()),
		FRRVRRPObservedGroups:   int(resp.GetFrrVrrpObservedGroups()),
		FRRVRRPActiveGroups:     int(resp.GetFrrVrrpActiveGroups()),
		FRRVRRPIssues:           append([]string(nil), resp.GetFrrVrrpIssues()...),
		FRRVRRPLastError:        resp.GetFrrVrrpLastError(),
		FRRBFDConfiguredPeers:   int(resp.GetFrrBfdConfiguredPeers()),
		FRRBFDObservedPeers:     int(resp.GetFrrBfdObservedPeers()),
		FRRBFDUpPeers:           int(resp.GetFrrBfdUpPeers()),
		FRRBFDDownPeers:         int(resp.GetFrrBfdDownPeers()),
		FRRBFDIssues:            append([]string(nil), resp.GetFrrBfdIssues()...),
		FRRBFDLastError:         resp.GetFrrBfdLastError(),
		VPPLCPPairs:             int(resp.GetVppLcpPairs()),
		VPPLCPInconsistencies:   append([]string(nil), resp.GetVppLcpInconsistencies()...),
		VPPLCPLastError:         resp.GetVppLcpLastError(),
	}
	if rawLastCheck := resp.GetFrrVrrpLastCheck(); rawLastCheck != "" {
		parsed, err := time.Parse(time.RFC3339Nano, rawLastCheck)
		if err == nil {
			info.FRRVRRPLastCheck = parsed
		}
	}
	if rawLastCheck := resp.GetFrrBfdLastCheck(); rawLastCheck != "" {
		parsed, err := time.Parse(time.RFC3339Nano, rawLastCheck)
		if err == nil {
			info.FRRBFDLastCheck = parsed
		}
	}
	if rawLastCheck := resp.GetVppLcpLastCheck(); rawLastCheck != "" {
		parsed, err := time.Parse(time.RFC3339Nano, rawLastCheck)
		if err == nil {
			info.VPPLCPLastCheck = parsed
		}
	}
	return info, nil
}

// GetRoutingInstances returns running routing-instance intent and table mapping.
func (c *Client) GetRoutingInstances(ctx context.Context) ([]RoutingInstanceInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetRoutingInstances(ctx, &apiv1.GetRoutingInstancesRequest{})
	if err != nil {
		return nil, err
	}
	instances := make([]RoutingInstanceInfo, 0, len(resp.GetInstances()))
	for _, instance := range resp.GetInstances() {
		if instance == nil {
			continue
		}
		instances = append(instances, RoutingInstanceInfo{
			Name:               instance.GetName(),
			InstanceType:       instance.GetInstanceType(),
			RouteDistinguisher: instance.GetRouteDistinguisher(),
			IPv4TableID:        instance.GetIpv4TableId(),
			IPv6TableID:        instance.GetIpv6TableId(),
			ImportTargets:      append([]string(nil), instance.GetImportTargets()...),
			ExportTargets:      append([]string(nil), instance.GetExportTargets()...),
			ImportPolicies:     append([]string(nil), instance.GetImportPolicies()...),
			ExportPolicies:     append([]string(nil), instance.GetExportPolicies()...),
			Interfaces:         append([]string(nil), instance.GetInterfaces()...),
		})
	}
	return instances, nil
}

// GetClassOfService returns running class-of-service intent.
func (c *Client) GetClassOfService(ctx context.Context) (*ClassOfServiceInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetClassOfService(ctx, &apiv1.GetClassOfServiceRequest{})
	if err != nil {
		return nil, err
	}
	info := &ClassOfServiceInfo{
		EnforcementStatus: resp.GetEnforcementStatus(),
	}
	if capabilities := resp.GetCapabilities(); capabilities != nil {
		info.Capabilities = &ClassOfServiceCapabilitiesInfo{
			MetadataBindingSupported: capabilities.GetMetadataBindingSupported(),
			QueueSchedulerSupported:  capabilities.GetQueueSchedulerSupported(),
			PolicerSupported:         capabilities.GetPolicerSupported(),
			CountersSupported:        capabilities.GetCountersSupported(),
			LastError:                capabilities.GetLastError(),
			Diagnostics:              append([]string(nil), capabilities.GetDiagnostics()...),
		}
		if rawLastCheck := capabilities.GetLastCheck(); rawLastCheck != "" {
			parsed, err := time.Parse(time.RFC3339Nano, rawLastCheck)
			if err == nil {
				info.Capabilities.LastCheck = parsed
			}
		}
	}
	for _, fc := range resp.GetForwardingClasses() {
		info.ForwardingClasses = append(info.ForwardingClasses, ClassOfServiceForwardingClassInfo{
			Name:  fc.GetName(),
			Queue: int(fc.GetQueue()),
		})
	}
	for _, profile := range resp.GetTrafficControlProfiles() {
		info.TrafficControlProfiles = append(info.TrafficControlProfiles, ClassOfServiceTrafficControlProfileInfo{
			Name:              profile.GetName(),
			ShapingRate:       profile.GetShapingRate(),
			SchedulerMap:      profile.GetSchedulerMap(),
			EnforcementStatus: profile.GetEnforcementStatus(),
		})
	}
	for _, iface := range resp.GetInterfaces() {
		info.Interfaces = append(info.Interfaces, ClassOfServiceInterfaceInfo{
			Name:                        iface.GetName(),
			OutputTrafficControlProfile: iface.GetOutputTrafficControlProfile(),
			EnforcementStatus:           iface.GetEnforcementStatus(),
		})
	}
	return info, nil
}

// GetSystemInfo returns system information.
func (c *Client) GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetSystemInfo(ctx, &apiv1.GetSystemInfoRequest{})
	if err != nil {
		return nil, err
	}
	return &SystemInfo{
		Hostname:   resp.GetHostname(),
		Version:    resp.GetVersion(),
		UptimeSecs: resp.GetUptimeSecs(),
	}, nil
}

// TelemetryReceiver receives structured telemetry events.
type TelemetryReceiver interface {
	Recv() (*TelemetryEvent, error)
}

// GetTelemetryCatalog returns the daemon's supported telemetry path catalog.
func (c *Client) GetTelemetryCatalog(ctx context.Context) (TelemetryCatalog, error) {
	return c.GetFilteredTelemetryCatalog(ctx, nil, nil)
}

// GetFilteredTelemetryCatalog returns the daemon's supported telemetry path catalog after server-side filters.
func (c *Client) GetFilteredTelemetryCatalog(ctx context.Context, cardinalities []string, payloadSchemas []string) (TelemetryCatalog, error) {
	return c.GetPathFilteredTelemetryCatalog(ctx, nil, cardinalities, payloadSchemas)
}

// GetPathFilteredTelemetryCatalog returns the daemon's supported telemetry path catalog after server-side path and metadata filters.
func (c *Client) GetPathFilteredTelemetryCatalog(ctx context.Context, paths []string, cardinalities []string, payloadSchemas []string) (TelemetryCatalog, error) {
	return c.GetTelemetryCatalogWithFilter(ctx, TelemetryCatalogFilter{
		Paths:          paths,
		Cardinalities:  cardinalities,
		PayloadSchemas: payloadSchemas,
	})
}

// GetTelemetryCatalogWithFilter returns the daemon's supported telemetry path catalog after server-side filters.
func (c *Client) GetTelemetryCatalogWithFilter(ctx context.Context, filter TelemetryCatalogFilter) (TelemetryCatalog, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.telemetry.GetTelemetryCatalog(ctx, &apiv1.GetTelemetryCatalogRequest{
		Path:          append([]string(nil), filter.Paths...),
		Cardinality:   append([]string(nil), filter.Cardinalities...),
		PayloadSchema: append([]string(nil), filter.PayloadSchemas...),
		Encoding:      append([]string(nil), filter.Encodings...),
		DefaultOnly:   filter.DefaultOnly,
	})
	if err != nil {
		return TelemetryCatalog{}, err
	}
	catalog := TelemetryCatalog{
		EventSchemaVersion:      resp.GetEventSchemaVersion(),
		Encoding:                resp.GetEncoding(),
		DefaultPaths:            append([]string(nil), resp.GetDefaultPaths()...),
		DefaultSampleIntervalMs: resp.GetDefaultSampleIntervalMs(),
		MinSampleIntervalMs:     resp.GetMinSampleIntervalMs(),
		MaxSampleIntervalMs:     resp.GetMaxSampleIntervalMs(),
		Paths:                   make([]TelemetryPathInfo, 0, len(resp.GetPaths())),
	}
	for _, path := range resp.GetPaths() {
		catalog.Paths = append(catalog.Paths, TelemetryPathInfo{
			Path:          path.GetPath(),
			Description:   path.GetDescription(),
			Cardinality:   path.GetCardinality(),
			PayloadSchema: path.GetPayloadSchema(),
			Aliases:       append([]string(nil), path.GetAliases()...),
			Default:       path.GetDefault(),
		})
	}
	return catalog, nil
}

// SubscribeTelemetry starts a structured telemetry stream.
func (c *Client) SubscribeTelemetry(ctx context.Context, paths []string, sampleInterval time.Duration, once bool) (TelemetryReceiver, error) {
	stream, err := c.telemetry.SubscribeTelemetry(ctx, &apiv1.SubscribeTelemetryRequest{
		Paths:            append([]string(nil), paths...),
		SampleIntervalMs: durationMillisUint32(sampleInterval),
		Once:             once,
	})
	if err != nil {
		return nil, err
	}
	return &TelemetryStream{stream: stream}, nil
}

// TelemetryStream receives structured telemetry events.
type TelemetryStream struct {
	stream apiv1.TelemetryService_SubscribeTelemetryClient
}

// Recv reads the next telemetry event from the stream.
func (s *TelemetryStream) Recv() (*TelemetryEvent, error) {
	resp, err := s.stream.Recv()
	if err != nil {
		return nil, err
	}
	event := &TelemetryEvent{
		Sequence:      resp.GetSequence(),
		Path:          resp.GetPath(),
		PayloadSchema: resp.GetPayloadSchema(),
		EventType:     resp.GetEventType(),
		Encoding:      resp.GetEncoding(),
		JSONPayload:   resp.GetJsonPayload(),
		SchemaVersion: resp.GetSchemaVersion(),
		PayloadBytes:  int(resp.GetPayloadBytes()),
	}
	if event.PayloadBytes == 0 && event.JSONPayload != "" {
		event.PayloadBytes = len(event.JSONPayload)
	}
	if rawTimestamp := resp.GetTimestamp(); rawTimestamp != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, rawTimestamp); err == nil {
			event.Timestamp = parsed
		}
	}
	return event, nil
}

func contextWithDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, 10*time.Second)
}

func durationMillisUint32(duration time.Duration) uint32 {
	if duration <= 0 {
		return 0
	}
	ms := duration / time.Millisecond
	if ms < 1 {
		return 1
	}
	if ms > time.Duration(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(ms)
}

func commitInfosFromProto(entries []*apiv1.CommitEntry) []CommitInfo {
	infos := make([]CommitInfo, 0, len(entries))
	for _, entry := range entries {
		timestamp, err := time.Parse(time.RFC3339Nano, entry.GetTimestamp())
		if err != nil {
			timestamp = time.Time{}
		}
		infos = append(infos, CommitInfo{
			CommitID:   entry.GetCommitId(),
			User:       entry.GetUser(),
			Timestamp:  timestamp,
			Message:    entry.GetMessage(),
			IsRollback: entry.GetIsRollback(),
		})
	}
	return infos
}

func interfaceInfosFromProto(interfaces []*apiv1.InterfaceState) []InterfaceInfo {
	infos := make([]InterfaceInfo, 0, len(interfaces))
	for _, iface := range interfaces {
		infos = append(infos, InterfaceInfo{
			Name:        iface.GetName(),
			AdminStatus: iface.GetAdminStatus(),
			OperStatus:  iface.GetOperStatus(),
			Speed:       iface.GetSpeed(),
			MTU:         iface.GetMtu(),
			MAC:         iface.GetMac(),
			QoSProfile:  iface.GetQosProfile(),
			IPv4TableID: iface.GetIpv4TableId(),
			IPv6TableID: iface.GetIpv6TableId(),
			RxPackets:   iface.GetRxPackets(),
			TxPackets:   iface.GetTxPackets(),
			RxBytes:     iface.GetRxBytes(),
			TxBytes:     iface.GetTxBytes(),
			RxErrors:    iface.GetRxErrors(),
			TxErrors:    iface.GetTxErrors(),
			RxQueues:    rxQueueInfosFromProto(iface.GetRxQueues()),
			TxQueues:    txQueueInfosFromProto(iface.GetTxQueues()),
		})
	}
	return infos
}

func rxQueueInfosFromProto(queues []*apiv1.InterfaceRxQueue) []InterfaceRxQueueInfo {
	infos := make([]InterfaceRxQueueInfo, 0, len(queues))
	for _, queue := range queues {
		infos = append(infos, InterfaceRxQueueInfo{
			QueueID:  queue.GetQueueId(),
			WorkerID: queue.GetWorkerId(),
			Mode:     queue.GetMode(),
		})
	}
	return infos
}

func txQueueInfosFromProto(queues []*apiv1.InterfaceTxQueue) []InterfaceTxQueueInfo {
	infos := make([]InterfaceTxQueueInfo, 0, len(queues))
	for _, queue := range queues {
		infos = append(infos, InterfaceTxQueueInfo{
			QueueID: queue.GetQueueId(),
			Shared:  queue.GetShared(),
			Threads: append([]uint32(nil), queue.GetThreads()...),
		})
	}
	return infos
}

func routeInfosFromProto(routes []*apiv1.RouteEntry) []RouteInfo {
	infos := make([]RouteInfo, 0, len(routes))
	for _, route := range routes {
		infos = append(infos, RouteInfo{
			Prefix:    route.GetPrefix(),
			NextHop:   route.GetNextHop(),
			Protocol:  route.GetProtocol(),
			Metric:    route.GetMetric(),
			Interface: route.GetInterface(),
			Active:    route.GetActive(),
		})
	}
	return infos
}

func bgpNeighborInfosFromProto(neighbors []*apiv1.BGPNeighborState) []BGPNeighborInfo {
	infos := make([]BGPNeighborInfo, 0, len(neighbors))
	for _, neighbor := range neighbors {
		infos = append(infos, BGPNeighborInfo{
			PeerAddress:    neighbor.GetPeerAddress(),
			PeerAS:         neighbor.GetPeerAs(),
			State:          neighbor.GetState(),
			UptimeSecs:     neighbor.GetUptimeSecs(),
			PrefixReceived: neighbor.GetPrefixReceived(),
			PrefixSent:     neighbor.GetPrefixSent(),
		})
	}
	return infos
}

func ospfNeighborInfosFromProto(neighbors []*apiv1.OSPFNeighborState) []OSPFNeighborInfo {
	infos := make([]OSPFNeighborInfo, 0, len(neighbors))
	for _, neighbor := range neighbors {
		infos = append(infos, OSPFNeighborInfo{
			RouterID:     neighbor.GetRouterId(),
			Address:      neighbor.GetAddress(),
			Interface:    neighbor.GetInterface(),
			State:        neighbor.GetState(),
			Role:         neighbor.GetRole(),
			Priority:     neighbor.GetPriority(),
			DeadTimeSecs: neighbor.GetDeadTimeSecs(),
			UptimeSecs:   neighbor.GetUptimeSecs(),
		})
	}
	return infos
}

func bfdPeerInfosFromProto(peers []*apiv1.BFDPeerState) []BFDPeerInfo {
	infos := make([]BFDPeerInfo, 0, len(peers))
	for _, peer := range peers {
		infos = append(infos, BFDPeerInfo{
			Peer:              peer.GetPeer(),
			LocalAddress:      peer.GetLocalAddress(),
			Interface:         peer.GetInterface(),
			VRF:               peer.GetVrf(),
			Status:            peer.GetStatus(),
			Diagnostic:        peer.GetDiagnostic(),
			RemoteDiagnostic:  peer.GetRemoteDiagnostic(),
			Observed:          peer.GetObserved(),
			Up:                peer.GetUp(),
			SessionDownEvents: peer.GetSessionDownEvents(),
			RxFailPackets:     peer.GetRxFailPackets(),
		})
	}
	return infos
}

// --- Response types ---

// CommitInfo represents a commit history entry.
type CommitInfo struct {
	CommitID   string
	User       string
	Timestamp  time.Time
	Message    string
	IsRollback bool
}

// InterfaceInfo represents interface operational state.
type InterfaceInfo struct {
	Name        string
	AdminStatus string
	OperStatus  string
	Speed       uint64
	MTU         uint32
	MAC         string
	QoSProfile  string
	IPv4TableID uint32
	IPv6TableID uint32
	RxPackets   uint64
	TxPackets   uint64
	RxBytes     uint64
	TxBytes     uint64
	RxErrors    uint64
	TxErrors    uint64
	RxQueues    []InterfaceRxQueueInfo
	TxQueues    []InterfaceTxQueueInfo
}

// InterfaceRxQueueInfo maps an RX queue to a VPP worker.
type InterfaceRxQueueInfo struct {
	QueueID  uint32
	WorkerID uint32
	Mode     string
}

// InterfaceTxQueueInfo maps a TX queue to VPP worker threads.
type InterfaceTxQueueInfo struct {
	QueueID uint32
	Shared  bool
	Threads []uint32
}

// LCPReconciliationInfo represents VPP LCP cache reconciliation state.
type LCPReconciliationInfo struct {
	LastRun         time.Time
	PairCount       int
	Inconsistencies []string
	LastError       string
}

// HAStatusInfo represents control-plane HA convergence state.
type HAStatusInfo struct {
	Configured              bool
	Converged               bool
	VRRPGroups              int
	Issues                  []string
	ClusterEnabled          bool
	ClusterNodes            int
	ClusterEtcdSync         bool
	ClusterSyncAligned      bool
	FRRVRRPLastCheck        time.Time
	FRRVRRPConfiguredGroups int
	FRRVRRPObservedGroups   int
	FRRVRRPActiveGroups     int
	FRRVRRPIssues           []string
	FRRVRRPLastError        string
	FRRBFDLastCheck         time.Time
	FRRBFDConfiguredPeers   int
	FRRBFDObservedPeers     int
	FRRBFDUpPeers           int
	FRRBFDDownPeers         int
	FRRBFDIssues            []string
	FRRBFDLastError         string
	VPPLCPLastCheck         time.Time
	VPPLCPPairs             int
	VPPLCPInconsistencies   []string
	VPPLCPLastError         string
}

// RoutingInstanceInfo represents running routing-instance intent and table mapping.
type RoutingInstanceInfo struct {
	Name               string
	InstanceType       string
	RouteDistinguisher string
	IPv4TableID        uint32
	IPv6TableID        uint32
	ImportTargets      []string
	ExportTargets      []string
	ImportPolicies     []string
	ExportPolicies     []string
	Interfaces         []string
}

// ClassOfServiceInfo represents running class-of-service intent.
type ClassOfServiceInfo struct {
	ForwardingClasses      []ClassOfServiceForwardingClassInfo
	TrafficControlProfiles []ClassOfServiceTrafficControlProfileInfo
	Interfaces             []ClassOfServiceInterfaceInfo
	EnforcementStatus      string
	Capabilities           *ClassOfServiceCapabilitiesInfo
}

// ClassOfServiceCapabilitiesInfo represents detected VPP QoS dataplane support.
type ClassOfServiceCapabilitiesInfo struct {
	MetadataBindingSupported bool
	QueueSchedulerSupported  bool
	PolicerSupported         bool
	CountersSupported        bool
	LastCheck                time.Time
	LastError                string
	Diagnostics              []string
}

// ClassOfServiceForwardingClassInfo maps a forwarding class to a queue.
type ClassOfServiceForwardingClassInfo struct {
	Name  string
	Queue int
}

// ClassOfServiceTrafficControlProfileInfo describes a traffic-control profile.
type ClassOfServiceTrafficControlProfileInfo struct {
	Name              string
	ShapingRate       uint64
	SchedulerMap      string
	EnforcementStatus string
}

// ClassOfServiceInterfaceInfo describes a class-of-service interface binding.
type ClassOfServiceInterfaceInfo struct {
	Name                        string
	OutputTrafficControlProfile string
	EnforcementStatus           string
}

// RouteInfo represents a routing table entry.
type RouteInfo struct {
	Prefix    string
	NextHop   string
	Protocol  string
	Metric    uint32
	Interface string
	Active    bool
}

// BGPNeighborInfo represents BGP neighbor state.
type BGPNeighborInfo struct {
	PeerAddress    string
	PeerAS         uint32
	State          string
	UptimeSecs     uint64
	PrefixReceived uint32
	PrefixSent     uint32
}

// OSPFNeighborInfo represents OSPFv2 or OSPFv3 neighbor state.
type OSPFNeighborInfo struct {
	RouterID     string
	Address      string
	Interface    string
	State        string
	Role         string
	Priority     uint32
	DeadTimeSecs uint64
	UptimeSecs   uint64
}

func ospfNeighborInfoSortKey(neighbor OSPFNeighborInfo) string {
	return neighbor.RouterID + "\x00" + neighbor.Interface + "\x00" + neighbor.Address
}

// BFDStatusInfo represents FRR BFD operational state.
type BFDStatusInfo struct {
	LastRun           time.Time
	ConfiguredPeers   int
	ObservedPeers     int
	UpPeers           int
	DownPeers         int
	SessionDownEvents uint64
	RxFailPackets     uint64
	Peers             []BFDPeerInfo
	Issues            []string
	LastError         string
}

// BFDPeerInfo represents one FRR BFD peer state.
type BFDPeerInfo struct {
	Peer              string
	LocalAddress      string
	Interface         string
	VRF               string
	Status            string
	Diagnostic        string
	RemoteDiagnostic  string
	Observed          bool
	Up                bool
	SessionDownEvents uint64
	RxFailPackets     uint64
}

// SystemInfo represents system information.
type SystemInfo struct {
	Hostname   string
	Version    string
	UptimeSecs uint64
}
