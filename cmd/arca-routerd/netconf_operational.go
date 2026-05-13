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
		result[stateName] = converted
	}
	return result, nil
}
