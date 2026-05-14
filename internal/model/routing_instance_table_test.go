package model

import (
	"strings"
	"testing"
)

func TestRoutingInstanceTablePlansUsesRouteDistinguisherNumber(t *testing.T) {
	plans, err := RoutingInstanceTablePlans(map[string]*RoutingInstance{
		"BLUE": {
			InstanceType:       "vrf",
			RouteDistinguisher: "65000:100",
			Interfaces:         []string{"ge-0/0/1", "ge-0/0/0", "ge-0/0/0"},
		},
	})
	if err != nil {
		t.Fatalf("RoutingInstanceTablePlans() error = %v", err)
	}
	plan := plans["BLUE"]
	if plan.TableID != 100 {
		t.Fatalf("TableID = %d, want 100", plan.TableID)
	}
	if got := strings.Join(plan.Interfaces, ","); got != "ge-0/0/0,ge-0/0/1" {
		t.Fatalf("Interfaces = %q, want sorted unique list", got)
	}
}

func TestRoutingInstanceTablePlansDerivesStableTableID(t *testing.T) {
	plans, err := RoutingInstanceTablePlans(map[string]*RoutingInstance{
		"BLUE": {InstanceType: "vrf"},
	})
	if err != nil {
		t.Fatalf("RoutingInstanceTablePlans() error = %v", err)
	}
	tableID := plans["BLUE"].TableID
	if tableID < 100000 || tableID > 999999 {
		t.Fatalf("TableID = %d, want derived non-zero six-digit range", tableID)
	}

	again, err := RoutingInstanceTablePlans(map[string]*RoutingInstance{
		"BLUE": {InstanceType: "vrf"},
	})
	if err != nil {
		t.Fatalf("RoutingInstanceTablePlans() second error = %v", err)
	}
	if again["BLUE"].TableID != tableID {
		t.Fatalf("TableID changed from %d to %d", tableID, again["BLUE"].TableID)
	}
}

func TestRoutingInstanceTablePlansRejectsExplicitCollision(t *testing.T) {
	_, err := RoutingInstanceTablePlans(map[string]*RoutingInstance{
		"BLUE": {RouteDistinguisher: "65000:100"},
		"RED":  {RouteDistinguisher: "65000:100"},
	})
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("RoutingInstanceTablePlans() error = %v, want collision", err)
	}
}
