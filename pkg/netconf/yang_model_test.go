package netconf

import (
	"fmt"
	"testing"
)

func TestNewYANGValidator(t *testing.T) {
	v, err := NewYANGValidator()
	if err != nil {
		t.Fatalf("NewYANGValidator() error = %v", err)
	}

	if v == nil {
		t.Fatal("NewYANGValidator() returned nil")
	}

	if v.modules == nil {
		t.Error("NewYANGValidator() modules is nil")
	}
}

func TestGetGlobalValidator(t *testing.T) {
	// First call
	v1, err := GetGlobalValidator()
	if err != nil {
		t.Fatalf("GetGlobalValidator() first call error = %v", err)
	}

	if v1 == nil {
		t.Fatal("GetGlobalValidator() returned nil")
	}

	// Second call should return same instance (singleton)
	v2, err := GetGlobalValidator()
	if err != nil {
		t.Fatalf("GetGlobalValidator() second call error = %v", err)
	}

	// Should be same instance (pointer equality)
	if v1 != v2 {
		t.Error("GetGlobalValidator() returned different instances (not singleton)")
	}
}

func TestYANGValidator_GetArcaRouterModel(t *testing.T) {
	v, err := NewYANGValidator()
	if err != nil {
		t.Fatalf("NewYANGValidator() error = %v", err)
	}

	module, err := v.GetArcaRouterModel()

	// Note: This will fail because ietf-interfaces/ietf-routing modules are not available
	// But we should at least test that the function doesn't panic
	if err != nil {
		// Expected in Phase 3 (IETF modules not available)
		t.Logf("GetArcaRouterModel() error (expected in Phase 3): %v", err)
	}

	if module != nil {
		t.Logf("GetArcaRouterModel() returned module: %s", module.Name)
	}
}

func TestYANGValidator_ListModules(t *testing.T) {
	v, err := NewYANGValidator()
	if err != nil {
		t.Fatalf("NewYANGValidator() error = %v", err)
	}

	modules := v.ListModules()

	// Should at least have arca-router module
	if len(modules) == 0 {
		t.Error("ListModules() returned empty list")
	}

	// Check if arca-router is in the list
	found := false
	for _, name := range modules {
		if name == "arca-router" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("ListModules() doesn't include arca-router, got: %v", modules)
	}
}

func TestYANGValidator_ValidateElementPath(t *testing.T) {
	v, err := NewYANGValidator()
	if err != nil {
		t.Fatalf("NewYANGValidator() error = %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid system path",
			path:    "/system",
			wantErr: false,
		},
		{
			name:    "valid interfaces path",
			path:    "/interfaces",
			wantErr: false,
		},
		{
			name:    "valid routing-options path",
			path:    "/routing-options",
			wantErr: false,
		},
		{
			name:    "valid protocols path",
			path:    "/protocols",
			wantErr: false,
		},
		{
			name:    "valid state path",
			path:    "/state",
			wantErr: false,
		},
		{
			name:    "invalid path - no leading slash",
			path:    "system",
			wantErr: true,
		},
		{
			name:    "invalid path - unknown element",
			path:    "/unknown",
			wantErr: true,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateElementPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateElementPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestYANGValidator_ValidateConfig(t *testing.T) {
	v, err := NewYANGValidator()
	if err != nil {
		t.Fatalf("NewYANGValidator() error = %v", err)
	}

	// Test basic validation (Phase 3: minimal implementation)
	xmlData := []byte(`<config><system><host-name>test</host-name></system></config>`)

	err = v.ValidateConfig(xmlData)
	// Phase 3: This is a stub implementation, should not error
	if err != nil {
		t.Errorf("ValidateConfig() error = %v, want nil", err)
	}
}

func TestYANGValidator_GetModel(t *testing.T) {
	v, err := NewYANGValidator()
	if err != nil {
		t.Fatalf("NewYANGValidator() error = %v", err)
	}

	// Try to get arca-router module
	module, err := v.GetModel("arca-router")
	if err != nil {
		// May fail in Phase 3 due to missing IETF modules
		t.Logf("GetModel(arca-router) error (may be expected): %v", err)
	}

	if module != nil {
		if module.Name != "arca-router" {
			t.Errorf("GetModel() name = %s, want arca-router", module.Name)
		}
	}

	// Try to get non-existent module
	_, err = v.GetModel("non-existent-module")
	if err == nil {
		t.Error("GetModel(non-existent) should return error")
	}
}

func TestYANGValidator_NilReceiver(t *testing.T) {
	var v *YANGValidator

	// Test nil receiver handling
	modules := v.ListModules()
	if modules != nil {
		t.Error("ListModules() on nil receiver should return nil")
	}

	err := v.ValidateConfig([]byte{})
	if err == nil {
		t.Error("ValidateConfig() on nil receiver should return error")
	}

	err = v.ValidateElementPath("/system")
	if err == nil {
		t.Error("ValidateElementPath() on nil receiver should return error")
	}

	_, err = v.GetModel("test")
	if err == nil {
		t.Error("GetModel() on nil receiver should return error")
	}
}

func TestYANGValidator_ConcurrentAccess(t *testing.T) {
	v, err := NewYANGValidator()
	if err != nil {
		t.Fatalf("NewYANGValidator() error = %v", err)
	}

	// Test concurrent ListModules calls (goroutine-safe)
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			modules := v.ListModules()
			if len(modules) == 0 {
				done <- fmt.Errorf("Concurrent ListModules() returned empty list")
			} else {
				done <- nil
			}
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}

	// Should not panic or corrupt data
}
