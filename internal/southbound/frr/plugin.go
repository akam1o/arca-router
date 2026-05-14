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
	mu          sync.Mutex
	applier     pkgfrr.Applier
	fileApplier pkgfrr.Applier
	mode        pkgfrr.BackendMode
	log         *slog.Logger

	statusReader    pkgfrr.VRRPStatusReader
	bfdStatusReader pkgfrr.BFDStatusReader
	statusCancel    context.CancelFunc
	vrrpStatus      VRRPOperationalStatus
	bfdStatus       BFDOperationalStatus

	currentConfig     string
	rollbackConfig    string
	currentFRRConfig  *pkgfrr.Config
	rollbackFRRConfig *pkgfrr.Config
	currentApplyMode  pkgfrr.BackendMode
	rollbackApplyMode pkgfrr.BackendMode
}

// NewFRRPlugin creates a new FRR plugin.
func NewFRRPlugin(log *slog.Logger) *FRRPlugin {
	return NewFRRPluginWithApplyMode(log, pkgfrr.BackendModeTransactional)
}

// NewFRRPluginWithApplyMode creates a new FRR plugin with an explicit apply backend.
func NewFRRPluginWithApplyMode(log *slog.Logger, mode pkgfrr.BackendMode) *FRRPlugin {
	return &FRRPlugin{
		applier:         pkgfrr.NewApplier(mode),
		fileApplier:     pkgfrr.NewApplier(pkgfrr.BackendModeFile),
		mode:            mode,
		log:             log.With("plugin", "frr", "apply_mode", string(mode)),
		statusReader:    pkgfrr.NewVtyshVRRPStatusReader(),
		bfdStatusReader: pkgfrr.NewVtyshBFDStatusReader(),
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
	go p.runBFDStatusLoop(statusCtx)
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
	return nil
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
	previousApplyMode := p.currentApplyMode
	if previousApplyMode == "" {
		previousApplyMode = p.mode
	}
	if previousConfig == "" && diff.OldConfig != nil {
		if oldFRRConfig, oldConfigContent, oldErr := generateFRRArtifacts(diff.OldConfig); oldErr == nil {
			previousConfig = oldConfigContent
			previousFRRConfig = oldFRRConfig
			previousApplyMode = p.applyModeForConfig(oldFRRConfig)
		}
	}

	applier, applyMode := p.applierForConfig(frrConfig)
	if err := applier.ApplyConfig(ctx, configContent, frrConfig); err != nil {
		return fmt.Errorf("apply FRR config: %w", err)
	}

	p.rollbackConfig = previousConfig
	p.rollbackFRRConfig = previousFRRConfig
	p.rollbackApplyMode = previousApplyMode
	p.currentConfig = configContent
	p.currentFRRConfig = frrConfig
	p.currentApplyMode = applyMode
	p.vrrpStatus = p.checkVRRPOperationalStatus(ctx, frrConfig)
	p.bfdStatus = p.checkBFDOperationalStatus(ctx, frrConfig)

	p.log.Info("FRR configuration applied",
		slog.Int("config_length", len(configContent)),
		slog.String("effective_apply_mode", string(applyMode)),
		slog.Bool("bfd_changed", diff.BFDChanged),
		slog.Bool("bgp_changed", diff.BGPChanged),
		slog.Bool("ospf_changed", diff.OSPFChanged),
		slog.Bool("ospf3_changed", diff.OSPF3Changed),
	)
	p.logVRRPStatus(p.vrrpStatus)
	p.logBFDStatus(p.bfdStatus)

	return nil
}

// VRRPOperationalStatus returns the latest observed FRR VRRP runtime status.
func (p *FRRPlugin) VRRPOperationalStatus() VRRPOperationalStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return cloneVRRPOperationalStatus(p.vrrpStatus)
}

// BFDOperationalStatus returns the latest observed FRR BFD runtime status.
func (p *FRRPlugin) BFDOperationalStatus() BFDOperationalStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return cloneBFDOperationalStatus(p.bfdStatus)
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
	rollbackApplyMode := p.rollbackApplyMode
	if rollbackFRRConfig == nil && diff != nil && diff.OldConfig != nil {
		if oldFRRConfig, oldConfigContent, oldErr := generateFRRArtifacts(diff.OldConfig); oldErr == nil {
			rollbackFRRConfig = oldFRRConfig
			if rollbackConfig == "" {
				rollbackConfig = oldConfigContent
			}
			if rollbackApplyMode == "" {
				rollbackApplyMode = p.applyModeForConfig(oldFRRConfig)
			}
		}
	}

	applier, applyMode := p.applierForRollback(rollbackFRRConfig, rollbackApplyMode)
	if err := applier.ApplyConfig(ctx, rollbackConfig, rollbackFRRConfig); err != nil {
		return fmt.Errorf("rollback FRR config: %w", err)
	}
	p.currentConfig = rollbackConfig
	p.currentFRRConfig = rollbackFRRConfig
	p.currentApplyMode = applyMode
	p.vrrpStatus = p.checkVRRPOperationalStatus(ctx, rollbackFRRConfig)
	p.bfdStatus = p.checkBFDOperationalStatus(ctx, rollbackFRRConfig)

	p.log.Info("FRR configuration rolled back", slog.String("effective_apply_mode", string(applyMode)))
	p.logVRRPStatus(p.vrrpStatus)
	p.logBFDStatus(p.bfdStatus)
	return nil
}

func (p *FRRPlugin) applierForConfig(cfg *pkgfrr.Config) (pkgfrr.Applier, pkgfrr.BackendMode) {
	mode := p.applyModeForConfig(cfg)
	if mode == pkgfrr.BackendModeFile {
		return p.fileBackendApplier(), mode
	}
	return p.applier, mode
}

func (p *FRRPlugin) applierForRollback(cfg *pkgfrr.Config, rollbackMode pkgfrr.BackendMode) (pkgfrr.Applier, pkgfrr.BackendMode) {
	if p.currentApplyMode == pkgfrr.BackendModeFile || rollbackMode == pkgfrr.BackendModeFile {
		return p.fileBackendApplier(), pkgfrr.BackendModeFile
	}
	return p.applierForConfig(cfg)
}

func (p *FRRPlugin) fileBackendApplier() pkgfrr.Applier {
	if p.mode == pkgfrr.BackendModeFile && p.applier != nil {
		return p.applier
	}
	if p.fileApplier != nil {
		return p.fileApplier
	}
	return pkgfrr.NewApplier(pkgfrr.BackendModeFile)
}

func (p *FRRPlugin) applyModeForConfig(cfg *pkgfrr.Config) pkgfrr.BackendMode {
	if p.mode == pkgfrr.BackendModeFile {
		return pkgfrr.BackendModeFile
	}
	if requiresFRRFileBackend(cfg) {
		return pkgfrr.BackendModeFile
	}
	return p.mode
}

func requiresFRRFileBackend(cfg *pkgfrr.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.OSPF3 != nil || frrBGPHasBFDProfiles(cfg.BGP) || frrOSPFHasBFDProfiles(cfg.OSPF)
}

func frrBGPHasBFDProfiles(cfg *pkgfrr.BGPConfig) bool {
	if cfg == nil {
		return false
	}
	for _, neighbor := range cfg.Neighbors {
		if neighbor.BFDProfile != "" {
			return true
		}
	}
	return false
}

func frrOSPFHasBFDProfiles(cfg *pkgfrr.OSPFConfig) bool {
	if cfg == nil {
		return false
	}
	for _, iface := range cfg.Interfaces {
		if iface.BFDProfile != "" {
			return true
		}
	}
	return false
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
