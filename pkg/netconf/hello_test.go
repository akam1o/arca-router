package netconf

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestServerHello(t *testing.T) {
	hello := ServerHello(12345)

	if hello.SessionID != 12345 {
		t.Errorf("SessionID = %d, want 12345", hello.SessionID)
	}

	// Verify required capabilities
	requiredCaps := []string{
		CapabilityBase10,
		CapabilityBase11,
		CapabilityCandidate,
		CapabilityValidate,
		CapabilityArcaRouter,
	}

	for _, cap := range requiredCaps {
		if !hello.HasCapability(cap) {
			t.Errorf("Missing required capability: %s", cap)
		}
	}
}

func TestMarshalHello(t *testing.T) {
	hello := ServerHello(12345)
	data, err := MarshalHello(hello)
	if err != nil {
		t.Fatalf("MarshalHello failed: %v", err)
	}

	// Verify XML declaration
	if !strings.HasPrefix(string(data), `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("Missing XML declaration")
	}

	// Verify it can be unmarshaled
	unmarshaled, err := UnmarshalHello(data)
	if err != nil {
		t.Fatalf("UnmarshalHello failed: %v", err)
	}

	if unmarshaled.SessionID != hello.SessionID {
		t.Errorf("SessionID mismatch: got %d, want %d", unmarshaled.SessionID, hello.SessionID)
	}

	if len(unmarshaled.Capabilities.Capability) != len(hello.Capabilities.Capability) {
		t.Errorf("Capability count mismatch: got %d, want %d",
			len(unmarshaled.Capabilities.Capability), len(hello.Capabilities.Capability))
	}
}

func TestUnmarshalClientHello(t *testing.T) {
	clientHelloXML := `<?xml version="1.0" encoding="UTF-8"?>
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:xml:ns:netconf:base:1.0</capability>
    <capability>urn:ietf:params:xml:ns:netconf:base:1.1</capability>
  </capabilities>
</hello>`

	hello, err := UnmarshalHello([]byte(clientHelloXML))
	if err != nil {
		t.Fatalf("UnmarshalHello failed: %v", err)
	}

	if !hello.HasCapability(CapabilityBase10) {
		t.Errorf("Missing base:1.0 capability")
	}

	if !hello.HasCapability(CapabilityBase11) {
		t.Errorf("Missing base:1.1 capability")
	}

	if hello.SessionID != 0 {
		t.Errorf("Client hello should not have session-id, got %d", hello.SessionID)
	}
}

func TestNegotiateBaseVersion(t *testing.T) {
	tests := []struct {
		name         string
		capabilities []string
		want         string
	}{
		{
			name:         "both versions",
			capabilities: []string{CapabilityBase10, CapabilityBase11},
			want:         "1.1",
		},
		{
			name:         "only 1.1",
			capabilities: []string{CapabilityBase11},
			want:         "1.1",
		},
		{
			name:         "only 1.0",
			capabilities: []string{CapabilityBase10},
			want:         "1.0",
		},
		{
			name:         "neither (invalid but test fallback)",
			capabilities: []string{"other:capability"},
			want:         "1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hello := &Hello{}
			hello.Capabilities.Capability = tt.capabilities

			got := NegotiateBaseVersion(hello)
			if got != tt.want {
				t.Errorf("NegotiateBaseVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateClientHello(t *testing.T) {
	tests := []struct {
		name      string
		hello     *Hello
		wantError bool
	}{
		{
			name: "valid hello with base:1.0 only",
			hello: &Hello{
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{CapabilityBase10},
				},
			},
			wantError: false,
		},
		{
			name: "valid hello with base:1.0 and base:1.1",
			hello: &Hello{
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{CapabilityBase10, CapabilityBase11},
				},
			},
			wantError: false,
		},
		{
			name: "invalid hello with only base:1.1 (RFC violation)",
			hello: &Hello{
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{CapabilityBase11},
				},
			},
			wantError: true,
		},
		{
			name: "invalid - no base capability",
			hello: &Hello{
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{"other:capability"},
				},
			},
			wantError: true,
		},
		{
			name: "invalid - has session-id",
			hello: &Hello{
				SessionID: 123,
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{CapabilityBase10},
				},
			},
			wantError: true,
		},
		{
			name: "invalid - no capabilities",
			hello: &Hello{
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClientHello(tt.hello)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateClientHello() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestHasCapability(t *testing.T) {
	hello := &Hello{}
	hello.Capabilities.Capability = []string{
		CapabilityBase10,
		CapabilityBase11,
		CapabilityCandidate,
	}

	tests := []struct {
		name       string
		capability string
		want       bool
	}{
		{
			name:       "has base:1.0",
			capability: CapabilityBase10,
			want:       true,
		},
		{
			name:       "has base:1.1",
			capability: CapabilityBase11,
			want:       true,
		},
		{
			name:       "has candidate",
			capability: CapabilityCandidate,
			want:       true,
		},
		{
			name:       "does not have validate",
			capability: CapabilityValidate,
			want:       false,
		},
		{
			name:       "does not have unknown",
			capability: "unknown:capability",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hello.HasCapability(tt.capability)
			if got != tt.want {
				t.Errorf("HasCapability(%q) = %v, want %v", tt.capability, got, tt.want)
			}
		})
	}
}

func TestGetClientCapabilities(t *testing.T) {
	hello := &Hello{}
	hello.Capabilities.Capability = []string{
		CapabilityBase10,
		CapabilityBase11,
		"custom:capability",
	}

	caps := GetClientCapabilities(hello)
	if len(caps) != 3 {
		t.Errorf("GetClientCapabilities() returned %d capabilities, want 3", len(caps))
	}

	// Should contain short names
	foundBase10 := false
	for _, cap := range caps {
		if cap == "1.0" || strings.Contains(cap, "base:1.0") {
			foundBase10 = true
			break
		}
	}
	if !foundBase10 {
		t.Errorf("GetClientCapabilities() did not contain base:1.0: %v", caps)
	}
}

func TestHelloXMLNamespace(t *testing.T) {
	hello := ServerHello(12345)
	data, err := MarshalHello(hello)
	if err != nil {
		t.Fatalf("MarshalHello failed: %v", err)
	}

	// Verify namespace is present
	if !strings.Contains(string(data), NetconfNamespace) {
		t.Errorf("Hello XML missing namespace: %s", NetconfNamespace)
	}

	// Verify it can be unmarshaled with namespace validation
	var parsed Hello
	if err := xml.Unmarshal(data, &parsed); err != nil {
		t.Errorf("Failed to unmarshal hello with namespace: %v", err)
	}
}

func TestUnmarshalHelloWrongNamespace(t *testing.T) {
	// Hello with wrong namespace
	wrongNamespaceXML := `<?xml version="1.0" encoding="UTF-8"?>
<hello xmlns="http://wrong.namespace.com">
  <capabilities>
    <capability>urn:ietf:params:xml:ns:netconf:base:1.0</capability>
  </capabilities>
</hello>`

	_, err := UnmarshalHello([]byte(wrongNamespaceXML))
	if err == nil {
		t.Errorf("Expected error for wrong namespace, but got nil")
	}
	// xml.Unmarshal returns error before our validation, so just check it failed
	if !strings.Contains(err.Error(), "namespace") && !strings.Contains(err.Error(), "name space") {
		t.Errorf("Expected namespace-related error, got: %v", err)
	}
}

func TestUnmarshalHelloWrongElementName(t *testing.T) {
	// Wrong element name (not "hello")
	wrongElementXML := `<?xml version="1.0" encoding="UTF-8"?>
<goodbye xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:xml:ns:netconf:base:1.0</capability>
  </capabilities>
</goodbye>`

	_, err := UnmarshalHello([]byte(wrongElementXML))
	if err == nil {
		t.Errorf("Expected error for wrong element name, but got nil")
	}
	// xml.Unmarshal returns error before our validation, so just check it failed
	if !strings.Contains(err.Error(), "element") {
		t.Errorf("Expected element-related error, got: %v", err)
	}
}

func TestUnmarshalHelloMalformedXML(t *testing.T) {
	malformedXML := `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">`

	_, err := UnmarshalHello([]byte(malformedXML))
	if err == nil {
		t.Errorf("Expected error for malformed XML, but got nil")
	}
}
