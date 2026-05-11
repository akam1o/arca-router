// Package frr implements the FRR southbound plugin for the config engine.
// It translates ConfigDiff operations into FRR configuration updates.
package frr

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	pkgfrr "github.com/akam1o/arca-router/pkg/frr"
)

// FRRPlugin implements engine.Plugin for FRR routing daemon operations.
type FRRPlugin struct {
	mu       sync.Mutex
	reloader *pkgfrr.Reloader
	log      *slog.Logger

	currentConfig  string
	rollbackConfig string
}

// NewFRRPlugin creates a new FRR plugin.
func NewFRRPlugin(log *slog.Logger) *FRRPlugin {
	return &FRRPlugin{
		reloader: pkgfrr.NewReloader(),
		log:      log.With("plugin", "frr"),
	}
}

func (p *FRRPlugin) Name() string { return "frr" }

func (p *FRRPlugin) Init(ctx context.Context) error {
	// FRR is managed via config file + reload, no persistent connection needed
	return nil
}

func (p *FRRPlugin) Close() error {
	return nil
}

func (p *FRRPlugin) HealthCheck(ctx context.Context) error {
	// Could check if FRR daemons are running
	return nil
}

// ValidateChanges validates that routing configuration changes are feasible.
func (p *FRRPlugin) ValidateChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	// FRR validation is done at config parse time.
	// Additional validation could check that referenced interfaces exist in VPP.
	return nil
}

// ApplyChanges regenerates the FRR configuration and reloads.
// Currently does a full regeneration — incremental updates via FRR MGMT API
// is a future optimization.
func (p *FRRPlugin) ApplyChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Only regenerate FRR config if routing-related changes occurred
	if !diff.BGPChanged && !diff.OSPFChanged && !diff.StaticRoutesChanged &&
		!diff.PolicyChanged && !diff.RoutingChanged && !diff.SystemChanged &&
		!hasFRRRelevantInterfaceChanges(diff) {
		p.log.Debug("No routing-related changes, skipping FRR reload")
		return nil
	}

	// Build the full RouterConfig needed for FRR generation
	// We need the complete new config, not just the diff, because FRR config
	// generation is whole-file based. The diff tells us *whether* to regenerate.
	newCfg := p.buildFullConfig(diff)

	// Convert to legacy config for the existing FRR generator
	legacyCfg := newCfg.ToLegacyConfig()

	// Generate FRR config
	frrConfig, err := pkgfrr.GenerateFRRConfig(legacyCfg)
	if err != nil {
		return fmt.Errorf("generate FRR config: %w", err)
	}

	configContent, err := pkgfrr.GenerateFRRConfigFile(frrConfig)
	if err != nil {
		return fmt.Errorf("generate FRR config file: %w", err)
	}

	previousConfig := p.currentConfig
	if previousConfig == "" {
		if data, readErr := os.ReadFile(p.reloader.ConfigPath); readErr == nil {
			previousConfig = string(data)
		}
	}

	// Apply via reloader (atomic write + validation + reload)
	if err := p.reloader.ApplyConfig(ctx, configContent); err != nil {
		return fmt.Errorf("apply FRR config: %w", err)
	}

	p.rollbackConfig = previousConfig
	p.currentConfig = configContent

	p.log.Info("FRR configuration applied",
		slog.Int("config_length", len(configContent)),
		slog.Bool("bgp_changed", diff.BGPChanged),
		slog.Bool("ospf_changed", diff.OSPFChanged),
	)

	return nil
}

// RollbackChanges reverts to the previous FRR configuration.
func (p *FRRPlugin) RollbackChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.rollbackConfig == "" {
		p.log.Warn("No previous FRR config for rollback")
		return nil
	}

	if err := p.reloader.ApplyConfig(ctx, p.rollbackConfig); err != nil {
		return fmt.Errorf("rollback FRR config: %w", err)
	}
	p.currentConfig = p.rollbackConfig

	p.log.Info("FRR configuration rolled back")
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

	// Policy
	if diff.NewPolicy != nil {
		cfg.Policy = diff.NewPolicy
	} else if diff.OldPolicy != nil && !diff.PolicyChanged {
		cfg.Policy = diff.OldPolicy
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
