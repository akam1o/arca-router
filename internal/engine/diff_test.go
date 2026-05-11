package engine

import (
	"testing"

	"github.com/akam1o/arca-router/internal/model"
)

func TestComputeDiffDetectsPolicyTermChanges(t *testing.T) {
	accept := true
	oldCfg := model.NewRouterConfig()
	oldCfg.Policy = &model.PolicyConfig{
		PolicyStatements: map[string]*model.PolicyStatement{
			"IMPORT": {
				Terms: []*model.PolicyTerm{
					{Name: "10", Then: &model.PolicyActions{Accept: &accept}},
				},
			},
		},
	}

	localPref := uint32(200)
	newCfg := model.NewRouterConfig()
	newCfg.Policy = &model.PolicyConfig{
		PolicyStatements: map[string]*model.PolicyStatement{
			"IMPORT": {
				Terms: []*model.PolicyTerm{
					{Name: "10", Then: &model.PolicyActions{Accept: &accept, LocalPreference: &localPref}},
				},
			},
		},
	}

	diff := ComputeDiff(oldCfg, newCfg)
	if !diff.PolicyChanged {
		t.Fatal("ComputeDiff() did not detect policy term content change")
	}
}

func TestComputeDiffDetectsSecurityRateLimitChanges(t *testing.T) {
	oldCfg := model.NewRouterConfig()
	oldCfg.Security = &model.SecurityConfig{
		RateLimit: &model.RateLimitConfig{PerIP: 10},
	}
	newCfg := model.NewRouterConfig()
	newCfg.Security = &model.SecurityConfig{
		RateLimit: &model.RateLimitConfig{PerIP: 20},
	}

	diff := ComputeDiff(oldCfg, newCfg)
	if !diff.SecurityChanged {
		t.Fatal("ComputeDiff() did not detect security rate-limit change")
	}
}
