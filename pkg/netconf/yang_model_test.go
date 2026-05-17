package netconf

import (
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"strings"
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

func TestEmbeddedYANGMatchesPublicModel(t *testing.T) {
	publicModel, err := os.ReadFile("../../models/arca-router.yang")
	if err != nil {
		t.Fatalf("ReadFile(models/arca-router.yang) error = %v", err)
	}
	if strings.TrimSpace(string(publicModel)) != strings.TrimSpace(arcaRouterYANG) {
		t.Fatal("embedded YANG model differs from models/arca-router.yang")
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
	if err != nil {
		t.Fatalf("GetArcaRouterModel() error = %v", err)
	}
	if module == nil {
		t.Fatal("GetArcaRouterModel() returned nil module")
	}
	if module.Name != "arca-router" {
		t.Fatalf("GetArcaRouterModel() module name = %s, want arca-router", module.Name)
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

	for _, want := range []string{"arca-router", "ietf-interfaces", "ietf-routing"} {
		found := false
		for _, name := range modules {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ListModules() doesn't include %s, got: %v", want, modules)
		}
	}
	if !sort.StringsAreSorted(modules) {
		t.Errorf("ListModules() = %v, want sorted module names", modules)
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
			name:    "valid system leaf path",
			path:    "/system/host-name",
			wantErr: false,
		},
		{
			name:    "valid interfaces path",
			path:    "/interfaces",
			wantErr: false,
		},
		{
			name:    "valid interface list key predicate",
			path:    "/interfaces/interface[name='ge-0/0/0']",
			wantErr: false,
		},
		{
			name:    "valid interface nested leaf path",
			path:    "/interfaces/interface/unit/family/address",
			wantErr: false,
		},
		{
			name:    "valid chassis path",
			path:    "/chassis",
			wantErr: false,
		},
		{
			name:    "valid routing-options path",
			path:    "/routing-options",
			wantErr: false,
		},
		{
			name:    "valid routing-options static route predicate",
			path:    "/routing-options/static/route[prefix='10.0.0.0/24']",
			wantErr: false,
		},
		{
			name:    "valid NETCONF routing XML alias path",
			path:    "/routing/static-routes/route/prefix",
			wantErr: false,
		},
		{
			name:    "valid routing-instances path",
			path:    "/routing-instances",
			wantErr: false,
		},
		{
			name:    "valid protocols path",
			path:    "/protocols",
			wantErr: false,
		},
		{
			name:    "valid BGP neighbor leaf path",
			path:    "/protocols/bgp/group/neighbor/peer-as",
			wantErr: false,
		},
		{
			name:    "valid EVPN VNI leaf path",
			path:    "/protocols/evpn/vni/remote-vtep",
			wantErr: false,
		},
		{
			name:    "valid class-of-service path",
			path:    "/class-of-service",
			wantErr: false,
		},
		{
			name:    "valid security path",
			path:    "/security",
			wantErr: false,
		},
		{
			name:    "valid state path",
			path:    "/state",
			wantErr: false,
		},
		{
			name:    "valid route state predicate",
			path:    "/state/routes/route[prefix='192.0.2.0/24']",
			wantErr: false,
		},
		{
			name:    "valid route state multiple predicates",
			path:    "/state/routes/route[prefix='192.0.2.0/24'][protocol='static']",
			wantErr: false,
		},
		{
			name:    "valid BFD peer state path",
			path:    "/state/protocols/bfd/peer/status",
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
			name:    "invalid path - unknown nested element",
			path:    "/system/unknown",
			wantErr: true,
		},
		{
			name:    "invalid path - missing BGP group segment",
			path:    "/protocols/bgp/neighbor",
			wantErr: true,
		},
		{
			name:    "invalid path - unknown operational state leaf",
			path:    "/state/routes/route/unknown",
			wantErr: true,
		},
		{
			name:    "invalid path - unknown predicate key",
			path:    "/interfaces/interface[foo='bar']",
			wantErr: true,
		},
		{
			name:    "invalid path - undeclared namespace prefix",
			path:    "/if:interfaces",
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

func TestYANGValidatorValidateElementPathWithContext(t *testing.T) {
	v, err := NewYANGValidator()
	if err != nil {
		t.Fatalf("NewYANGValidator() error = %v", err)
	}

	tests := []struct {
		name    string
		path    string
		attrs   []xml.Attr
		wantErr bool
	}{
		{
			name: "valid interfaces namespace path",
			path: "/if:interfaces/if:interface[if:name='ge-0/0/0']",
			attrs: []xml.Attr{
				{Name: xml.Name{Space: "xmlns", Local: "if"}, Value: IETFInterfacesNS},
			},
			wantErr: false,
		},
		{
			name: "valid arca namespace path",
			path: "/arca:protocols/arca:bgp/arca:group/arca:neighbor/arca:peer-as",
			attrs: []xml.Attr{
				{Name: xml.Name{Space: "xmlns", Local: "arca"}, Value: ArcaConfigNS},
			},
			wantErr: false,
		},
		{
			name: "invalid namespace mismatch",
			path: "/rt:interfaces",
			attrs: []xml.Attr{
				{Name: xml.Name{Space: "xmlns", Local: "rt"}, Value: IETFRoutingNS},
			},
			wantErr: true,
		},
		{
			name: "invalid predicate namespace mismatch",
			path: "/if:interfaces/if:interface[rt:name='ge-0/0/0']",
			attrs: []xml.Attr{
				{Name: xml.Name{Space: "xmlns", Local: "if"}, Value: IETFInterfacesNS},
				{Name: xml.Name{Space: "xmlns", Local: "rt"}, Value: IETFRoutingNS},
			},
			wantErr: true,
		},
		{
			name:    "invalid undeclared namespace prefix",
			path:    "/if:interfaces",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateElementPathWithContext(tt.path, tt.attrs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateElementPathWithContext() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestYANGValidator_ValidateConfig(t *testing.T) {
	v, err := NewYANGValidator()
	if err != nil {
		t.Fatalf("NewYANGValidator() error = %v", err)
	}

	tests := []struct {
		name    string
		xmlData []byte
		wantErr bool
	}{
		{
			name:    "valid system config",
			xmlData: []byte(`<config><system><host-name>test</host-name></system></config>`),
			wantErr: false,
		},
		{
			name:    "unknown element rejected",
			xmlData: []byte(`<config><unknown/></config>`),
			wantErr: true,
		},
		{
			name:    "semantic hostname validation rejected",
			xmlData: []byte(`<config><system><host-name>bad_name</host-name></system></config>`),
			wantErr: true,
		},
		{
			name:    "semantic interface validation rejected",
			xmlData: []byte(`<config><interfaces><interface><name>bad0</name></interface></interfaces></config>`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateConfig(tt.xmlData)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
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
		t.Fatalf("GetModel(arca-router) error = %v", err)
	}
	if module == nil {
		t.Fatal("GetModel(arca-router) returned nil module")
	}
	if module.Name != "arca-router" {
		t.Errorf("GetModel() name = %s, want arca-router", module.Name)
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

func TestYANGValidatorZeroValue(t *testing.T) {
	v := &YANGValidator{}

	if modules := v.ListModules(); modules != nil {
		t.Errorf("ListModules() on zero value = %v, want nil", modules)
	}

	if err := v.ValidateConfig([]byte("<config/>")); err == nil {
		t.Error("ValidateConfig() on zero value should return error")
	}

	if err := v.ValidateElementPath("/system"); err == nil {
		t.Error("ValidateElementPath() on zero value should return error")
	}

	if _, err := v.GetModel("arca-router"); err == nil {
		t.Error("GetModel() on zero value should return error")
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
