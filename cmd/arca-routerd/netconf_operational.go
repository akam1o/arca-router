package main

import (
	"context"

	"github.com/akam1o/arca-router/internal/model"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	pkgfrr "github.com/akam1o/arca-router/pkg/frr"
	"github.com/akam1o/arca-router/pkg/netconf"
)

type interfaceStateCollector interface {
	CollectState(ctx context.Context) (map[string]*model.InterfaceState, error)
}

type netconfBFDStatusSource interface {
	BFDOperationalStatus() sbfrr.BFDOperationalStatus
}

type netconfOperationalStateProvider struct {
	collector   interfaceStateCollector
	bfdSource   netconfBFDStatusSource
	routeReader pkgfrr.RouteStatusReader
	bgpReader   pkgfrr.BGPSummaryStatusReader
	ospfReader  pkgfrr.OSPFNeighborStatusReader
}

func newNETCONFOperationalStateProvider(collector interfaceStateCollector, bfdSource netconfBFDStatusSource) netconf.OperationalStateProvider {
	if collector == nil && bfdSource == nil {
		return nil
	}
	provider := &netconfOperationalStateProvider{collector: collector, bfdSource: bfdSource}
	if bfdSource != nil {
		provider.routeReader = pkgfrr.NewVtyshRouteStatusReader()
		provider.bgpReader = pkgfrr.NewVtyshBGPSummaryStatusReader()
		provider.ospfReader = pkgfrr.NewVtyshOSPFNeighborStatusReader()
	}
	return provider
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

func (p *netconfOperationalStateProvider) Routes(ctx context.Context) ([]netconf.RouteOperationalState, error) {
	if p.routeReader == nil {
		return nil, nil
	}
	status, err := p.routeReader.ReadRouteStatus(ctx)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, nil
	}
	result := make([]netconf.RouteOperationalState, 0, len(status.Routes))
	for _, route := range status.Routes {
		result = append(result, netconf.RouteOperationalState{
			Prefix:    route.Prefix,
			NextHop:   route.NextHop,
			Protocol:  route.Protocol,
			Metric:    route.Metric,
			Interface: route.Interface,
			Active:    route.Active,
		})
	}
	return result, nil
}

func (p *netconfOperationalStateProvider) BGPNeighbors(ctx context.Context) ([]netconf.BGPNeighborOperationalState, error) {
	if p.bgpReader == nil {
		return nil, nil
	}
	status, err := p.bgpReader.ReadBGPSummaryStatus(ctx)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, nil
	}
	result := make([]netconf.BGPNeighborOperationalState, 0, len(status.Neighbors))
	for _, neighbor := range status.Neighbors {
		result = append(result, netconf.BGPNeighborOperationalState{
			PeerAddress:    neighbor.PeerAddress,
			PeerAS:         neighbor.PeerAS,
			State:          neighbor.State,
			UptimeSecs:     neighbor.UptimeSecs,
			PrefixReceived: neighbor.PrefixReceived,
			PrefixSent:     neighbor.PrefixSent,
		})
	}
	return result, nil
}

func (p *netconfOperationalStateProvider) OSPFNeighbors(ctx context.Context, ipv6 bool) ([]netconf.OSPFNeighborOperationalState, error) {
	if p.ospfReader == nil {
		return nil, nil
	}
	status, err := p.ospfReader.ReadOSPFNeighborStatus(ctx, ipv6)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, nil
	}
	result := make([]netconf.OSPFNeighborOperationalState, 0, len(status.Neighbors))
	for _, neighbor := range status.Neighbors {
		result = append(result, netconf.OSPFNeighborOperationalState{
			RouterID:     neighbor.RouterID,
			Address:      neighbor.Address,
			Interface:    neighbor.Interface,
			State:        neighbor.State,
			Role:         neighbor.Role,
			Priority:     neighbor.Priority,
			DeadTimeSecs: neighbor.DeadTimeSecs,
			UptimeSecs:   neighbor.UptimeSecs,
		})
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
