package netconf

import (
	"encoding/xml"
	"fmt"
	"strings"
)

const (
	// NETCONF capabilities
	CapabilityBase10        = "urn:ietf:params:xml:ns:netconf:base:1.0"
	CapabilityBase11        = "urn:ietf:params:xml:ns:netconf:base:1.1"
	CapabilityCandidate     = "urn:ietf:params:xml:ns:netconf:capability:candidate:1.0"
	CapabilityValidate      = "urn:ietf:params:xml:ns:netconf:capability:validate:1.1"
	CapabilityArcaRouter    = "http://github.com/akam1o/arca-router?module=arca-router&revision=2025-12-26"

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
		CapabilityArcaRouter,
	}
	return hello
}

// MarshalHello marshals a Hello message to XML
func MarshalHello(hello *Hello) ([]byte, error) {
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
	if err := xml.Unmarshal(data, &hello); err != nil {
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
	for _, cap := range h.Capabilities.Capability {
		if cap == capability {
			return true
		}
	}
	return false
}

// NegotiateBaseVersion determines the NETCONF base version to use based on client capabilities
// Returns "1.1" if client supports base:1.1, otherwise "1.0"
func NegotiateBaseVersion(clientHello *Hello) string {
	if clientHello.HasCapability(CapabilityBase11) {
		return "1.1"
	}
	return "1.0"
}

// ValidateClientHello validates a client <hello> message
func ValidateClientHello(clientHello *Hello) error {
	// RFC 6241: Client must support base:1.0 (required capability)
	// base:1.1 is optional and indicates preference for chunked framing
	if !clientHello.HasCapability(CapabilityBase10) {
		return fmt.Errorf("client must support base:1.0 (RFC 6241 required capability)")
	}

	// Client hello must not include session-id
	if clientHello.SessionID != 0 {
		return fmt.Errorf("client hello must not include session-id")
	}

	// Must have at least one capability
	if len(clientHello.Capabilities.Capability) == 0 {
		return fmt.Errorf("client hello must include capabilities")
	}

	return nil
}

// GetClientCapabilities returns a human-readable list of client capabilities
func GetClientCapabilities(clientHello *Hello) []string {
	capabilities := make([]string, 0, len(clientHello.Capabilities.Capability))
	for _, cap := range clientHello.Capabilities.Capability {
		// Extract short name for common capabilities
		shortName := cap
		if strings.HasPrefix(cap, "urn:ietf:params:xml:ns:netconf:") {
			parts := strings.Split(cap, ":")
			if len(parts) > 0 {
				shortName = parts[len(parts)-1]
			}
		}
		capabilities = append(capabilities, shortName)
	}
	return capabilities
}
