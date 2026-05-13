package main

import (
	"time"

	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
)

type lcpReconciliationRuntimeSource interface {
	LCPReconciliationStatus() sbvpp.LCPReconciliationStatus
}

type grpcLCPReconciliationSource struct {
	source lcpReconciliationRuntimeSource
}

func newGRPCLCPReconciliationSource(source lcpReconciliationRuntimeSource) *grpcLCPReconciliationSource {
	if source == nil {
		return nil
	}
	return &grpcLCPReconciliationSource{source: source}
}

func (s *grpcLCPReconciliationSource) LCPReconciliationInfo() nbgrpc.LCPReconciliationInfo {
	status := s.source.LCPReconciliationStatus()
	return nbgrpc.LCPReconciliationInfo{
		LastRun:         status.LastRun,
		PairCount:       status.PairCount,
		Inconsistencies: append([]string(nil), status.Inconsistencies...),
		LastError:       status.LastError,
	}
}

type grpcHAStatusSource struct {
	source metricsSource
}

func newGRPCHAStatusSource(source metricsSource) *grpcHAStatusSource {
	return &grpcHAStatusSource{source: source}
}

func (s *grpcHAStatusSource) HAStatusInfo() nbgrpc.HAStatusInfo {
	metrics := s.source.snapshot(time.Now())
	return nbgrpc.HAStatusInfo{
		Configured:              metrics.HAConfigured,
		Converged:               metrics.HAConverged,
		VRRPGroups:              metrics.HAVRPGroups,
		Issues:                  append([]string(nil), metrics.HAIssues...),
		ClusterEnabled:          metrics.ClusterEnabled,
		ClusterNodes:            metrics.ClusterNodeCount,
		ClusterEtcdSync:         metrics.ClusterEtcdSync,
		ClusterSyncAligned:      metrics.ClusterSyncAligned,
		FRRVRRPLastCheck:        metrics.FRRVRRPLastRun,
		FRRVRRPConfiguredGroups: metrics.FRRVRRPConfiguredGroups,
		FRRVRRPObservedGroups:   metrics.FRRVRRPObservedGroups,
		FRRVRRPActiveGroups:     metrics.FRRVRRPActiveGroups,
		FRRVRRPIssues:           append([]string(nil), metrics.FRRVRRPIssues...),
		FRRVRRPLastError:        metrics.FRRVRRPError,
		VPPLCPLastCheck:         metrics.VPPLCPReconcileLastRun,
		VPPLCPPairs:             metrics.VPPLCPPairs,
		VPPLCPInconsistencies:   append([]string(nil), metrics.VPPLCPInconsistencies...),
		VPPLCPLastError:         metrics.VPPLCPReconcileError,
	}
}
