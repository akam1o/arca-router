package grafana_test

import (
	"encoding/json"
	"os"
	"testing"
)

type dashboard struct {
	Title      string  `json:"title"`
	UID        string  `json:"uid"`
	Version    int     `json:"version"`
	Panels     []panel `json:"panels"`
	Templating struct {
		List []templateVariable `json:"list"`
	} `json:"templating"`
}

type templateVariable struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Query string `json:"query"`
}

type panel struct {
	ID      int      `json:"id"`
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	GridPos gridPos  `json:"gridPos"`
	Targets []target `json:"targets"`
}

type gridPos struct {
	H int `json:"h"`
	W int `json:"w"`
	X int `json:"x"`
	Y int `json:"y"`
}

type target struct {
	Expr  string `json:"expr"`
	RefID string `json:"refId"`
}

func TestDashboardJSONIsValid(t *testing.T) {
	dash := loadDashboard(t)
	if dash.Title != "ARCA Router Daemon" {
		t.Fatalf("Title = %q, want ARCA Router Daemon", dash.Title)
	}
	if dash.UID != "arca-routerd" {
		t.Fatalf("UID = %q, want arca-routerd", dash.UID)
	}
	if dash.Version < 1 {
		t.Fatalf("Version = %d, want positive", dash.Version)
	}
	if len(dash.Panels) == 0 {
		t.Fatal("Panels is empty")
	}
	if !hasPrometheusDatasourceVariable(dash.Templating.List) {
		t.Fatal("dashboard missing Prometheus datasource variable")
	}

	ids := make(map[int]string, len(dash.Panels))
	for _, panel := range dash.Panels {
		if panel.ID <= 0 {
			t.Fatalf("panel %q has invalid id %d", panel.Title, panel.ID)
		}
		if previous, ok := ids[panel.ID]; ok {
			t.Fatalf("panel id %d reused by %q and %q", panel.ID, previous, panel.Title)
		}
		ids[panel.ID] = panel.Title
		if panel.Title == "" {
			t.Fatalf("panel id %d has empty title", panel.ID)
		}
		if panel.Type == "" {
			t.Fatalf("panel %q has empty type", panel.Title)
		}
		if panel.GridPos.W <= 0 || panel.GridPos.H <= 0 {
			t.Fatalf("panel %q has invalid grid size %#v", panel.Title, panel.GridPos)
		}
		if panel.GridPos.X < 0 || panel.GridPos.X+panel.GridPos.W > 24 {
			t.Fatalf("panel %q is outside 24-column grid: %#v", panel.Title, panel.GridPos)
		}
		for _, target := range panel.Targets {
			if target.Expr == "" {
				t.Fatalf("panel %q has empty Prometheus expression", panel.Title)
			}
			if target.RefID == "" {
				t.Fatalf("panel %q has empty target refId", panel.Title)
			}
		}
	}
}

func TestDashboardIncludesClassOfServicePanels(t *testing.T) {
	dash := loadDashboard(t)
	want := map[string]string{
		"arca_router_class_of_service_configured":               "Class of Service",
		"arca_router_class_of_service_intent_only":              "CoS Enforcement",
		"arca_router_class_of_service_forwarding_classes":       "CoS Forwarding Classes",
		"arca_router_class_of_service_traffic_control_profiles": "CoS Traffic Profiles",
		"arca_router_class_of_service_interface_bindings":       "CoS Interface Bindings",
	}
	expressions := panelExpressions(dash.Panels)
	for expr, title := range want {
		if got := expressions[expr]; got != title {
			t.Fatalf("expression %q is on panel %q, want %q", expr, got, title)
		}
	}
}

func TestDashboardIncludesEVPNPanels(t *testing.T) {
	dash := loadDashboard(t)
	want := map[string]string{
		"arca_router_overlay_evpn_configured":     "EVPN Overlay",
		"arca_router_overlay_evpn_vnis":           "EVPN VNIs",
		"arca_router_overlay_evpn_l2_vnis":        "EVPN L2 VNIs",
		"arca_router_overlay_evpn_l3_vnis":        "EVPN L3 VNIs",
		"arca_router_overlay_evpn_multicast_vnis": "EVPN Multicast VNIs",
	}
	expressions := panelExpressions(dash.Panels)
	for expr, title := range want {
		if got := expressions[expr]; got != title {
			t.Fatalf("expression %q is on panel %q, want %q", expr, got, title)
		}
	}
}

func TestDashboardIncludesVPPLCPPanels(t *testing.T) {
	dash := loadDashboard(t)
	want := map[string]string{
		"arca_router_vpp_lcp_pairs":                                   "VPP LCP Pairs",
		"arca_router_vpp_lcp_inconsistencies":                         "VPP LCP Inconsistencies",
		"arca_router_vpp_lcp_reconcile_error":                         "VPP LCP Errors",
		"arca_router_vpp_lcp_last_reconcile_timestamp_seconds * 1000": "VPP LCP Last Check",
	}
	expressions := panelExpressions(dash.Panels)
	for expr, title := range want {
		if got := expressions[expr]; got != title {
			t.Fatalf("expression %q is on panel %q, want %q", expr, got, title)
		}
	}
}

func TestDashboardIncludesQoSCapabilityPanels(t *testing.T) {
	dash := loadDashboard(t)
	want := map[string]string{
		"arca_router_class_of_service_metadata_binding_supported":                     "CoS Metadata Binding",
		"arca_router_class_of_service_queue_scheduler_supported":                      "CoS Scheduler Support",
		"arca_router_class_of_service_policer_supported":                              "CoS Policer Support",
		"arca_router_class_of_service_counters_supported":                             "CoS Counter Support",
		"arca_router_class_of_service_capability_error":                               "CoS Capability Errors",
		"arca_router_class_of_service_capability_last_check_timestamp_seconds * 1000": "CoS Capability Last Check",
	}
	expressions := panelExpressions(dash.Panels)
	for expr, title := range want {
		if got := expressions[expr]; got != title {
			t.Fatalf("expression %q is on panel %q, want %q", expr, got, title)
		}
	}
}

func loadDashboard(t *testing.T) dashboard {
	t.Helper()
	data, err := os.ReadFile("arca-routerd-dashboard.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var dash dashboard
	if err := json.Unmarshal(data, &dash); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return dash
}

func hasPrometheusDatasourceVariable(variables []templateVariable) bool {
	for _, variable := range variables {
		if variable.Name == "datasource" && variable.Type == "datasource" && variable.Query == "prometheus" {
			return true
		}
	}
	return false
}

func panelExpressions(panels []panel) map[string]string {
	expressions := make(map[string]string)
	for _, panel := range panels {
		for _, target := range panel.Targets {
			expressions[target.Expr] = panel.Title
		}
	}
	return expressions
}
