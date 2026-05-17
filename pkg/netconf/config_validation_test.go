package netconf

import (
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestValidateConfigSemanticsErrorPath(t *testing.T) {
	cfg := &config.Config{
		System:     &config.SystemConfig{HostName: "bad_name"},
		Interfaces: map[string]*config.Interface{},
	}

	tests := []struct {
		name    string
		rpcName string
		want    string
	}{
		{name: "commit", rpcName: "commit", want: "/rpc/commit"},
		{name: "edit config", rpcName: "edit-config", want: "/rpc/edit-config/config"},
		{name: "validate", rpcName: "validate", want: "/rpc/validate/source"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigSemantics(tt.rpcName, cfg)
			if err == nil {
				t.Fatal("validateConfigSemantics() error = nil, want validation error")
			}
			if err.ErrorPath != tt.want {
				t.Fatalf("validateConfigSemantics() path = %q, want %q", err.ErrorPath, tt.want)
			}
		})
	}
}
