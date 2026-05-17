package netconf

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

const (
	// NETCONF capabilities
	CapabilityBase10     = "urn:ietf:params:netconf:base:1.0"
	CapabilityBase11     = "urn:ietf:params:netconf:base:1.1"
	CapabilityCandidate  = "urn:ietf:params:netconf:capability:candidate:1.0"
	CapabilityValidate   = "urn:ietf:params:netconf:capability:validate:1.1"
	CapabilityRollback   = "urn:ietf:params:netconf:capability:rollback-on-error:1.0"
	CapabilityArcaRouter = "urn:arca:router:config:1.0?module=arca-router&revision=2025-12-27"
	// Arca-specific capability for the safe absolute XPath subset accepted by filters.
	CapabilityArcaXPathFilterSubset = "urn:arca:router:netconf:capability:xpath-filter-subset:1.0"

	// NETCONF namespace
	NetconfNamespace = "urn:ietf:params:xml:ns:netconf:base:1.0"
)

// Hello represents a NETCONF <hello> message
type Hello struct {
	XMLName      xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 hello"`
	Capabilities struct {
		Capability []string `xml:"capability"`
	} `xml:"capabilities"`
	SessionID uint32 `xml:"session-id,omitempty"` // RFC 6241: session-id is an integer (uint32)
}

// ServerHello creates a server <hello> message with the given session ID
func ServerHello(sessionID uint32) *Hello {
	hello := &Hello{
		SessionID: sessionID,
	}
	hello.Capabilities.Capability = []string{
		CapabilityBase10,
		CapabilityBase11,
		CapabilityCandidate,
		CapabilityValidate,
		CapabilityRollback,
		CapabilityArcaRouter,
		CapabilityArcaXPathFilterSubset,
	}
	return hello
}

// MarshalHello marshals a Hello message to XML
func MarshalHello(hello *Hello) ([]byte, error) {
	if hello == nil {
		return nil, fmt.Errorf("nil hello")
	}

	data, err := xml.MarshalIndent(hello, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal hello: %w", err)
	}

	// Add XML declaration
	xmlDecl := []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	return append(xmlDecl, data...), nil
}

// UnmarshalHello unmarshals XML data into a Hello message
func UnmarshalHello(data []byte) (*Hello, error) {
	var hello Hello
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	decoder.Entity = nil

	if err := decoder.Decode(&hello); err != nil {
		return nil, fmt.Errorf("unmarshal hello: %w", err)
	}
	if err := ensureNoTrailingXML(decoder, "hello"); err != nil {
		return nil, fmt.Errorf("unmarshal hello: %w", err)
	}

	// Validate namespace and element name
	if hello.XMLName.Space != NetconfNamespace {
		return nil, fmt.Errorf("invalid hello namespace: %q (expected %q)", hello.XMLName.Space, NetconfNamespace)
	}
	if hello.XMLName.Local != "hello" {
		return nil, fmt.Errorf("invalid element name: %q (expected \"hello\")", hello.XMLName.Local)
	}

	return &hello, nil
}

// HasCapability checks if the hello message contains a specific capability
func (h *Hello) HasCapability(capability string) bool {
	if h == nil {
		return false
	}
	want := strings.TrimSpace(capability)
	for _, cap := range h.Capabilities.Capability {
		if strings.TrimSpace(cap) == want {
			return true
		}
	}
	return false
}

// NegotiateBaseVersion determines the NETCONF base version to use based on client capabilities
// Returns "1.1" if client supports base:1.1, otherwise "1.0"
func NegotiateBaseVersion(clientHello *Hello) string {
	if clientHello.hasBaseCapability(CapabilityBase11) {
		return "1.1"
	}
	return "1.0"
}

// ValidateClientHello validates a client <hello> message
func ValidateClientHello(clientHello *Hello) error {
	if clientHello == nil {
		return fmt.Errorf("nil hello")
	}
	if len(clientHello.Capabilities.Capability) == 0 {
		return fmt.Errorf("client hello must include capabilities")
	}
	if !clientHello.hasBaseCapability(CapabilityBase10) && !clientHello.hasBaseCapability(CapabilityBase11) {
		return fmt.Errorf("client must support base:1.0 or base:1.1")
	}

	// Client hello must not include session-id
	if clientHello.SessionID != 0 {
		return fmt.Errorf("client hello must not include session-id")
	}

	return nil
}

// GetClientCapabilities returns a human-readable list of client capabilities
func GetClientCapabilities(clientHello *Hello) []string {
	if clientHello == nil {
		return nil
	}
	capabilities := make([]string, 0, len(clientHello.Capabilities.Capability))
	for _, cap := range clientHello.Capabilities.Capability {
		// Extract short name for common capabilities
		capabilities = append(capabilities, shortCapabilityName(cap))
	}
	return capabilities
}

func (h *Hello) hasBaseCapability(capability string) bool {
	if h == nil {
		return false
	}
	for _, cap := range h.Capabilities.Capability {
		if capabilityBasePart(cap) == capability {
			return true
		}
	}
	return false
}

func capabilityBasePart(capability string) string {
	capability = strings.TrimSpace(capability)
	if idx := strings.Index(capability, "?"); idx != -1 {
		return capability[:idx]
	}
	return capability
}

func shortCapabilityName(capability string) string {
	capability = strings.TrimSpace(capability)
	base := capabilityBasePart(capability)
	const prefix = "urn:ietf:params:netconf:"
	if !strings.HasPrefix(base, prefix) {
		return capability
	}
	name := strings.TrimPrefix(base, prefix)
	return strings.TrimPrefix(name, "capability:")
}
