package netconf

import (
	"bytes"
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
		CapabilityRollback,
		CapabilityArcaRouter,
		CapabilityArcaXPathFilterSubset,
		CapabilityXPath,
	}

	for _, cap := range requiredCaps {
		if !hello.HasCapability(cap) {
			t.Errorf("Missing required capability: %s", cap)
		}
	}
	if !strings.Contains(CapabilityArcaRouter, "revision=2025-12-27") {
		t.Errorf("CapabilityArcaRouter = %q, want current YANG revision", CapabilityArcaRouter)
	}
}

func TestServerHelloDoesNotAdvertiseUnsupportedCapabilities(t *testing.T) {
	hello := ServerHello(12345)

	unsupportedCaps := []string{
		"urn:ietf:params:xml:ns:netconf:base:1.0",
		"urn:ietf:params:xml:ns:netconf:base:1.1",
		"urn:ietf:params:xml:ns:netconf:capability:candidate:1.0",
		"urn:ietf:params:netconf:capability:startup:1.0",
		"urn:ietf:params:netconf:capability:writable-running:1.0",
	}

	for _, cap := range unsupportedCaps {
		if hello.HasCapability(cap) {
			t.Errorf("ServerHello() advertised unsupported capability %q", cap)
		}
	}
}

func TestServerHelloCanSuppressStandardXPath(t *testing.T) {
	hello := ServerHelloWithOptions(12345, HelloOptions{DisableStandardXPath: true})

	if hello.HasCapability(CapabilityXPath) {
		t.Fatalf("ServerHelloWithOptions() advertised %q", CapabilityXPath)
	}
	if hello.HasCapability("urn:ietf:params:netconf:capability:startup:1.0") {
		t.Fatal("ServerHelloWithOptions() advertised startup capability")
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

func TestMarshalHelloNil(t *testing.T) {
	data, err := MarshalHello(nil)
	if err == nil {
		t.Fatal("MarshalHello(nil) error = nil, want nil hello error")
	}
	if data != nil {
		t.Fatalf("MarshalHello(nil) data = %q, want nil", string(data))
	}
	if !strings.Contains(err.Error(), "nil hello") {
		t.Fatalf("MarshalHello(nil) error = %v, want nil hello", err)
	}
}

func TestMarshalHelloRejectsOversizedXML(t *testing.T) {
	hello := &Hello{}
	hello.Capabilities.Capability = []string{strings.Repeat("x", MaxXMLSize)}

	data, err := MarshalHello(hello)
	if err == nil {
		t.Fatal("MarshalHello() error = nil, want size limit error")
	}
	if data != nil {
		t.Fatalf("MarshalHello() data length = %d, want nil", len(data))
	}
	if !strings.Contains(err.Error(), "XML size exceeds maximum") {
		t.Fatalf("MarshalHello() error = %v, want size limit error", err)
	}
}

func TestUnmarshalClientHello(t *testing.T) {
	clientHelloXML := `<?xml version="1.0" encoding="UTF-8"?>
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
    <capability>urn:ietf:params:netconf:base:1.1</capability>
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
			name:         "only 1.1 with parameters",
			capabilities: []string{CapabilityBase11 + "?foo=bar"},
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
		{
			name:         "base 1.1 trims whitespace",
			capabilities: []string{"\n " + CapabilityBase11 + "\t"},
			want:         "1.1",
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

	if got := NegotiateBaseVersion(nil); got != "1.0" {
		t.Errorf("NegotiateBaseVersion(nil) = %q, want 1.0", got)
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
			name: "valid hello with base:1.1 only",
			hello: &Hello{
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{CapabilityBase11},
				},
			},
			wantError: false,
		},
		{
			name: "valid hello with base capability parameters",
			hello: &Hello{
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{CapabilityBase11 + "?foo=bar"},
				},
			},
			wantError: false,
		},
		{
			name: "valid hello trims base capability",
			hello: &Hello{
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{"\n " + CapabilityBase10 + "\t"},
				},
			},
			wantError: false,
		},
		{
			name:      "invalid - nil hello",
			hello:     nil,
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

func TestValidateClientHelloReportsSpecificDirectInputErrors(t *testing.T) {
	tests := []struct {
		name    string
		hello   *Hello
		wantErr string
	}{
		{
			name:    "nil hello",
			hello:   nil,
			wantErr: "nil hello",
		},
		{
			name:    "empty capabilities",
			hello:   &Hello{},
			wantErr: "client hello must include capabilities",
		},
		{
			name: "missing base capability",
			hello: &Hello{
				Capabilities: struct {
					Capability []string `xml:"capability"`
				}{
					Capability: []string{"custom:capability"},
				},
			},
			wantErr: "client must support base:1.0 or base:1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClientHello(tt.hello)
			if err == nil {
				t.Fatal("ValidateClientHello() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateClientHello() error = %v, want %q", err, tt.wantErr)
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
		{
			name:       "trims capability value",
			capability: " " + CapabilityCandidate + "\t",
			want:       true,
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

	var nilHello *Hello
	if nilHello.HasCapability(CapabilityBase10) {
		t.Error("HasCapability() on nil receiver = true, want false")
	}
}

func TestGetClientCapabilities(t *testing.T) {
	hello := &Hello{}
	hello.Capabilities.Capability = []string{
		CapabilityBase10,
		CapabilityBase11,
		CapabilityCandidate,
		" custom:capability ",
	}

	caps := GetClientCapabilities(hello)
	if len(caps) != 4 {
		t.Errorf("GetClientCapabilities() returned %d capabilities, want 4", len(caps))
	}

	wantCaps := []string{"base:1.0", "base:1.1", "candidate:1.0", "custom:capability"}
	for _, want := range wantCaps {
		if !containsString(caps, want) {
			t.Errorf("GetClientCapabilities() = %v, missing %q", caps, want)
		}
	}

	if caps := GetClientCapabilities(nil); caps != nil {
		t.Errorf("GetClientCapabilities(nil) = %v, want nil", caps)
	}
}

func containsString(items []string, want string) bool {
	for _, cap := range items {
		if cap == want {
			return true
		}
	}
	return false
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
    <capability>urn:ietf:params:netconf:base:1.0</capability>
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
    <capability>urn:ietf:params:netconf:base:1.0</capability>
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

func TestUnmarshalHelloRejectsOversizedXML(t *testing.T) {
	_, err := UnmarshalHello(bytes.Repeat([]byte("x"), MaxXMLSize+1))
	if err == nil {
		t.Fatal("UnmarshalHello() error = nil, want size limit error")
	}
	if !strings.Contains(err.Error(), "XML size exceeds maximum") {
		t.Fatalf("UnmarshalHello() error = %v, want size limit error", err)
	}
}

func TestUnmarshalHelloRejectsTrailingContent(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{
			name: "trailing hello",
			xml: `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
  </capabilities>
</hello><hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
  </capabilities>
</hello>`,
		},
		{
			name: "trailing text",
			xml: `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
  </capabilities>
</hello>junk`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalHello([]byte(tt.xml))
			if err == nil {
				t.Fatal("UnmarshalHello() error = nil, want trailing content error")
			}
			if !strings.Contains(err.Error(), "trailing content after hello element") {
				t.Fatalf("UnmarshalHello() error = %v, want trailing hello content error", err)
			}
		})
	}
}

func TestUnmarshalHelloRejectsUnsafeDirectives(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{
			name: "doctype",
			xml: `<!DOCTYPE hello SYSTEM "evil.dtd">
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
  </capabilities>
</hello>`,
		},
		{
			name: "entity",
			xml: `<!ENTITY xxe SYSTEM "file:///etc/passwd">
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
  </capabilities>
</hello>`,
		},
		{
			name: "lowercase doctype",
			xml: `<!doctype hello SYSTEM "evil.dtd">
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
  </capabilities>
</hello>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalHello([]byte(tt.xml))
			if err == nil {
				t.Fatal("UnmarshalHello() error = nil, want unsafe directive error")
			}
			if !strings.Contains(err.Error(), "DTD and ENTITY declarations are not allowed") {
				t.Fatalf("UnmarshalHello() error = %v, want unsafe directive error", err)
			}
		})
	}
}
