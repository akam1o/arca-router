package model

import "testing"

func TestClassOfServiceValidationRejectsInvalidQueue(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.ClassOfService = &ClassOfServiceConfig{
		ForwardingClasses: map[string]*ForwardingClass{
			"bad": {Queue: 9},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid queue error")
	}
}
