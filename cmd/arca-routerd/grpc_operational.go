package main

import (
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
