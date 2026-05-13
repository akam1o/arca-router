package main

import (
	"context"

	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/netconf"
)

type interfaceStateCollector interface {
	CollectState(ctx context.Context) (map[string]*model.InterfaceState, error)
}

type netconfOperationalStateProvider struct {
	collector interfaceStateCollector
}

func newNETCONFOperationalStateProvider(collector interfaceStateCollector) netconf.OperationalStateProvider {
	if collector == nil {
		return nil
	}
	return &netconfOperationalStateProvider{collector: collector}
}

func (p *netconfOperationalStateProvider) InterfaceStates(ctx context.Context) (map[string]*netconf.InterfaceOperationalState, error) {
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
