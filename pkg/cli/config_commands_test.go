package cli

import (
	"context"
	"testing"
)

func TestGetConfigPath(t *testing.T) {
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Initial path should be empty
	if path := session.GetConfigPath(); path != "" {
		t.Errorf("Initial config path should be empty, got %q", path)
	}

	// After setting path
	session.EditHierarchy([]string{"interfaces", "ge-0/0/0"})
	path := session.GetConfigPath()
	if path != "interfaces ge-0/0/0" {
		t.Errorf("Config path should be 'interfaces ge-0/0/0', got %q", path)
	}

	// After clearing path
	session.TopHierarchy()
	if path := session.GetConfigPath(); path != "" {
		t.Errorf("After top, config path should be empty, got %q", path)
	}
}

func TestShowConfigCommand(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// ShowConfigCommand should return candidate config
	config, err := session.ShowConfigCommand(ctx)
	if err != nil {
		t.Errorf("ShowConfigCommand() error = %v", err)
	}

	if config == "" {
		t.Error("ShowConfigCommand() should return non-empty config")
	}
}

func TestShowConfigCommandInOperationalMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// ShowConfigCommand in operational mode should return running config
	config, err := session.ShowConfigCommand(ctx)
	if err != nil {
		t.Errorf("ShowConfigCommand() error = %v", err)
	}

	if config == "" {
		t.Error("ShowConfigCommand() should return non-empty config")
	}
}

func TestConfigPathString(t *testing.T) {
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	tests := []struct {
		name string
		path []string
		want string
	}{
		{
			name: "empty path",
			path: []string{},
			want: "",
		},
		{
			name: "single level",
			path: []string{"system"},
			want: "system",
		},
		{
			name: "multi level",
			path: []string{"interfaces", "ge-0/0/0", "unit", "0"},
			want: "interfaces ge-0/0/0 unit 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session.EditHierarchy(tt.path)
			if got := session.GetConfigPath(); got != tt.want {
				t.Errorf("GetConfigPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetCommandWithPath(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Set hierarchy context
	session.EditHierarchy([]string{"interfaces", "ge-0/0/0"})

	// Test SetCommandWithPath
	err := session.SetCommandWithPath(ctx, []string{"unit", "0", "family", "inet"})
	if err != nil {
		t.Errorf("SetCommandWithPath() error = %v", err)
	}
}

func TestDeleteCommandWithPath(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Set hierarchy context
	session.EditHierarchy([]string{"system"})

	// Test DeleteCommandWithPath on existing config from mock
	// Mock returns "set system host-name test-router"
	err := session.DeleteCommandWithPath(ctx, []string{"host-name"})
	if err != nil {
		t.Errorf("DeleteCommandWithPath() error = %v", err)
	}
}
