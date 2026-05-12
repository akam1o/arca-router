package model

import "testing"

func TestValidateAllowsLegacyInterfaceNames(t *testing.T) {
	for _, name := range []string{"ge-0/0/0", "xe-1/2/3", "et-4/5/6", "ae0", "lo0", "irb", "fxp0"} {
		t.Run(name, func(t *testing.T) {
			cfg := NewRouterConfig()
			cfg.Interfaces[name] = &InterfaceConfig{}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}
