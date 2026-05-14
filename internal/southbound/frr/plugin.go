// Package frr implements the FRR southbound plugin for the config engine.
// It translates ConfigDiff operations into FRR configuration updates.
package frr

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	pkgfrr "github.com/akam1o/arca-router/pkg/frr"
)

// FRRPlugin implements engine.Plugin for FRR routing daemon operations.
type FRRPlugin struct {
	mu      sync.Mutex
	applier pkgfrr.Applier
	mode    pkgfrr.BackendMode
	log     *slog.Logger

	statusReader pkgfrr.VRRPStatusReader
	statusCancel context.CancelFunc
	vrrpStatus   VRRPOperationalStatus

	currentConfig     string
	rollbackConfig    string
	currentFRRConfig  *pkgfrr.Config
	rollbackFRRConfig *pkgfrr.Config
}

// NewFRRPlugin creates a new FRR plugin.
func NewFRRPlugin(log *slog.Logger) *FRRPlugin {
	return NewFRRPluginWithApplyMode(log, pkgfrr.BackendModeTransactional)
}

// NewFRRPluginWithApplyMode creates a new FRR plugin with an explicit apply backend.
func NewFRRPluginWithApplyMode(log *slog.Logger, mode pkgfrr.BackendMode) *FRRPlugin {
	return &FRRPlugin{
		applier:      pkgfrr.NewApplier(mode),
		mode:         mode,
		log:          log.With("plugin", "frr", "apply_mode", string(mode)),
		statusReader: pkgfrr.NewVtyshVRRPStatusReader(),
	}
}

func (p *FRRPlugin) Name() string { return "frr" }

func (p *FRRPlugin) Init(ctx context.Context) error {
	// FRR apply backends are command-driven, so no persistent connection is needed.
	statusCtx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.statusCancel = cancel
	p.mu.Unlock()
	go p.runVRRPStatusLoop(statusCtx)
	return nil
}

func (p *FRRPlugin) Close() error {
	p.mu.Lock()
	cancel := p.statusCancel
	p.statusCancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (p *FRRPlugin) HealthCheck(ctx context.Context) error {
	// Could check if FRR daemons are running
	return nil
}

// ValidateChanges validates that routing configuration changes are feasible.
func (p *FRRPlugin) ValidateChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	if diff == nil {
		return nil
	}
	if p.mode == pkgfrr.BackendModeTransactional && diff.BFDChanged {
		return fmt.Errorf("BFD requires FRR file backend until frr-bfdd management operations are implemented")
	}
	if p.mode == pkgfrr.BackendModeTransactional && diffHasBFDProtocolBindings(diff) {
		return fmt.Errorf("BFD protocol bindings require FRR file backend until frr-bfdd management operations are implemented")
	}
	if p.mode == pkgfrr.BackendModeTransactional && diff.NewOSPF3 != nil {
		return fmt.Errorf("OSPFv3 requires FRR file backend until core ospf6d YANG paths are available")
	}
	return nil
}

func diffHasBFDProtocolBindings(diff *engine.ConfigDiff) bool {
	if diff == nil {
		return false
	}
	if routerConfigHasBFDProtocolBindings(diff.NewConfig) {
		return true
	}
	if diff.BGPChanged && (bgpHasBFDProtocolBindings(diff.OldBGP) || bgpHasBFDProtocolBindings(diff.NewBGP)) {
		return true
	}
	if diff.OSPFChanged && (ospfHasBFDProtocolBindings(diff.OldOSPF) || ospfHasBFDProtocolBindings(diff.NewOSPF)) {
		return true
	}
	if diff.OSPF3Changed && (ospfHasBFDProtocolBindings(diff.OldOSPF3) || ospfHasBFDProtocolBindings(diff.NewOSPF3)) {
		return true
	}
	return false
}

func routerConfigHasBFDProtocolBindings(cfg *model.RouterConfig) bool {
	if cfg == nil || cfg.Protocols == nil {
		return false
	}
	return bgpHasBFDProtocolBindings(cfg.Protocols.BGP) ||
		ospfHasBFDProtocolBindings(cfg.Protocols.OSPF) ||
		ospfHasBFDProtocolBindings(cfg.Protocols.OSPF3)
}

func bgpHasBFDProtocolBindings(cfg *model.BGPConfig) bool {
	if cfg == nil {
		return false
	}
	for _, group := range cfg.Groups {
		if group == nil {
			continue
		}
		for _, neighbor := range group.Neighbors {
			if neighbor != nil && (neighbor.BFD || neighbor.BFDProfile != "") {
				return true
			}
		}
	}
	return false
}

func ospfHasBFDProtocolBindings(cfg *model.OSPFConfig) bool {
	if cfg == nil {
		return false
	}
	for _, area := range cfg.Areas {
		if area == nil {
			continue
		}
		for _, iface := range area.Interfaces {
			if iface != nil && (iface.BFD || iface.BFDProfile != "") {
				return true
			}
		}
	}
	return false
}

// ApplyChanges regenerates the desired FRR view and commits it through the
// configured apply backend. The default backend is transactional management.
func (p *FRRPlugin) ApplyChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Only regenerate FRR config if routing-related changes occurred
	if !diff.BFDChanged && !diff.BGPChanged && !diff.OSPFChanged && !diff.OSPF3Changed && !diff.StaticRoutesChanged &&
		!diff.PolicyChanged && !diff.RoutingChanged && !diff.SystemChanged && !diff.VRRPChanged &&
		!diff.RoutingInstancesChanged &&
		!hasFRRRelevantInterfaceChanges(diff) {
		p.log.Debug("No routing-related changes, skipping FRR reload")
		return nil
	}

	// Build the full RouterConfig needed for FRR generation
	// We need the complete new config, not just the diff, because FRR config
	// generation is whole-file based. The diff tells us *whether* to regenerate.
	newCfg := p.buildFullConfig(diff)

	frrConfig, configContent, err := generateFRRArtifacts(newCfg)
	if err != nil {
		return err
	}

	previousConfig := p.currentConfig
	previousFRRConfig := p.currentFRRConfig
	if previousConfig == "" && diff.OldConfig != nil {
		if oldFRRConfig, oldConfigContent, oldErr := generateFRRArtifacts(diff.OldConfig); oldErr == nil {
			previousConfig = oldConfigContent
			previousFRRConfig = oldFRRConfig
		}
	}

	if err := p.applier.ApplyConfig(ctx, configContent, frrConfig); err != nil {
		return fmt.Errorf("apply FRR config: %w", err)
	}

	p.rollbackConfig = previousConfig
	p.rollbackFRRConfig = previousFRRConfig
	p.currentConfig = configContent
	p.currentFRRConfig = frrConfig
	p.vrrpStatus = p.checkVRRPOperationalStatus(ctx, frrConfig)

	p.log.Info("FRR configuration applied",
		slog.Int("config_length", len(configContent)),
		slog.Bool("bfd_changed", diff.BFDChanged),
		slog.Bool("bgp_changed", diff.BGPChanged),
		slog.Bool("ospf_changed", diff.OSPFChanged),
		slog.Bool("ospf3_changed", diff.OSPF3Changed),
	)
	p.logVRRPStatus(p.vrrpStatus)

	return nil
}

// VRRPOperationalStatus returns the latest observed FRR VRRP runtime status.
func (p *FRRPlugin) VRRPOperationalStatus() VRRPOperationalStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return cloneVRRPOperationalStatus(p.vrrpStatus)
}

func generateFRRArtifacts(cfg *model.RouterConfig) (*pkgfrr.Config, string, error) {
	legacyCfg := cfg.ToLegacyConfig()
	frrConfig, err := pkgfrr.GenerateFRRConfig(legacyCfg)
	if err != nil {
		return nil, "", fmt.Errorf("generate FRR config: %w", err)
	}
	configContent, err := pkgfrr.GenerateFRRConfigFile(frrConfig)
	if err != nil {
		return nil, "", fmt.Errorf("generate FRR config file: %w", err)
	}
	return frrConfig, configContent, nil
}

// RollbackChanges reverts to the previous FRR configuration.
func (p *FRRPlugin) RollbackChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.rollbackConfig == "" {
		p.log.Warn("No previous FRR config for rollback")
		return nil
	}

	rollbackConfig := p.rollbackConfig
	rollbackFRRConfig := p.rollbackFRRConfig
	if rollbackFRRConfig == nil && diff != nil && diff.OldConfig != nil {
		if oldFRRConfig, oldConfigContent, oldErr := generateFRRArtifacts(diff.OldConfig); oldErr == nil {
			rollbackFRRConfig = oldFRRConfig
			if rollbackConfig == "" {
				rollbackConfig = oldConfigContent
			}
		}
	}

	if err := p.applier.ApplyConfig(ctx, rollbackConfig, rollbackFRRConfig); err != nil {
		return fmt.Errorf("rollback FRR config: %w", err)
	}
	p.currentConfig = rollbackConfig
	p.currentFRRConfig = rollbackFRRConfig
	p.vrrpStatus = p.checkVRRPOperationalStatus(ctx, rollbackFRRConfig)

	p.log.Info("FRR configuration rolled back")
	p.logVRRPStatus(p.vrrpStatus)
	return nil
}

// buildFullConfig reconstructs the complete RouterConfig from the diff's new state.
// This is needed because FRR generates the entire config file, not incremental changes.
func (p *FRRPlugin) buildFullConfig(diff *engine.ConfigDiff) *model.RouterConfig {
	if diff.NewConfig != nil {
		return diff.NewConfig
	}

	cfg := model.NewRouterConfig()

	// System
	if diff.NewSystem != nil {
		cfg.System = diff.NewSystem
	} else if diff.OldSystem != nil {
		cfg.System = diff.OldSystem
	}

	// Routing
	if diff.NewRouting != nil {
		cfg.Routing = diff.NewRouting
	} else if diff.OldRouting != nil {
		cfg.Routing = diff.OldRouting
	}

	// Protocols
	cfg.Protocols = &model.ProtocolsConfig{}
	if diff.NewBFD != nil {
		cfg.Protocols.BFD = diff.NewBFD
	} else if diff.OldBFD != nil && !diff.BFDChanged {
		cfg.Protocols.BFD = diff.OldBFD
	}
	if diff.NewBGP != nil {
		cfg.Protocols.BGP = diff.NewBGP
	} else if diff.OldBGP != nil && !diff.BGPChanged {
		cfg.Protocols.BGP = diff.OldBGP
	}
	if diff.NewOSPF != nil {
		cfg.Protocols.OSPF = diff.NewOSPF
	} else if diff.OldOSPF != nil && !diff.OSPFChanged {
		cfg.Protocols.OSPF = diff.OldOSPF
	}
	if diff.NewOSPF3 != nil {
		cfg.Protocols.OSPF3 = diff.NewOSPF3
	} else if diff.OldOSPF3 != nil && !diff.OSPF3Changed {
		cfg.Protocols.OSPF3 = diff.OldOSPF3
	}
	if diff.NewVRRP != nil {
		cfg.Protocols.VRRP = diff.NewVRRP
	} else if diff.OldVRRP != nil && !diff.VRRPChanged {
		cfg.Protocols.VRRP = diff.OldVRRP
	}

	// Policy
	if diff.NewPolicy != nil {
		cfg.Policy = diff.NewPolicy
	} else if diff.OldPolicy != nil && !diff.PolicyChanged {
		cfg.Policy = diff.OldPolicy
	}

	if diff.NewRoutingInstances != nil {
		cfg.RoutingInstances = diff.NewRoutingInstances
	} else if diff.OldRoutingInstances != nil && !diff.RoutingInstancesChanged {
		cfg.RoutingInstances = diff.OldRoutingInstances
	}

	// Static routes
	if diff.NewStaticRoutes != nil {
		if cfg.Routing == nil {
			cfg.Routing = &model.RoutingConfig{}
		}
		cfg.Routing.StaticRoutes = diff.NewStaticRoutes
	} else if diff.OldStaticRoutes != nil && !diff.StaticRoutesChanged {
		if cfg.Routing == nil {
			cfg.Routing = &model.RoutingConfig{}
		}
		cfg.Routing.StaticRoutes = diff.OldStaticRoutes
	}

	return cfg
}

func hasFRRRelevantInterfaceChanges(diff *engine.ConfigDiff) bool {
	if len(diff.InterfacesAdded) > 0 || len(diff.InterfacesRemoved) > 0 {
		return true
	}
	for _, change := range diff.InterfacesChanged {
		if len(change.AddressesAdded) > 0 || len(change.AddressesRemoved) > 0 {
			return true
		}
	}
	return false
}
