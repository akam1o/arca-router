package netconf

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
)

// RPC represents a NETCONF <rpc> request envelope
type RPC struct {
	XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc"`
	MessageID string   `xml:"message-id,attr"`
	Operation xml.Name `xml:",any"`
	Content   []byte   `xml:",innerxml"`
}

// ParseRPC parses NETCONF RPC from XML bytes with security checks
func ParseRPC(data []byte) (*RPC, error) {
	// Security check: reject DTD/DOCTYPE
	if bytes.Contains(data, []byte("<!DOCTYPE")) || bytes.Contains(data, []byte("<!ENTITY")) {
		return nil, ErrDTDNotAllowed()
	}

	// Size limit check (10MB)
	const maxRPCSize = 10 * 1024 * 1024
	if len(data) > maxRPCSize {
		return nil, ErrMalformedMessage(fmt.Sprintf("RPC size exceeds maximum (%d bytes)", maxRPCSize))
	}

	// Parse XML with strict settings
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true // Enable strict well-formedness checking
	decoder.Entity = nil  // Disable entity expansion

	var rpc RPC
	if err := decoder.Decode(&rpc); err != nil {
		return nil, ErrMalformedMessage(fmt.Sprintf("XML parse error: %v", err))
	}

	// Validate NETCONF base namespace
	if rpc.XMLName.Space != netconfNamespace {
		return nil, ErrInvalidNamespace(rpc.XMLName.Space)
	}

	// Validate message-id presence
	if rpc.MessageID == "" {
		return nil, ErrMissingElement("rpc", "message-id")
	}

	// Validate protocol namespace for operation element
	if err := ValidateProtocolNamespace(rpc.Operation); err != nil {
		return nil, err
	}

	return &rpc, nil
}

// GetOperationName returns the RPC operation name (e.g., "get-config", "edit-config")
func (r *RPC) GetOperationName() string {
	return r.Operation.Local
}

// GetOperationNamespace returns the RPC operation namespace
func (r *RPC) GetOperationNamespace() string {
	return r.Operation.Space
}

// UnmarshalOperation unmarshals the RPC operation content into a specific struct
func (r *RPC) UnmarshalOperation(v interface{}) error {
	// Wrap content in operation tag for proper unmarshaling
	wrapped := fmt.Sprintf("<%s xmlns=\"%s\">%s</%s>",
		r.Operation.Local, netconfNamespace, string(r.Content), r.Operation.Local)

	decoder := xml.NewDecoder(bytes.NewReader([]byte(wrapped)))
	decoder.Strict = true
	decoder.Entity = nil

	if err := decoder.Decode(v); err != nil {
		return ErrMalformedMessage(fmt.Sprintf("operation parse error: %v", err))
	}

	return nil
}

// ValidateOperationNamespace checks if operation is in NETCONF namespace
func (r *RPC) ValidateOperationNamespace() error {
	// Allow both NETCONF base:1.0 namespace and empty namespace (default)
	if r.Operation.Space != "" && r.Operation.Space != netconfNamespace {
		return ErrInvalidNamespace(r.Operation.Space)
	}
	return nil
}

// Datastore target constants
const (
	DatastoreRunning   = "running"
	DatastoreCandidate = "candidate"
	DatastoreStartup   = "startup"
)

// Source represents <source> element in get-config
type Source struct {
	Running   *struct{} `xml:"running"`
	Candidate *struct{} `xml:"candidate"`
	Startup   *struct{} `xml:"startup"`
}

// GetDatastore returns the datastore name from Source
func (s *Source) GetDatastore() (string, error) {
	if s.Running != nil {
		return DatastoreRunning, nil
	}
	if s.Candidate != nil {
		return DatastoreCandidate, nil
	}
	if s.Startup != nil {
		return DatastoreStartup, nil
	}
	return "", ErrMissingElement("source", "datastore")
}

// Target represents <target> element in edit-config/lock/unlock
type Target struct {
	Running   *struct{} `xml:"running"`
	Candidate *struct{} `xml:"candidate"`
	Startup   *struct{} `xml:"startup"`
}

// GetDatastore returns the datastore name from Target
func (t *Target) GetDatastore() (string, error) {
	if t.Running != nil {
		return DatastoreRunning, nil
	}
	if t.Candidate != nil {
		return DatastoreCandidate, nil
	}
	if t.Startup != nil {
		return DatastoreStartup, nil
	}
	return "", ErrMissingElement("target", "datastore")
}

// Filter represents optional <filter> element in get-config/get
type Filter struct {
	Type    string `xml:"type,attr,omitempty"`
	Select  string `xml:"select,attr,omitempty"` // For xpath (not supported)
	Content []byte `xml:",innerxml"`
}

// Validate validates filter constraints per design document
func (f *Filter) Validate(rpcName string) error {
	if f == nil {
		return nil // Filter is optional
	}

	// Check filter type
	if f.Type == "" {
		// Default to subtree if not specified
		f.Type = "subtree"
	}

	// Reject xpath type
	if f.Type == "xpath" {
		return ErrUnsupportedFilterType(rpcName, "xpath")
	}

	// Only subtree is supported
	if f.Type != "subtree" {
		return ErrUnsupportedFilterType(rpcName, f.Type)
	}

	// Validate subtree filter content (basic check)
	if len(f.Content) > 0 {
		// Check for predicates ([ ]) which are not supported
		if bytes.Contains(f.Content, []byte("[")) {
			return ErrInvalidFilter(rpcName, "filter contains unsupported predicates")
		}
	}

	return nil
}

// DefaultOperation for edit-config
type DefaultOperation string

const (
	DefaultOpMerge   DefaultOperation = "merge"
	DefaultOpReplace DefaultOperation = "replace"
	DefaultOpNone    DefaultOperation = "none"
)

// TestOption for edit-config
type TestOption string

const (
	TestSet       TestOption = "set"
	TestTestThenSet TestOption = "test-then-set"
	TestTestOnly  TestOption = "test-only"
)

// ErrorOption for edit-config
type ErrorOption string

const (
	ErrorStop          ErrorOption = "stop-on-error"
	ErrorContinue      ErrorOption = "continue-on-error"
	ErrorRollbackOnError ErrorOption = "rollback-on-error"
)

// ParseAndValidateRPC is a convenience function that parses and performs basic validation
func ParseAndValidateRPC(data []byte) (*RPC, error) {
	rpc, err := ParseRPC(data)
	if err != nil {
		return nil, err
	}

	if err := rpc.ValidateOperationNamespace(); err != nil {
		return nil, err
	}

	return rpc, nil
}

// ReadRPCFromFraming reads and parses RPC from a framing reader
func ReadRPCFromFraming(reader io.Reader, baseVersion string) (*RPC, error) {
	fr := NewFramingReader(reader, baseVersion)
	data, err := fr.ReadMessage()
	if err != nil {
		return nil, ErrMalformedMessage(fmt.Sprintf("framing error: %v", err))
	}

	return ParseAndValidateRPC(data)
}
