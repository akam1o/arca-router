package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"time"

	apiv1 "github.com/akam1o/arca-router/api/v1"
	"github.com/akam1o/arca-router/internal/engine"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type configServiceAdapter struct {
	apiv1.UnimplementedConfigServiceServer
	server *Server
}

func (a *configServiceAdapter) GetRunning(ctx context.Context, _ *apiv1.GetRunningRequest) (*apiv1.GetRunningResponse, error) {
	configText, version, err := a.server.GetRunning(ctx)
	if err != nil {
		return nil, configEditStatusError(err)
	}
	return &apiv1.GetRunningResponse{
		ConfigText: configText,
		Version:    version,
	}, nil
}

func (a *configServiceAdapter) GetCandidate(ctx context.Context, req *apiv1.GetCandidateRequest) (*apiv1.GetCandidateResponse, error) {
	configText, err := a.server.GetCandidate(ctx, req.GetSessionId())
	if err != nil {
		return nil, configEditStatusError(err)
	}
	return &apiv1.GetCandidateResponse{ConfigText: configText}, nil
}

func (a *configServiceAdapter) EditCandidate(ctx context.Context, req *apiv1.EditCandidateRequest) (*apiv1.EditCandidateResponse, error) {
	if err := a.server.EditCandidate(ctx, req.GetSessionId(), req.GetConfigText()); err != nil {
		return nil, configEditStatusError(err)
	}
	return &apiv1.EditCandidateResponse{}, nil
}

func (a *configServiceAdapter) ReplaceCandidate(ctx context.Context, req *apiv1.ReplaceCandidateRequest) (*apiv1.ReplaceCandidateResponse, error) {
	if err := a.server.ReplaceCandidate(ctx, req.GetSessionId(), req.GetConfigText()); err != nil {
		return nil, configEditStatusError(err)
	}
	return &apiv1.ReplaceCandidateResponse{}, nil
}

func (a *configServiceAdapter) Commit(ctx context.Context, req *apiv1.CommitRequest) (*apiv1.CommitResponse, error) {
	commitID, version, err := a.server.Commit(ctx, req.GetSessionId(), grpcRequestUser(ctx, req.GetUser()), req.GetMessage())
	if err != nil {
		return nil, configEditStatusError(err)
	}
	return &apiv1.CommitResponse{CommitId: commitID, Version: version}, nil
}

func (a *configServiceAdapter) ValidateCandidate(ctx context.Context, req *apiv1.ValidateCandidateRequest) (*apiv1.ValidateCandidateResponse, error) {
	if err := a.server.ValidateCandidate(ctx, req.GetSessionId()); err != nil {
		return nil, configEditStatusError(err)
	}
	return &apiv1.ValidateCandidateResponse{}, nil
}

func (a *configServiceAdapter) Discard(ctx context.Context, req *apiv1.DiscardRequest) (*apiv1.DiscardResponse, error) {
	if err := a.server.Discard(ctx, req.GetSessionId()); err != nil {
		return nil, configEditStatusError(err)
	}
	return &apiv1.DiscardResponse{}, nil
}

func (a *configServiceAdapter) Rollback(ctx context.Context, req *apiv1.RollbackRequest) (*apiv1.RollbackResponse, error) {
	commitID, version, err := a.server.Rollback(ctx, req.GetSessionId(), req.GetCommitId(), grpcRequestUser(ctx, req.GetUser()), req.GetMessage())
	if err != nil {
		return nil, configEditStatusError(err)
	}
	return &apiv1.RollbackResponse{NewCommitId: commitID, Version: version}, nil
}

func (a *configServiceAdapter) Diff(ctx context.Context, req *apiv1.DiffRequest) (*apiv1.DiffResponse, error) {
	diffText, hasChanges, err := a.server.Diff(ctx, req.GetSessionId())
	if err != nil {
		return nil, configEditStatusError(err)
	}
	return &apiv1.DiffResponse{DiffText: diffText, HasChanges: hasChanges}, nil
}

func configEditStatusError(err error) error {
	switch {
	case errors.Is(err, ErrConfigInput), errors.Is(err, engine.ErrConfigValidation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, ErrSessionNotFound):
		return status.Error(codes.NotFound, "configuration session not found")
	case errors.Is(err, ErrCandidateConflict):
		return status.Error(codes.FailedPrecondition, "configuration candidate is unavailable")
	case errors.Is(err, ErrCommitHistoryUnavailable):
		return status.Error(codes.Unavailable, "commit history is unavailable")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "configuration operation timed out")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "configuration operation canceled")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}

func (a *configServiceAdapter) ListHistory(ctx context.Context, req *apiv1.ListHistoryRequest) (*apiv1.ListHistoryResponse, error) {
	entries, err := a.server.ListHistory(ctx, int(req.GetLimit()), int(req.GetOffset()))
	if err != nil {
		return nil, configEditStatusError(err)
	}
	resp := &apiv1.ListHistoryResponse{Entries: make([]*apiv1.CommitEntry, 0, len(entries))}
	for _, entry := range entries {
		resp.Entries = append(resp.Entries, &apiv1.CommitEntry{
			CommitId:   entry.CommitID,
			User:       entry.User,
			Timestamp:  entry.Timestamp.Format(time.RFC3339Nano),
			Message:    entry.Message,
			IsRollback: entry.IsRollback,
			ConfigText: entry.ConfigText,
		})
	}
	return resp, nil
}

type sessionServiceAdapter struct {
	apiv1.UnimplementedSessionServiceServer
	server *Server
}

func grpcRequestUser(ctx context.Context, requested string) string {
	if p, ok := peer.FromContext(ctx); ok && p.AuthInfo != nil {
		if tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo); ok {
			if user := grpcTLSUser(tlsInfo.State); user != "" {
				return user
			}
		}
	}
	return strings.TrimSpace(requested)
}

func grpcTLSUser(state tls.ConnectionState) string {
	identities := grpcTLSIdentities(state)
	if len(identities) == 0 {
		return ""
	}
	return identities[0]
}

func grpcTLSIdentities(state tls.ConnectionState) []string {
	if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return nil
	}
	cert := state.VerifiedChains[0][0]
	identities := make([]string, 0, len(cert.URIs)+len(cert.DNSNames)+len(cert.EmailAddresses)+1)
	if len(cert.URIs) > 0 {
		for _, uri := range cert.URIs {
			if uri != nil && uri.String() != "" {
				identities = append(identities, uri.String())
			}
		}
	}
	if cert.Subject.CommonName != "" {
		identities = append(identities, cert.Subject.CommonName)
	}
	identities = append(identities, cert.DNSNames...)
	identities = append(identities, cert.EmailAddresses...)
	return identities
}

func (a *sessionServiceAdapter) CreateSession(ctx context.Context, req *apiv1.CreateSessionRequest) (*apiv1.CreateSessionResponse, error) {
	sessionID, err := a.server.CreateSession(ctx, grpcRequestUser(ctx, req.GetUser()))
	if err != nil {
		return nil, sessionStatusError(err)
	}
	return &apiv1.CreateSessionResponse{SessionId: sessionID}, nil
}

func (a *sessionServiceAdapter) CloseSession(ctx context.Context, req *apiv1.CloseSessionRequest) (*apiv1.CloseSessionResponse, error) {
	if err := a.server.CloseSession(ctx, req.GetSessionId()); err != nil {
		return nil, sessionStatusError(err)
	}
	return &apiv1.CloseSessionResponse{}, nil
}

func (a *sessionServiceAdapter) AcquireLock(ctx context.Context, req *apiv1.AcquireLockRequest) (*apiv1.AcquireLockResponse, error) {
	if err := a.server.AcquireLock(ctx, req.GetSessionId(), grpcRequestUser(ctx, req.GetUser())); err != nil {
		return nil, sessionStatusError(err)
	}
	return &apiv1.AcquireLockResponse{}, nil
}

func (a *sessionServiceAdapter) ReleaseLock(ctx context.Context, req *apiv1.ReleaseLockRequest) (*apiv1.ReleaseLockResponse, error) {
	if err := a.server.ReleaseLock(ctx, req.GetSessionId()); err != nil {
		return nil, sessionStatusError(err)
	}
	return &apiv1.ReleaseLockResponse{}, nil
}

func sessionStatusError(err error) error {
	switch {
	case errors.Is(err, ErrSessionNotFound):
		return status.Error(codes.NotFound, "configuration session not found")
	case errors.Is(err, ErrCandidateConflict):
		return status.Error(codes.FailedPrecondition, "configuration candidate is unavailable")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "session operation timed out")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "session operation canceled")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}

type stateServiceAdapter struct {
	apiv1.UnimplementedStateServiceServer
	server *Server
}

func (a *stateServiceAdapter) GetInterfaces(ctx context.Context, req *apiv1.GetInterfacesRequest) (*apiv1.GetInterfacesResponse, error) {
	interfaces, err := a.server.GetInterfaces(ctx, req.GetNameFilter())
	if err != nil {
		return nil, stateStatusError(err)
	}
	resp := &apiv1.GetInterfacesResponse{Interfaces: make([]*apiv1.InterfaceState, 0, len(interfaces))}
	for _, iface := range interfaces {
		resp.Interfaces = append(resp.Interfaces, &apiv1.InterfaceState{
			Name:        iface.Name,
			AdminStatus: iface.AdminStatus,
			OperStatus:  iface.OperStatus,
			Speed:       iface.Speed,
			Mtu:         iface.MTU,
			Mac:         iface.MAC,
			QosProfile:  iface.QoSProfile,
			Ipv4TableId: iface.IPv4TableID,
			Ipv6TableId: iface.IPv6TableID,
			RxPackets:   iface.RxPackets,
			TxPackets:   iface.TxPackets,
			RxBytes:     iface.RxBytes,
			TxBytes:     iface.TxBytes,
			RxErrors:    iface.RxErrors,
			TxErrors:    iface.TxErrors,
			RxQueues:    rxQueuesToProto(iface.RxQueues),
			TxQueues:    txQueuesToProto(iface.TxQueues),
		})
	}
	return resp, nil
}

func rxQueuesToProto(queues []InterfaceRxQueueInfo) []*apiv1.InterfaceRxQueue {
	out := make([]*apiv1.InterfaceRxQueue, 0, len(queues))
	for _, queue := range queues {
		out = append(out, &apiv1.InterfaceRxQueue{
			QueueId:  queue.QueueID,
			WorkerId: queue.WorkerID,
			Mode:     queue.Mode,
		})
	}
	return out
}

func txQueuesToProto(queues []InterfaceTxQueueInfo) []*apiv1.InterfaceTxQueue {
	out := make([]*apiv1.InterfaceTxQueue, 0, len(queues))
	for _, queue := range queues {
		out = append(out, &apiv1.InterfaceTxQueue{
			QueueId: queue.QueueID,
			Shared:  queue.Shared,
			Threads: append([]uint32(nil), queue.Threads...),
		})
	}
	return out
}

func (a *stateServiceAdapter) GetRoutes(ctx context.Context, req *apiv1.GetRoutesRequest) (*apiv1.GetRoutesResponse, error) {
	routes, err := a.server.GetRoutes(ctx, req.GetPrefixFilter(), req.GetProtocolFilter())
	if err != nil {
		return nil, stateStatusError(err)
	}
	resp := &apiv1.GetRoutesResponse{Routes: make([]*apiv1.RouteEntry, 0, len(routes))}
	for _, route := range routes {
		resp.Routes = append(resp.Routes, &apiv1.RouteEntry{
			Prefix:    route.Prefix,
			NextHop:   route.NextHop,
			Protocol:  route.Protocol,
			Metric:    route.Metric,
			Interface: route.Interface,
			Active:    route.Active,
		})
	}
	return resp, nil
}

func (a *stateServiceAdapter) GetBGPNeighbors(ctx context.Context, _ *apiv1.GetBGPNeighborsRequest) (*apiv1.GetBGPNeighborsResponse, error) {
	neighbors, err := a.server.GetBGPNeighbors(ctx)
	if err != nil {
		return nil, stateStatusError(err)
	}
	resp := &apiv1.GetBGPNeighborsResponse{Neighbors: make([]*apiv1.BGPNeighborState, 0, len(neighbors))}
	for _, neighbor := range neighbors {
		resp.Neighbors = append(resp.Neighbors, &apiv1.BGPNeighborState{
			PeerAddress:    neighbor.PeerAddress,
			PeerAs:         neighbor.PeerAS,
			State:          neighbor.State,
			UptimeSecs:     neighbor.UptimeSecs,
			PrefixReceived: neighbor.PrefixReceived,
			PrefixSent:     neighbor.PrefixSent,
		})
	}
	return resp, nil
}

func (a *stateServiceAdapter) GetOSPFNeighbors(ctx context.Context, req *apiv1.GetOSPFNeighborsRequest) (*apiv1.GetOSPFNeighborsResponse, error) {
	neighbors, err := a.server.GetOSPFNeighbors(ctx, req.GetAddressFamily())
	if err != nil {
		return nil, stateStatusError(err)
	}
	resp := &apiv1.GetOSPFNeighborsResponse{Neighbors: make([]*apiv1.OSPFNeighborState, 0, len(neighbors))}
	for _, neighbor := range neighbors {
		resp.Neighbors = append(resp.Neighbors, &apiv1.OSPFNeighborState{
			RouterId:     neighbor.RouterID,
			Address:      neighbor.Address,
			Interface:    neighbor.Interface,
			State:        neighbor.State,
			Role:         neighbor.Role,
			Priority:     neighbor.Priority,
			DeadTimeSecs: neighbor.DeadTimeSecs,
			UptimeSecs:   neighbor.UptimeSecs,
		})
	}
	return resp, nil
}

func (a *stateServiceAdapter) GetRouteText(ctx context.Context, req *apiv1.GetRouteTextRequest) (*apiv1.GetRouteTextResponse, error) {
	output, err := a.server.GetRouteText(ctx, req.GetProtocolFilter(), req.GetAddressFamily())
	if err != nil {
		return nil, stateStatusError(err)
	}
	return &apiv1.GetRouteTextResponse{Output: output}, nil
}

func (a *stateServiceAdapter) GetBGPSummaryText(ctx context.Context, _ *apiv1.GetBGPSummaryTextRequest) (*apiv1.GetBGPSummaryTextResponse, error) {
	output, err := a.server.GetBGPSummaryText(ctx)
	if err != nil {
		return nil, stateStatusError(err)
	}
	return &apiv1.GetBGPSummaryTextResponse{Output: output}, nil
}

func (a *stateServiceAdapter) GetBGPNeighborText(ctx context.Context, req *apiv1.GetBGPNeighborTextRequest) (*apiv1.GetBGPNeighborTextResponse, error) {
	output, err := a.server.GetBGPNeighborText(ctx, req.GetPeerAddress())
	if err != nil {
		return nil, stateStatusError(err)
	}
	return &apiv1.GetBGPNeighborTextResponse{Output: output}, nil
}

func (a *stateServiceAdapter) GetOSPFNeighborsText(ctx context.Context, req *apiv1.GetOSPFNeighborsTextRequest) (*apiv1.GetOSPFNeighborsTextResponse, error) {
	output, err := a.server.GetOSPFNeighborsText(ctx, req.GetAddressFamily())
	if err != nil {
		return nil, stateStatusError(err)
	}
	return &apiv1.GetOSPFNeighborsTextResponse{Output: output}, nil
}

func (a *stateServiceAdapter) GetVRRPText(ctx context.Context, _ *apiv1.GetVRRPTextRequest) (*apiv1.GetVRRPTextResponse, error) {
	output, err := a.server.GetVRRPText(ctx)
	if err != nil {
		return nil, stateStatusError(err)
	}
	return &apiv1.GetVRRPTextResponse{Output: output}, nil
}

func (a *stateServiceAdapter) GetBFDText(ctx context.Context, req *apiv1.GetBFDTextRequest) (*apiv1.GetBFDTextResponse, error) {
	output, err := a.server.GetBFDText(ctx, req.GetPeerAddress(), req.GetBrief(), req.GetCounters())
	if err != nil {
		return nil, stateStatusError(err)
	}
	return &apiv1.GetBFDTextResponse{Output: output}, nil
}

func (a *stateServiceAdapter) GetBFDStatus(ctx context.Context, _ *apiv1.GetBFDStatusRequest) (*apiv1.GetBFDStatusResponse, error) {
	info, err := a.server.GetBFDStatus(ctx)
	if err != nil {
		return nil, stateStatusError(err)
	}
	resp := &apiv1.GetBFDStatusResponse{
		ConfiguredPeers:   uint32(info.ConfiguredPeers),
		ObservedPeers:     uint32(info.ObservedPeers),
		UpPeers:           uint32(info.UpPeers),
		DownPeers:         uint32(info.DownPeers),
		SessionDownEvents: info.SessionDownEvents,
		RxFailPackets:     info.RxFailPackets,
		Issues:            append([]string(nil), info.Issues...),
		LastError:         info.LastError,
	}
	if !info.LastRun.IsZero() {
		resp.LastRun = info.LastRun.UTC().Format(time.RFC3339Nano)
	}
	for _, peer := range info.Peers {
		resp.Peers = append(resp.Peers, &apiv1.BFDPeerState{
			Peer:              peer.Peer,
			LocalAddress:      peer.LocalAddress,
			Interface:         peer.Interface,
			Vrf:               peer.VRF,
			Status:            peer.Status,
			Diagnostic:        peer.Diagnostic,
			RemoteDiagnostic:  peer.RemoteDiagnostic,
			Observed:          peer.Observed,
			Up:                peer.Up,
			SessionDownEvents: peer.SessionDownEvents,
			RxFailPackets:     peer.RxFailPackets,
		})
	}
	return resp, nil
}

func (a *stateServiceAdapter) GetLCPReconciliation(ctx context.Context, _ *apiv1.GetLCPReconciliationRequest) (*apiv1.GetLCPReconciliationResponse, error) {
	info, err := a.server.GetLCPReconciliation(ctx)
	if err != nil {
		return nil, stateStatusError(err)
	}
	resp := &apiv1.GetLCPReconciliationResponse{
		PairCount:       uint32(info.PairCount),
		Inconsistencies: append([]string(nil), info.Inconsistencies...),
		LastError:       info.LastError,
	}
	if !info.LastRun.IsZero() {
		resp.LastRun = info.LastRun.UTC().Format(time.RFC3339Nano)
	}
	return resp, nil
}

func (a *stateServiceAdapter) GetHAStatus(ctx context.Context, _ *apiv1.GetHAStatusRequest) (*apiv1.GetHAStatusResponse, error) {
	info, err := a.server.GetHAStatus(ctx)
	if err != nil {
		return nil, stateStatusError(err)
	}
	resp := &apiv1.GetHAStatusResponse{
		Configured:              info.Configured,
		Converged:               info.Converged,
		VrrpGroups:              uint32(info.VRRPGroups),
		Issues:                  append([]string(nil), info.Issues...),
		ClusterEnabled:          info.ClusterEnabled,
		ClusterNodes:            uint32(info.ClusterNodes),
		ClusterEtcdSync:         info.ClusterEtcdSync,
		ClusterSyncAligned:      info.ClusterSyncAligned,
		FrrVrrpConfiguredGroups: uint32(info.FRRVRRPConfiguredGroups),
		FrrVrrpObservedGroups:   uint32(info.FRRVRRPObservedGroups),
		FrrVrrpActiveGroups:     uint32(info.FRRVRRPActiveGroups),
		FrrVrrpIssues:           append([]string(nil), info.FRRVRRPIssues...),
		FrrVrrpLastError:        info.FRRVRRPLastError,
		FrrBfdConfiguredPeers:   uint32(info.FRRBFDConfiguredPeers),
		FrrBfdObservedPeers:     uint32(info.FRRBFDObservedPeers),
		FrrBfdUpPeers:           uint32(info.FRRBFDUpPeers),
		FrrBfdDownPeers:         uint32(info.FRRBFDDownPeers),
		FrrBfdIssues:            append([]string(nil), info.FRRBFDIssues...),
		FrrBfdLastError:         info.FRRBFDLastError,
		VppLcpPairs:             uint32(info.VPPLCPPairs),
		VppLcpInconsistencies:   append([]string(nil), info.VPPLCPInconsistencies...),
		VppLcpLastError:         info.VPPLCPLastError,
	}
	if !info.FRRVRRPLastCheck.IsZero() {
		resp.FrrVrrpLastCheck = info.FRRVRRPLastCheck.UTC().Format(time.RFC3339Nano)
	}
	if !info.FRRBFDLastCheck.IsZero() {
		resp.FrrBfdLastCheck = info.FRRBFDLastCheck.UTC().Format(time.RFC3339Nano)
	}
	if !info.VPPLCPLastCheck.IsZero() {
		resp.VppLcpLastCheck = info.VPPLCPLastCheck.UTC().Format(time.RFC3339Nano)
	}
	return resp, nil
}

func (a *stateServiceAdapter) GetRoutingInstances(ctx context.Context, _ *apiv1.GetRoutingInstancesRequest) (*apiv1.GetRoutingInstancesResponse, error) {
	instances, err := a.server.GetRoutingInstances(ctx)
	if err != nil {
		return nil, stateStatusError(err)
	}
	resp := &apiv1.GetRoutingInstancesResponse{Instances: make([]*apiv1.RoutingInstanceState, 0, len(instances))}
	for _, instance := range instances {
		resp.Instances = append(resp.Instances, &apiv1.RoutingInstanceState{
			Name:               instance.Name,
			InstanceType:       instance.InstanceType,
			RouteDistinguisher: instance.RouteDistinguisher,
			Ipv4TableId:        instance.IPv4TableID,
			Ipv6TableId:        instance.IPv6TableID,
			ImportTargets:      append([]string(nil), instance.ImportTargets...),
			ExportTargets:      append([]string(nil), instance.ExportTargets...),
			ImportPolicies:     append([]string(nil), instance.ImportPolicies...),
			ExportPolicies:     append([]string(nil), instance.ExportPolicies...),
			Interfaces:         append([]string(nil), instance.Interfaces...),
		})
	}
	return resp, nil
}

func (a *stateServiceAdapter) GetClassOfService(ctx context.Context, _ *apiv1.GetClassOfServiceRequest) (*apiv1.GetClassOfServiceResponse, error) {
	info, err := a.server.GetClassOfService(ctx)
	if err != nil {
		return nil, stateStatusError(err)
	}
	resp := &apiv1.GetClassOfServiceResponse{
		EnforcementStatus: info.EnforcementStatus,
	}
	if capabilities := info.Capabilities; capabilities != nil {
		resp.Capabilities = &apiv1.ClassOfServiceCapabilities{
			MetadataBindingSupported: capabilities.MetadataBindingSupported,
			QueueSchedulerSupported:  capabilities.QueueSchedulerSupported,
			PolicerSupported:         capabilities.PolicerSupported,
			CountersSupported:        capabilities.CountersSupported,
			LastError:                capabilities.LastError,
			Diagnostics:              append([]string(nil), capabilities.Diagnostics...),
		}
		if !capabilities.LastCheck.IsZero() {
			resp.Capabilities.LastCheck = capabilities.LastCheck.UTC().Format(time.RFC3339Nano)
		}
	}
	for _, fc := range info.ForwardingClasses {
		queue := uint32(0)
		if fc.Queue > 0 {
			queue = uint32(fc.Queue)
		}
		resp.ForwardingClasses = append(resp.ForwardingClasses, &apiv1.ClassOfServiceForwardingClass{
			Name:  fc.Name,
			Queue: queue,
		})
	}
	for _, profile := range info.TrafficControlProfiles {
		resp.TrafficControlProfiles = append(resp.TrafficControlProfiles, &apiv1.ClassOfServiceTrafficControlProfile{
			Name:              profile.Name,
			ShapingRate:       profile.ShapingRate,
			SchedulerMap:      profile.SchedulerMap,
			EnforcementStatus: profile.EnforcementStatus,
		})
	}
	for _, iface := range info.Interfaces {
		resp.Interfaces = append(resp.Interfaces, &apiv1.ClassOfServiceInterface{
			Name:                        iface.Name,
			OutputTrafficControlProfile: iface.OutputTrafficControlProfile,
			EnforcementStatus:           iface.EnforcementStatus,
		})
	}
	return resp, nil
}

func (a *stateServiceAdapter) GetSystemInfo(ctx context.Context, _ *apiv1.GetSystemInfoRequest) (*apiv1.GetSystemInfoResponse, error) {
	info, err := a.server.GetSystemInfo(ctx)
	if err != nil {
		return nil, stateStatusError(err)
	}
	return &apiv1.GetSystemInfoResponse{
		Hostname:   info.Hostname,
		Version:    info.Version,
		UptimeSecs: info.UptimeSecs,
	}, nil
}

func stateStatusError(err error) error {
	switch {
	case isStateInputError(err):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "state operation timed out")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "state operation canceled")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}

func isStateInputError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "invalid route prefix filter") ||
		strings.HasPrefix(msg, "invalid route protocol") ||
		strings.HasPrefix(msg, "invalid address family") ||
		strings.HasPrefix(msg, "invalid BGP neighbor address") ||
		strings.HasPrefix(msg, "invalid BFD peer address") ||
		strings.Contains(msg, "does not support brief")
}

type telemetryServiceAdapter struct {
	apiv1.UnimplementedTelemetryServiceServer
	server *Server
}

func (a *telemetryServiceAdapter) GetTelemetryCatalog(_ context.Context, req *apiv1.GetTelemetryCatalogRequest) (*apiv1.GetTelemetryCatalogResponse, error) {
	var filter TelemetryCatalogFilter
	if req != nil {
		filter = TelemetryCatalogFilter{
			Paths:          append([]string(nil), req.GetPath()...),
			Cardinalities:  append([]string(nil), req.GetCardinality()...),
			PayloadSchemas: append([]string(nil), req.GetPayloadSchema()...),
			Encodings:      append([]string(nil), req.GetEncoding()...),
			DefaultOnly:    req.GetDefaultOnly(),
		}
	}
	return telemetryCatalogToProto(NewFilteredTelemetryCatalog(filter)), nil
}

func (a *telemetryServiceAdapter) SubscribeTelemetry(req *apiv1.SubscribeTelemetryRequest, stream apiv1.TelemetryService_SubscribeTelemetryServer) error {
	interval := time.Duration(req.GetSampleIntervalMs()) * time.Millisecond
	return a.server.SubscribeTelemetry(stream.Context(), req.GetPaths(), interval, req.GetOnce(), func(event TelemetryEvent) error {
		return stream.Send(telemetryEventToProto(event))
	})
}

func telemetryCatalogToProto(catalog TelemetryCatalog) *apiv1.GetTelemetryCatalogResponse {
	resp := &apiv1.GetTelemetryCatalogResponse{
		EventSchemaVersion:      catalog.EventSchemaVersion,
		Encoding:                catalog.Encoding,
		DefaultPaths:            append([]string(nil), catalog.DefaultPaths...),
		DefaultSampleIntervalMs: catalog.DefaultSampleIntervalMs,
		MinSampleIntervalMs:     catalog.MinSampleIntervalMs,
		MaxSampleIntervalMs:     catalog.MaxSampleIntervalMs,
		Paths:                   make([]*apiv1.TelemetryPath, 0, len(catalog.Paths)),
	}
	for _, info := range catalog.Paths {
		resp.Paths = append(resp.Paths, &apiv1.TelemetryPath{
			Path:          info.Path,
			Description:   info.Description,
			Cardinality:   info.Cardinality,
			PayloadSchema: info.PayloadSchema,
			Aliases:       append([]string(nil), info.Aliases...),
			Default:       info.Default,
		})
	}
	return resp
}

func telemetryEventToProto(event TelemetryEvent) *apiv1.TelemetryEvent {
	resp := &apiv1.TelemetryEvent{
		Sequence:      event.Sequence,
		Path:          event.Path,
		Cardinality:   event.Cardinality,
		PayloadSchema: event.PayloadSchema,
		EventType:     event.EventType,
		Encoding:      event.Encoding,
		JsonPayload:   event.JSONPayload,
		SchemaVersion: event.SchemaVersion,
		PayloadBytes:  uint64(event.PayloadBytes),
	}
	if !event.Timestamp.IsZero() {
		resp.Timestamp = event.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return resp
}
