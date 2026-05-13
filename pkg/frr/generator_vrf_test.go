package frr

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestGenerateFRRConfigConvertsRoutingInstances(t *testing.T) {
	cfg := &config.Config{
		RoutingOptions: &config.RoutingOptions{AutonomousSystem: 65000},
		RoutingInstances: map[string]*config.RoutingInstance{
			"BLUE": {
				Name:               "BLUE",
				InstanceType:       "vrf",
				RouteDistinguisher: "65000:100",
				VRFTarget:          "target:65000:100",
				VRFTargetImport:    []string{"target:65000:101"},
				VRFTargetExport:    []string{"target:65000:102"},
			},
		},
	}

	frrCfg, err := GenerateFRRConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateFRRConfig() error = %v", err)
	}
	if len(frrCfg.VRFs) != 1 {
		t.Fatalf("VRFs = %#v, want one VRF", frrCfg.VRFs)
	}
	vrf := frrCfg.VRFs[0]
	if vrf.Name != "BLUE" || vrf.ASN != 65000 || vrf.RouteDistinguisher != "65000:100" {
		t.Fatalf("VRF = %#v, want converted BLUE VRF", vrf)
	}
	if got := strings.Join(vrf.ImportTargets, ","); got != "65000:100,65000:101" {
		t.Fatalf("ImportTargets = %q, want shared and import targets", got)
	}
	if got := strings.Join(vrf.ExportTargets, ","); got != "65000:100,65000:102" {
		t.Fatalf("ExportTargets = %q, want shared and export targets", got)
	}

	text, err := GenerateFRRConfigFile(frrCfg)
	if err != nil {
		t.Fatalf("GenerateFRRConfigFile() error = %v", err)
	}
	for _, want := range []string{
		"vrf BLUE",
		"router bgp 65000 vrf BLUE",
		" address-family ipv4 unicast",
		"  rd vpn export 65000:100",
		"  export vpn",
		"  label vpn export auto",
		"  import vpn",
		"  rt vpn import 65000:100 65000:101",
		"  rt vpn export 65000:100 65000:102",
		" address-family ipv6 unicast",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("FRR config missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateFRRConfigComposesRoutingInstancePolicyChains(t *testing.T) {
	accept := true
	cfg := &config.Config{
		RoutingOptions: &config.RoutingOptions{AutonomousSystem: 65000},
		PolicyOptions: &config.PolicyOptions{
			PolicyStatements: map[string]*config.PolicyStatement{
				"BLUE-IN": {
					Name: "BLUE-IN",
					Terms: []*config.PolicyTerm{
						{Name: "term1", Then: &config.PolicyActions{Accept: &accept}},
					},
				},
				"BLUE-IN-EXTRA": {
					Name: "BLUE-IN-EXTRA",
					Terms: []*config.PolicyTerm{
						{Name: "term1", From: &config.PolicyMatchConditions{Protocol: "bgp"}, Then: &config.PolicyActions{Accept: &accept}},
					},
				},
			},
		},
		RoutingInstances: map[string]*config.RoutingInstance{
			"BLUE": {
				Name:               "BLUE",
				InstanceType:       "vrf",
				RouteDistinguisher: "65000:100",
				VRFTargetImport:    []string{"target:65000:100"},
				VRFImport:          []string{"BLUE-IN", "BLUE-IN-EXTRA"},
			},
		},
	}

	frrCfg, err := GenerateFRRConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateFRRConfig() error = %v", err)
	}
	if len(frrCfg.VRFs) != 1 || frrCfg.VRFs[0].ImportRouteMap != "ARCA-BLUE-VRF-IMPORT" {
		t.Fatalf("VRFs = %#v, want synthetic import route-map", frrCfg.VRFs)
	}
	var synthetic *RouteMap
	for i := range frrCfg.RouteMaps {
		if frrCfg.RouteMaps[i].Name == "ARCA-BLUE-VRF-IMPORT" {
			synthetic = &frrCfg.RouteMaps[i]
			break
		}
	}
	if synthetic == nil || len(synthetic.Entries) != 2 {
		t.Fatalf("synthetic route-map = %#v, want two entries", synthetic)
	}

	text, err := GenerateFRRConfigFile(frrCfg)
	if err != nil {
		t.Fatalf("GenerateFRRConfigFile() error = %v", err)
	}
	if !strings.Contains(text, "  route-map vpn import ARCA-BLUE-VRF-IMPORT") {
		t.Fatalf("FRR config missing synthetic route-map reference:\n%s", text)
	}
}

func TestGenerateFRRConfigRejectsVPNExportWithoutRD(t *testing.T) {
	_, err := GenerateFRRConfig(&config.Config{
		RoutingOptions: &config.RoutingOptions{AutonomousSystem: 65000},
		RoutingInstances: map[string]*config.RoutingInstance{
			"BLUE": {
				Name:            "BLUE",
				InstanceType:    "vrf",
				VRFTargetExport: []string{"target:65000:100"},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "route-distinguisher") {
		t.Fatalf("GenerateFRRConfig() error = %v, want route-distinguisher error", err)
	}
}
