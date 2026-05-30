package frr

import (
	"context"
	"fmt"
	"strings"
	"time"

	pkgfrr "github.com/akam1o/arca-router/pkg/frr"
)

const (
	defaultBFDStatusInterval     = 5 * time.Second
	frrBFDStatusReadErrorMessage = "read FRR BFD status failed"
)

// BFDOperationalStatus is the latest FRR BFD runtime state observed by arca-routerd.
type BFDOperationalStatus struct {
	LastRun           time.Time
	ConfiguredPeers   int
	ObservedPeers     int
	UpPeers           int
	DownPeers         int
	SessionDownEvents int
	RxFailPackets     int
	Peers             []BFDPeerOperationalStatus
	Issues            []string
	LastError         string
}

// BFDPeerOperationalStatus is the observed state for one BFD peer.
type BFDPeerOperationalStatus struct {
	Peer              string
	LocalAddress      string
	Interface         string
	VRF               string
	Status            string
	Diagnostic        string
	RemoteDiagnostic  string
	Observed          bool
	Up                bool
	SessionDownEvents int
	RxFailPackets     int
}

type expectedBFDPeer struct {
	Peer         string
	LocalAddress string
	Interface    string
	VRF          string
	Source       string
}

func (p *FRRPlugin) runBFDStatusLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultBFDStatusInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.refreshBFDStatus(ctx)
		}
	}
}

func (p *FRRPlugin) refreshBFDStatus(ctx context.Context) {
	p.mu.Lock()
	cfg := p.currentFRRConfig
	p.mu.Unlock()
	status := p.checkBFDOperationalStatus(ctx, cfg)
	p.mu.Lock()
	p.bfdStatus = status
	p.mu.Unlock()
	p.logBFDStatus(status)
}

func (p *FRRPlugin) checkBFDOperationalStatus(ctx context.Context, cfg *pkgfrr.Config) BFDOperationalStatus {
	expected := expectedBFDPeers(cfg)
	status := BFDOperationalStatus{
		LastRun:         time.Now(),
		ConfiguredPeers: len(expected),
	}
	if !hasBFDOperationalInputs(cfg) {
		return status
	}
	if p.bfdStatusReader == nil {
		status.LastError = "FRR BFD status reader is unavailable"
		status.Issues = []string{status.LastError}
		return status
	}
	observed, err := p.bfdStatusReader.ReadBFDStatus(ctx)
	if err != nil {
		status.LastError = frrBFDStatusReadErrorMessage
		status.Issues = []string{frrBFDStatusReadErrorMessage}
		return status
	}
	fillBFDConvergenceStatus(&status, expected, observed)
	return status
}

func fillBFDConvergenceStatus(status *BFDOperationalStatus, expected []expectedBFDPeer, observed *pkgfrr.BFDStatus) {
	if status == nil {
		return
	}
	observedPeers := validBFDPeers(observed)
	status.ObservedPeers = len(observedPeers)
	matched := make(map[int]struct{}, len(expected))

	for _, peer := range expected {
		peerStatus := BFDPeerOperationalStatus{
			Peer:         peer.Peer,
			LocalAddress: peer.LocalAddress,
			Interface:    peer.Interface,
			VRF:          peer.VRF,
			Status:       "missing",
		}
		index, ok := bestBFDPeerMatch(peer, observedPeers)
		if !ok {
			status.Peers = append(status.Peers, peerStatus)
			status.Issues = append(status.Issues, fmt.Sprintf("FRR BFD peer %s for %s is missing", peer.Peer, peer.Source))
			continue
		}
		matched[index] = struct{}{}
		observedStatus := bfdOperationalPeerStatus(observedPeers[index])
		status.Peers = append(status.Peers, observedStatus)
		recordBFDObservedStatus(status, observedStatus)
		if !observedStatus.Up {
			state := observedStatus.Status
			if state == "" {
				state = "unknown"
			}
			status.Issues = append(status.Issues, fmt.Sprintf("FRR BFD peer %s for %s is not up: %s", peer.Peer, peer.Source, state))
		}
	}

	for index, peer := range observedPeers {
		if _, ok := matched[index]; ok {
			continue
		}
		observedStatus := bfdOperationalPeerStatus(peer)
		status.Peers = append(status.Peers, observedStatus)
		recordBFDObservedStatus(status, observedStatus)
	}
}

func validBFDPeers(observed *pkgfrr.BFDStatus) []pkgfrr.BFDPeerStatus {
	if observed == nil {
		return nil
	}
	peers := make([]pkgfrr.BFDPeerStatus, 0, len(observed.Peers))
	for _, peer := range observed.Peers {
		if strings.TrimSpace(peer.Peer) != "" {
			peers = append(peers, peer)
		}
	}
	return peers
}

func recordBFDObservedStatus(status *BFDOperationalStatus, peer BFDPeerOperationalStatus) {
	if status == nil {
		return
	}
	if peer.Up {
		status.UpPeers++
	} else {
		status.DownPeers++
	}
	status.SessionDownEvents += peer.SessionDownEvents
	status.RxFailPackets += peer.RxFailPackets
}

func bfdOperationalPeerStatus(peer pkgfrr.BFDPeerStatus) BFDPeerOperationalStatus {
	state := strings.TrimSpace(peer.Status)
	return BFDPeerOperationalStatus{
		Peer:              peer.Peer,
		LocalAddress:      peer.LocalAddress,
		Interface:         peer.Interface,
		VRF:               peer.VRF,
		Status:            state,
		Diagnostic:        peer.Diagnostic,
		RemoteDiagnostic:  peer.RemoteDiagnostic,
		Observed:          true,
		Up:                isUpBFDState(state),
		SessionDownEvents: peer.SessionDownEvents,
		RxFailPackets:     peer.RxFailPackets,
	}
}

func hasBFDOperationalInputs(cfg *pkgfrr.Config) bool {
	return len(expectedBFDPeers(cfg)) > 0 || ospfBFDInterfaceCount(cfg) > 0
}

func expectedBFDPeers(cfg *pkgfrr.Config) []expectedBFDPeer {
	if cfg == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var peers []expectedBFDPeer
	add := func(peer expectedBFDPeer) {
		peer.Peer = strings.TrimSpace(peer.Peer)
		if peer.Peer == "" {
			return
		}
		key := bfdExpectedPeerKey(peer)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		if peer.Source == "" {
			peer.Source = "configured peer"
		}
		peers = append(peers, peer)
	}
	if cfg.BFD != nil {
		for _, peer := range cfg.BFD.Peers {
			if peer.Shutdown {
				continue
			}
			add(expectedBFDPeer{
				Peer:         peer.Address,
				LocalAddress: peer.LocalAddress,
				Interface:    peer.Interface,
				VRF:          peer.VRF,
				Source:       "configured peer",
			})
		}
	}
	if cfg.BGP != nil {
		for _, neighbor := range cfg.BGP.Neighbors {
			if neighbor.BFD || neighbor.BFDProfile != "" {
				add(expectedBFDPeer{Peer: neighbor.IP, Source: "BGP neighbor"})
			}
		}
	}
	for _, route := range cfg.StaticRoutes {
		if route.BFD || route.BFDProfile != "" || route.BFDSource != "" || route.BFDMultihop {
			add(expectedBFDPeer{
				Peer:         route.NextHop,
				LocalAddress: route.BFDSource,
				Source:       "static route",
			})
		}
	}
	return peers
}

func ospfBFDInterfaceCount(cfg *pkgfrr.Config) int {
	if cfg == nil {
		return 0
	}
	return ospfConfigBFDInterfaceCount(cfg.OSPF) + ospfConfigBFDInterfaceCount(cfg.OSPF3)
}

func ospfConfigBFDInterfaceCount(cfg *pkgfrr.OSPFConfig) int {
	if cfg == nil {
		return 0
	}
	count := 0
	for _, iface := range cfg.Interfaces {
		if iface.BFD || iface.BFDProfile != "" {
			count++
		}
	}
	return count
}

func bestBFDPeerMatch(expected expectedBFDPeer, observed []pkgfrr.BFDPeerStatus) (int, bool) {
	bestIndex := -1
	bestScore := 0
	for index, peer := range observed {
		score, ok := bfdPeerMatchScore(expected, peer)
		if !ok || score <= bestScore {
			continue
		}
		bestIndex = index
		bestScore = score
	}
	if bestIndex == -1 {
		return 0, false
	}
	return bestIndex, true
}

func bfdPeerMatchScore(expected expectedBFDPeer, observed pkgfrr.BFDPeerStatus) (int, bool) {
	if !sameBFDValue(expected.Peer, observed.Peer) {
		return 0, false
	}
	score := 1
	for _, pair := range []struct {
		expected string
		observed string
	}{
		{expected: expected.LocalAddress, observed: observed.LocalAddress},
		{expected: expected.Interface, observed: observed.Interface},
		{expected: expected.VRF, observed: observed.VRF},
	} {
		if strings.TrimSpace(pair.expected) == "" {
			continue
		}
		if strings.TrimSpace(pair.observed) != "" && !sameBFDValue(pair.expected, pair.observed) {
			return 0, false
		}
		if sameBFDValue(pair.expected, pair.observed) {
			score += 2
		}
	}
	return score, true
}

func bfdExpectedPeerKey(peer expectedBFDPeer) string {
	return strings.Join([]string{
		normalizeBFDValue(peer.Peer),
		normalizeBFDValue(peer.LocalAddress),
		normalizeBFDValue(peer.Interface),
		normalizeBFDValue(peer.VRF),
	}, "|")
}

func sameBFDValue(a, b string) bool {
	return normalizeBFDValue(a) == normalizeBFDValue(b)
}

func normalizeBFDValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isUpBFDState(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "up")
}

func (p *FRRPlugin) logBFDStatus(status BFDOperationalStatus) {
	if status.ConfiguredPeers == 0 && status.ObservedPeers == 0 {
		return
	}
	if status.LastError != "" {
		p.log.Warn("FRR BFD status check failed", "error", status.LastError)
		return
	}
	if len(status.Issues) > 0 {
		p.log.Warn("FRR BFD status found convergence issues",
			"configured_peers", status.ConfiguredPeers,
			"observed_peers", status.ObservedPeers,
			"up_peers", status.UpPeers,
			"down_peers", status.DownPeers,
			"issues", len(status.Issues))
		return
	}
	p.log.Info("FRR BFD status converged",
		"configured_peers", status.ConfiguredPeers,
		"up_peers", status.UpPeers,
		"session_down_events", status.SessionDownEvents,
		"rx_fail_packets", status.RxFailPackets)
}

func cloneBFDOperationalStatus(status BFDOperationalStatus) BFDOperationalStatus {
	status.Peers = append([]BFDPeerOperationalStatus(nil), status.Peers...)
	status.Issues = append([]string(nil), status.Issues...)
	return status
}
