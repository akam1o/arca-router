package frr

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	pkgfrr "github.com/akam1o/arca-router/pkg/frr"
)

const defaultVRRPStatusInterval = 5 * time.Second

// VRRPOperationalStatus is the latest FRR VRRP runtime state observed by arca-routerd.
type VRRPOperationalStatus struct {
	LastRun          time.Time
	ConfiguredGroups int
	ObservedGroups   int
	ActiveGroups     int
	Issues           []string
	LastError        string
}

func (p *FRRPlugin) runVRRPStatusLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultVRRPStatusInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.refreshVRRPStatus(ctx)
		}
	}
}

func (p *FRRPlugin) refreshVRRPStatus(ctx context.Context) {
	p.mu.Lock()
	cfg := p.currentFRRConfig
	p.mu.Unlock()
	status := p.checkVRRPOperationalStatus(ctx, cfg)
	p.mu.Lock()
	p.vrrpStatus = status
	p.mu.Unlock()
	p.logVRRPStatus(status)
}

func (p *FRRPlugin) checkVRRPOperationalStatus(ctx context.Context, cfg *pkgfrr.Config) VRRPOperationalStatus {
	status := VRRPOperationalStatus{
		LastRun:          time.Now(),
		ConfiguredGroups: configuredVRRPGroupCount(cfg),
	}
	if status.ConfiguredGroups == 0 {
		return status
	}
	if p.statusReader == nil {
		status.LastError = "FRR VRRP status reader is unavailable"
		status.Issues = []string{status.LastError}
		return status
	}
	observed, err := p.statusReader.ReadVRRPStatus(ctx)
	if err != nil {
		status.LastError = err.Error()
		status.Issues = []string{"read FRR VRRP status failed"}
		return status
	}
	fillVRRPConvergenceStatus(&status, cfg, observed)
	return status
}

func fillVRRPConvergenceStatus(status *VRRPOperationalStatus, cfg *pkgfrr.Config, observed *pkgfrr.VRRPStatus) {
	if status == nil || cfg == nil || cfg.VRRP == nil {
		return
	}
	observedByKey := make(map[string]pkgfrr.VRRPRouterStatus)
	if observed != nil {
		for _, group := range observed.Groups {
			if group.Interface == "" || group.VRID == 0 {
				continue
			}
			observedByKey[vrrpOperationalKey(group.Interface, group.VRID)] = group
		}
	}

	for _, expected := range cfg.VRRP.Groups {
		key := vrrpOperationalKey(expected.Interface, expected.ID)
		group, ok := observedByKey[key]
		if !ok {
			status.Issues = append(status.Issues,
				fmt.Sprintf("FRR VRRP group %d on %s is missing", expected.ID, expected.Interface))
			continue
		}
		status.ObservedGroups++
		state := observedStateForExpectedFamily(group, expected.VirtualAddress)
		if isActiveVRRPState(state) {
			status.ActiveGroups++
			continue
		}
		if state == "" {
			state = "unknown"
		}
		status.Issues = append(status.Issues,
			fmt.Sprintf("FRR VRRP group %d on %s is not active: %s", expected.ID, expected.Interface, state))
	}
}

func configuredVRRPGroupCount(cfg *pkgfrr.Config) int {
	if cfg == nil || cfg.VRRP == nil {
		return 0
	}
	return len(cfg.VRRP.Groups)
}

func observedStateForExpectedFamily(status pkgfrr.VRRPRouterStatus, virtualAddress string) string {
	ip := net.ParseIP(virtualAddress)
	if ip != nil && ip.To4() == nil {
		if status.IPv6State != "" {
			return status.IPv6State
		}
		return status.State
	}
	if status.IPv4State != "" {
		return status.IPv4State
	}
	return status.State
}

func isActiveVRRPState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "master", "backup":
		return true
	default:
		return false
	}
}

func vrrpOperationalKey(iface string, id int) string {
	return fmt.Sprintf("%s:%d", iface, id)
}

func (p *FRRPlugin) logVRRPStatus(status VRRPOperationalStatus) {
	if status.ConfiguredGroups == 0 {
		return
	}
	if status.LastError != "" {
		p.log.Warn("FRR VRRP status check failed", "error", status.LastError)
		return
	}
	if len(status.Issues) > 0 {
		p.log.Warn("FRR VRRP status found convergence issues",
			"configured_groups", status.ConfiguredGroups,
			"observed_groups", status.ObservedGroups,
			"active_groups", status.ActiveGroups,
			"issues", len(status.Issues))
		return
	}
	p.log.Info("FRR VRRP status converged",
		"configured_groups", status.ConfiguredGroups,
		"active_groups", status.ActiveGroups)
}

func cloneVRRPOperationalStatus(status VRRPOperationalStatus) VRRPOperationalStatus {
	status.Issues = append([]string(nil), status.Issues...)
	return status
}
