package main

import (
	"context"
	"testing"

	"github.com/akam1o/arca-router/internal/model"
)

type fakeInterfaceStateCollector struct {
	states map[string]*model.InterfaceState
	err    error
}

func (c fakeInterfaceStateCollector) CollectState(ctx context.Context) (map[string]*model.InterfaceState, error) {
	return c.states, c.err
}

func TestNewNETCONFOperationalStateProviderNilCollector(t *testing.T) {
	if provider := newNETCONFOperationalStateProvider(nil); provider != nil {
		t.Fatalf("newNETCONFOperationalStateProvider(nil) = %#v, want nil", provider)
	}
}

func TestNETCONFOperationalStateProviderConvertsInterfaceState(t *testing.T) {
	provider := newNETCONFOperationalStateProvider(fakeInterfaceStateCollector{
		states: map[string]*model.InterfaceState{
			"ge-0/0/0": {
				Name:        "ge-0/0/0",
				AdminStatus: "up",
				OperStatus:  "down",
				MAC:         "02:00:00:00:00:01",
				QoSProfile:  "WAN",
				Counters: &model.InterfaceCounters{
					RxPackets: 10,
					TxPackets: 20,
					RxBytes:   1000,
					TxBytes:   2000,
					RxErrors:  1,
					TxErrors:  2,
					Drops:     3,
				},
				Queues: &model.InterfaceQueues{
					Rx: []model.InterfaceRxQueue{
						{QueueID: 0, WorkerID: 1, Mode: "polling"},
					},
					Tx: []model.InterfaceTxQueue{
						{QueueID: 0, Shared: true, Threads: []uint32{0, 2}},
					},
				},
			},
		},
	})

	states, err := provider.InterfaceStates(context.Background())
	if err != nil {
		t.Fatalf("InterfaceStates() error = %v", err)
	}
	state := states["ge-0/0/0"]
	if state == nil {
		t.Fatal("InterfaceStates() missing ge-0/0/0")
	}
	if state.AdminStatus != "up" || state.OperStatus != "down" || state.MAC != "02:00:00:00:00:01" || state.QoSProfile != "WAN" {
		t.Fatalf("state = %#v", state)
	}
	if state.Counters == nil || state.Counters.RxPackets != 10 || state.Counters.TxPackets != 20 ||
		state.Counters.RxBytes != 1000 || state.Counters.TxBytes != 2000 ||
		state.Counters.RxErrors != 1 || state.Counters.TxErrors != 2 || state.Counters.Drops != 3 {
		t.Fatalf("counters = %#v", state.Counters)
	}
	if state.Queues == nil || len(state.Queues.Rx) != 1 || len(state.Queues.Tx) != 1 {
		t.Fatalf("queues = %#v", state.Queues)
	}
	if got := state.Queues.Rx[0]; got.QueueID != 0 || got.WorkerID != 1 || got.Mode != "polling" {
		t.Fatalf("rx queue = %#v", got)
	}
	if got := state.Queues.Tx[0]; got.QueueID != 0 || !got.Shared || len(got.Threads) != 2 || got.Threads[0] != 0 || got.Threads[1] != 2 {
		t.Fatalf("tx queue = %#v", got)
	}
}
