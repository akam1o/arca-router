package main

import (
	"context"
	"testing"
	"time"

	"github.com/akam1o/arca-router/internal/model"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	pkgfrr "github.com/akam1o/arca-router/pkg/frr"
)

type fakeInterfaceStateCollector struct {
	states map[string]*model.InterfaceState
	err    error
}

func (c fakeInterfaceStateCollector) CollectState(ctx context.Context) (map[string]*model.InterfaceState, error) {
	return c.states, c.err
}

type fakeNETCONFBFDStatusSource struct {
	status sbfrr.BFDOperationalStatus
}

func (s fakeNETCONFBFDStatusSource) BFDOperationalStatus() sbfrr.BFDOperationalStatus {
	return s.status
}

type fakeNETCONFRouteStatusReader struct {
	status *pkgfrr.RouteStatus
	err    error
}

func (r fakeNETCONFRouteStatusReader) ReadRouteStatus(ctx context.Context) (*pkgfrr.RouteStatus, error) {
	return r.status, r.err
}

type fakeNETCONFBGPSummaryStatusReader struct {
	status *pkgfrr.BGPSummaryStatus
	err    error
}

func (r fakeNETCONFBGPSummaryStatusReader) ReadBGPSummaryStatus(ctx context.Context) (*pkgfrr.BGPSummaryStatus, error) {
	return r.status, r.err
}

func TestNewNETCONFOperationalStateProviderNilCollector(t *testing.T) {
	if provider := newNETCONFOperationalStateProvider(nil, nil); provider != nil {
		t.Fatalf("newNETCONFOperationalStateProvider(nil, nil) = %#v, want nil", provider)
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
				IPv4TableID: 100,
				IPv6TableID: 100,
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
	}, nil)

	states, err := provider.InterfaceStates(context.Background())
	if err != nil {
		t.Fatalf("InterfaceStates() error = %v", err)
	}
	state := states["ge-0/0/0"]
	if state == nil {
		t.Fatal("InterfaceStates() missing ge-0/0/0")
	}
	if state.AdminStatus != "up" || state.OperStatus != "down" || state.MAC != "02:00:00:00:00:01" || state.QoSProfile != "WAN" || state.IPv4TableID != 100 || state.IPv6TableID != 100 {
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

func TestNETCONFOperationalStateProviderConvertsRouteStatus(t *testing.T) {
	provider := &netconfOperationalStateProvider{
		routeReader: fakeNETCONFRouteStatusReader{status: &pkgfrr.RouteStatus{
			Routes: []pkgfrr.RouteStatusEntry{
				{
					Prefix:    "2001:db8::/64",
					NextHop:   "fe80::1",
					Protocol:  "bgp",
					Metric:    20,
					Interface: "ge-0/0/0",
					Active:    true,
				},
			},
		}},
	}

	routes, err := provider.Routes(context.Background())
	if err != nil {
		t.Fatalf("Routes() error = %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("Routes() len = %d, want 1", len(routes))
	}
	route := routes[0]
	if route.Prefix != "2001:db8::/64" || route.NextHop != "fe80::1" || route.Protocol != "bgp" ||
		route.Metric != 20 || route.Interface != "ge-0/0/0" || !route.Active {
		t.Fatalf("Routes()[0] = %#v, want converted route state", route)
	}
}

func TestNETCONFOperationalStateProviderConvertsBGPSummaryStatus(t *testing.T) {
	provider := &netconfOperationalStateProvider{
		bgpReader: fakeNETCONFBGPSummaryStatusReader{status: &pkgfrr.BGPSummaryStatus{
			Neighbors: []pkgfrr.BGPNeighborStatus{
				{
					PeerAddress:    "2001:db8::2",
					PeerAS:         65001,
					State:          "Established",
					UptimeSecs:     3661,
					PrefixReceived: 10,
					PrefixSent:     20,
				},
			},
		}},
	}

	neighbors, err := provider.BGPNeighbors(context.Background())
	if err != nil {
		t.Fatalf("BGPNeighbors() error = %v", err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("BGPNeighbors() len = %d, want 1", len(neighbors))
	}
	neighbor := neighbors[0]
	if neighbor.PeerAddress != "2001:db8::2" || neighbor.PeerAS != 65001 || neighbor.State != "Established" ||
		neighbor.UptimeSecs != 3661 || neighbor.PrefixReceived != 10 || neighbor.PrefixSent != 20 {
		t.Fatalf("BGPNeighbors()[0] = %#v, want converted BGP neighbor state", neighbor)
	}
}

func TestNETCONFOperationalStateProviderConvertsBFDStatus(t *testing.T) {
	lastRun := time.Date(2026, 5, 14, 6, 0, 0, 0, time.UTC)
	provider := newNETCONFOperationalStateProvider(nil, fakeNETCONFBFDStatusSource{status: sbfrr.BFDOperationalStatus{
		LastRun:           lastRun,
		ConfiguredPeers:   1,
		ObservedPeers:     1,
		UpPeers:           0,
		DownPeers:         1,
		SessionDownEvents: 2,
		RxFailPackets:     3,
		Issues:            []string{"peer down"},
		LastError:         "last read failed",
		Peers: []sbfrr.BFDPeerOperationalStatus{
			{
				Peer:              "192.0.2.2",
				LocalAddress:      "192.0.2.1",
				Interface:         "ge-0/0/0",
				VRF:               "BLUE",
				Status:            "down",
				Diagnostic:        "control detection time expired",
				RemoteDiagnostic:  "none",
				Observed:          true,
				Up:                false,
				SessionDownEvents: 2,
				RxFailPackets:     3,
			},
		},
	}})

	status, err := provider.BFDStatus(context.Background())
	if err != nil {
		t.Fatalf("BFDStatus() error = %v", err)
	}
	if status == nil {
		t.Fatal("BFDStatus() = nil")
	}
	if !status.LastRun.Equal(lastRun) || status.ConfiguredPeers != 1 || status.DownPeers != 1 ||
		status.SessionDownEvents != 2 || status.RxFailPackets != 3 || status.LastError != "last read failed" {
		t.Fatalf("BFDStatus() = %#v, want converted aggregate state", status)
	}
	if len(status.Issues) != 1 || status.Issues[0] != "peer down" {
		t.Fatalf("BFDStatus().Issues = %#v, want peer down", status.Issues)
	}
	if len(status.Peers) != 1 || status.Peers[0].Peer != "192.0.2.2" || status.Peers[0].VRF != "BLUE" ||
		status.Peers[0].Status != "down" || status.Peers[0].SessionDownEvents != 2 || status.Peers[0].RxFailPackets != 3 {
		t.Fatalf("BFDStatus().Peers = %#v, want converted peer state", status.Peers)
	}
}
