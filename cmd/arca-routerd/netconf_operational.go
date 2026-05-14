package main

import (
	"context"

	"github.com/akam1o/arca-router/internal/model"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	"github.com/akam1o/arca-router/pkg/netconf"
)

type interfaceStateCollector interface {
	CollectState(ctx context.Context) (map[string]*model.InterfaceState, error)
}

type netconfBFDStatusSource interface {
	BFDOperationalStatus() sbfrr.BFDOperationalStatus
}

type netconfOperationalStateProvider struct {
	collector interfaceStateCollector
	bfdSource netconfBFDStatusSource
}

func newNETCONFOperationalStateProvider(collector interfaceStateCollector, bfdSource netconfBFDStatusSource) netconf.OperationalStateProvider {
	if collector == nil && bfdSource == nil {
		return nil
	}
	return &netconfOperationalStateProvider{collector: collector, bfdSource: bfdSource}
}

func (p *netconfOperationalStateProvider) InterfaceStates(ctx context.Context) (map[string]*netconf.InterfaceOperationalState, error) {
	if p.collector == nil {
		return nil, nil
	}
	states, err := p.collector.CollectState(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*netconf.InterfaceOperationalState, len(states))
	for name, state := range states {
		if state == nil {
			continue
		}
		stateName := state.Name
		if stateName == "" {
			stateName = name
		}
		if stateName == "" {
			continue
		}
		converted := &netconf.InterfaceOperationalState{
			Name:        stateName,
			AdminStatus: state.AdminStatus,
			OperStatus:  state.OperStatus,
			MAC:         state.MAC,
			QoSProfile:  state.QoSProfile,
			IPv4TableID: state.IPv4TableID,
			IPv6TableID: state.IPv6TableID,
		}
		if state.Counters != nil {
			converted.Counters = &netconf.InterfaceOperationalCounters{
				RxPackets: state.Counters.RxPackets,
				TxPackets: state.Counters.TxPackets,
				RxBytes:   state.Counters.RxBytes,
				TxBytes:   state.Counters.TxBytes,
				RxErrors:  state.Counters.RxErrors,
				TxErrors:  state.Counters.TxErrors,
				Drops:     state.Counters.Drops,
			}
		}
		if state.Queues != nil {
			converted.Queues = convertInterfaceOperationalQueues(state.Queues)
		}
		result[stateName] = converted
	}
	return result, nil
}

func (p *netconfOperationalStateProvider) BFDStatus(ctx context.Context) (*netconf.BFDOperationalState, error) {
	_ = ctx
	if p.bfdSource == nil {
		return nil, nil
	}
	status := p.bfdSource.BFDOperationalStatus()
	result := &netconf.BFDOperationalState{
		LastRun:           status.LastRun,
		ConfiguredPeers:   status.ConfiguredPeers,
		ObservedPeers:     status.ObservedPeers,
		UpPeers:           status.UpPeers,
		DownPeers:         status.DownPeers,
		SessionDownEvents: uint64(status.SessionDownEvents),
		RxFailPackets:     uint64(status.RxFailPackets),
		Issues:            append([]string(nil), status.Issues...),
		LastError:         status.LastError,
		Peers:             make([]netconf.BFDPeerOperationalState, 0, len(status.Peers)),
	}
	for _, peer := range status.Peers {
		result.Peers = append(result.Peers, netconf.BFDPeerOperationalState{
			Peer:              peer.Peer,
			LocalAddress:      peer.LocalAddress,
			Interface:         peer.Interface,
			VRF:               peer.VRF,
			Status:            peer.Status,
			Diagnostic:        peer.Diagnostic,
			RemoteDiagnostic:  peer.RemoteDiagnostic,
			Observed:          peer.Observed,
			Up:                peer.Up,
			SessionDownEvents: uint64(peer.SessionDownEvents),
			RxFailPackets:     uint64(peer.RxFailPackets),
		})
	}
	return result, nil
}

func convertInterfaceOperationalQueues(queues *model.InterfaceQueues) *netconf.InterfaceOperationalQueues {
	if queues == nil || (len(queues.Rx) == 0 && len(queues.Tx) == 0) {
		return nil
	}
	result := &netconf.InterfaceOperationalQueues{
		Rx: make([]netconf.InterfaceOperationalRxQueue, 0, len(queues.Rx)),
		Tx: make([]netconf.InterfaceOperationalTxQueue, 0, len(queues.Tx)),
	}
	for _, queue := range queues.Rx {
		result.Rx = append(result.Rx, netconf.InterfaceOperationalRxQueue{
			QueueID:  queue.QueueID,
			WorkerID: queue.WorkerID,
			Mode:     queue.Mode,
		})
	}
	for _, queue := range queues.Tx {
		result.Tx = append(result.Tx, netconf.InterfaceOperationalTxQueue{
			QueueID: queue.QueueID,
			Shared:  queue.Shared,
			Threads: append([]uint32(nil), queue.Threads...),
		})
	}
	return result
}
