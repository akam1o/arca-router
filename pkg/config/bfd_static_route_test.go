package config

import (
	"strings"
	"testing"
)

func TestBFDStaticRouteParseValidateAndSerialize(t *testing.T) {
	input := strings.Join([]string{
		"set protocols bfd profile fast receive-interval 150",
		"set routing-options static route 203.0.113.0/24 next-hop 192.0.2.2 bfd source 192.0.2.1 profile fast multi-hop",
	}, "\n")

	cfg, err := NewParser(strings.NewReader(input)).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(cfg.RoutingOptions.StaticRoutes) != 1 {
		t.Fatalf("StaticRoutes = %d, want 1", len(cfg.RoutingOptions.StaticRoutes))
	}
	route := cfg.RoutingOptions.StaticRoutes[0]
	if !route.BFD || route.BFDProfile != "fast" || route.BFDSource != "192.0.2.1" || !route.BFDMultihop {
		t.Fatalf("Static route BFD = %#v, want source/profile/multihop", route)
	}

	got := ToSetCommands(cfg)
	want := "set routing-options static route 203.0.113.0/24 next-hop 192.0.2.2 bfd multi-hop source 192.0.2.1 profile fast\n"
	if !strings.Contains(got, want) {
		t.Fatalf("ToSetCommands() missing %q:\n%s", want, got)
	}
}

func TestValidateBFDStaticRouteRejectsUnknownProfile(t *testing.T) {
	cfg := NewConfig()
	cfg.RoutingOptions = &RoutingOptions{StaticRoutes: []*StaticRoute{
		{Prefix: "203.0.113.0/24", NextHop: "192.0.2.2", BFD: true, BFDProfile: "missing"},
	}}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Validate() error = %v, want missing BFD profile error", err)
	}
}

func TestValidateBFDStaticRouteRejectsDistance(t *testing.T) {
	cfg := NewConfig()
	cfg.RoutingOptions = &RoutingOptions{StaticRoutes: []*StaticRoute{
		{Prefix: "203.0.113.0/24", NextHop: "192.0.2.2", Distance: 10, BFD: true},
	}}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "distance") {
		t.Fatalf("Validate() error = %v, want BFD distance error", err)
	}
}
