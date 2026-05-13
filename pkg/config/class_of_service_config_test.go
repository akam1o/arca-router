package config

import (
	"strings"
	"testing"
)

func TestClassOfServiceConfigRoundTrip(t *testing.T) {
	cfg := parseSetCommands(t,
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set class-of-service forwarding-class expedited-forwarding queue 5",
		"set class-of-service traffic-control-profile WAN shaping-rate 1000000000",
		"set class-of-service traffic-control-profile WAN scheduler-map WAN-SCHED",
		"set class-of-service interfaces ge-0/0/0 output-traffic-control-profile WAN",
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if got := cfg.ClassOfService.ForwardingClasses["expedited-forwarding"].Queue; got != 5 {
		t.Fatalf("forwarding-class queue = %d", got)
	}
	if got := cfg.ClassOfService.TrafficControlProfiles["WAN"].ShapingRate; got != 1000000000 {
		t.Fatalf("shaping-rate = %d", got)
	}
	if got := cfg.ClassOfService.Interfaces["ge-0/0/0"].OutputTrafficControlProfile; got != "WAN" {
		t.Fatalf("CoS interface profile = %q", got)
	}
	assertSetCommandRoundTrip(t, cfg)
}

func TestClassOfServiceValidationRejectsMissingTrafficControlProfile(t *testing.T) {
	cfg := NewConfig()
	cfg.ClassOfService = &ClassOfServiceConfig{
		Interfaces: map[string]*CoSInterface{
			"ge-0/0/0": {
				Name:                        "ge-0/0/0",
				OutputTrafficControlProfile: "missing",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing traffic-control-profile error")
	}
}

func TestClassOfServiceValidationRejectsUnknownInterfaceReference(t *testing.T) {
	cfg := NewConfig()
	cfg.ClassOfService = &ClassOfServiceConfig{
		TrafficControlProfiles: map[string]*TrafficControlProfile{
			"WAN": {Name: "WAN"},
		},
		Interfaces: map[string]*CoSInterface{
			"ge-0/0/0": {
				Name:                        "ge-0/0/0",
				OutputTrafficControlProfile: "WAN",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want unknown interface reference error")
	}
	if want := "Class-of-service references non-existent interface ge-0/0/0"; !strings.Contains(err.Error(), want) {
		t.Fatalf("Validate() error = %v, want substring %q", err, want)
	}
}
