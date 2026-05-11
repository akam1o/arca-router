package netconf

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// RPC represents a NETCONF <rpc> request envelope
type RPC struct {
	XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc"`
	MessageID string   `xml:"message-id,attr"`
	Operation xml.Name `xml:",any"`
	Content   []byte   `xml:",innerxml"`

	NamespaceAttrs []xml.Attr `xml:"-"`
}

type rpcEnvelope struct {
	XMLName    xml.Name       `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc"`
	MessageID  string         `xml:"message-id,attr"`
	Attrs      []xml.Attr     `xml:",any,attr"`
	Operations []rpcOperation `xml:",any"`
}

type rpcOperation struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Content []byte     `xml:",innerxml"`
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

	var envelope rpcEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, ErrMalformedMessage(fmt.Sprintf("XML parse error: %v", err))
	}
	if err := ensureNoTrailingXML(decoder); err != nil {
		return nil, err
	}

	// Validate NETCONF base namespace
	if envelope.XMLName.Space != netconfNamespace {
		return nil, ErrInvalidNamespace(envelope.XMLName.Space)
	}

	// Validate message-id presence
	if envelope.MessageID == "" {
		return nil, ErrMissingElement("rpc", "message-id")
	}
	if len(envelope.Operations) == 0 {
		return nil, ErrMissingElement("rpc", "operation")
	}
	if len(envelope.Operations) > 1 {
		return nil, ErrMalformedMessage("rpc must contain exactly one operation")
	}

	operation := envelope.Operations[0]
	if err := validateRPCOperationAttributes(operation); err != nil {
		return nil, err
	}

	// Validate protocol namespace for operation element
	rpc := &RPC{
		XMLName:        envelope.XMLName,
		MessageID:      envelope.MessageID,
		Operation:      operation.XMLName,
		Content:        operation.Content,
		NamespaceAttrs: collectNamespaceAttrs(envelope.Attrs, operation.Attrs),
	}
	if err := ValidateProtocolNamespace(rpc.Operation); err != nil {
		return nil, err
	}
	if err := rpc.validateOperationPayload(); err != nil {
		return nil, err
	}

	return rpc, nil
}

func ensureNoTrailingXML(decoder *xml.Decoder) error {
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return ErrMalformedMessage(fmt.Sprintf("XML parse error: %v", err))
		}
		if charData, ok := token.(xml.CharData); ok && len(bytes.TrimSpace(charData)) == 0 {
			continue
		}
		return ErrMalformedMessage("trailing content after rpc element")
	}
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
	wrapped := r.operationXML()

	decoder := xml.NewDecoder(bytes.NewReader(wrapped))
	decoder.Strict = true
	decoder.Entity = nil

	if err := decoder.Decode(v); err != nil {
		return ErrMalformedMessage(fmt.Sprintf("operation parse error: %v", err))
	}
	if receiver, ok := v.(inheritedNamespaceReceiver); ok {
		receiver.SetInheritedNamespaceAttrs(r.NamespaceAttrs)
	}

	return nil
}

type inheritedNamespaceReceiver interface {
	SetInheritedNamespaceAttrs([]xml.Attr)
}

func (r *RPC) operationXML() []byte {
	var buf bytes.Buffer
	buf.WriteByte('<')
	buf.WriteString(r.Operation.Local)

	defaultNamespace := r.Operation.Space
	if defaultNamespace == "" {
		defaultNamespace = netconfNamespace
	}
	written := map[string]string{"xmlns": defaultNamespace}
	writeXMLAttribute(&buf, "xmlns", defaultNamespace)
	writeNamespaceDeclarationAttrs(&buf, r.NamespaceAttrs, written)

	buf.WriteByte('>')
	buf.Write(r.Content)
	buf.WriteString("</")
	buf.WriteString(r.Operation.Local)
	buf.WriteByte('>')
	return buf.Bytes()
}

func validateRPCOperationAttributes(operation rpcOperation) error {
	for _, attr := range operation.Attrs {
		if isNamespaceDeclarationAttribute(attr) {
			continue
		}
		rpcErr := ErrUnknownAttribute("/rpc/"+operation.XMLName.Local, attr.Name.Local)
		if attr.Name.Space != "" {
			rpcErr = rpcErr.WithBadNamespace(attr.Name.Space)
		}
		return rpcErr
	}
	return nil
}

func (r *RPC) validateOperationPayload() error {
	if _, ok := rpcOperationElementPaths[r.Operation.Local]; !ok {
		return nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(r.operationXML()))
	decoder.Strict = true
	decoder.Entity = nil

	stack := []string{}
	openContentDepth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if len(stack) != 0 || openContentDepth != 0 {
				return ErrMalformedMessage("unexpected end of operation payload")
			}
			return nil
		}
		if err != nil {
			return ErrMalformedMessage(fmt.Sprintf("operation parse error: %v", err))
		}

		switch t := token.(type) {
		case xml.StartElement:
			if openContentDepth > 0 {
				openContentDepth++
				continue
			}
			path := append(append([]string{}, stack...), t.Name.Local)
			if err := r.validateOperationElement(t, path); err != nil {
				return err
			}
			stack = append(stack, t.Name.Local)
			if isOpenRPCContentPath(path) {
				openContentDepth = 1
			}
		case xml.EndElement:
			if openContentDepth > 0 {
				openContentDepth--
				if openContentDepth == 0 {
					stack = stack[:len(stack)-1]
				}
				continue
			}
			if len(stack) == 0 {
				return ErrMalformedMessage(fmt.Sprintf("unexpected closing element: %s", t.Name.Local))
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func (r *RPC) validateOperationElement(start xml.StartElement, path []string) error {
	pathKey := rpcPathKey(path)
	if _, ok := rpcOperationElementPaths[r.Operation.Local][pathKey]; !ok {
		return ErrUnknownElement(rpcElementRPCPath(path), start.Name.Local)
	}

	if !allowsAnyElementNamespace(path) && start.Name.Space != netconfNamespace {
		return NewRPCError(ErrorTypeProtocol, "unknown-namespace",
			fmt.Sprintf("invalid namespace for RPC element %s", start.Name.Local)).
			WithPath(rpcElementRPCPath(path)).
			WithBadNamespace(start.Name.Space)
	}

	allowedAttrs := rpcElementAllowedAttrs(pathKey)
	for _, attr := range start.Attr {
		if isNamespaceDeclarationAttribute(attr) {
			continue
		}
		if attr.Name.Space == "" && allowedAttrs[attr.Name.Local] {
			continue
		}
		rpcErr := ErrUnknownAttribute(rpcElementRPCPath(path), attr.Name.Local)
		if attr.Name.Space != "" {
			rpcErr = rpcErr.WithBadNamespace(attr.Name.Space)
		}
		return rpcErr
	}
	return nil
}

func collectNamespaceAttrs(attrGroups ...[]xml.Attr) []xml.Attr {
	var attrs []xml.Attr
	seen := map[string]int{}
	for _, group := range attrGroups {
		for _, attr := range group {
			if !isNamespaceDeclarationAttribute(attr) {
				continue
			}
			name, ok := namespaceDeclarationAttrName(attr)
			if !ok {
				continue
			}
			if idx, exists := seen[name]; exists {
				attrs[idx] = attr
				continue
			}
			seen[name] = len(attrs)
			attrs = append(attrs, attr)
		}
	}
	return attrs
}

func cloneXMLAttrs(attrs []xml.Attr) []xml.Attr {
	if len(attrs) == 0 {
		return nil
	}
	clone := make([]xml.Attr, len(attrs))
	copy(clone, attrs)
	return clone
}

func writeNamespaceDeclarationAttrs(buf *bytes.Buffer, attrs []xml.Attr, written map[string]string) {
	for _, attr := range attrs {
		name, ok := namespaceDeclarationAttrName(attr)
		if !ok {
			continue
		}
		if value, exists := written[name]; exists && value == attr.Value {
			continue
		}
		if _, exists := written[name]; exists {
			continue
		}
		writeXMLAttribute(buf, name, attr.Value)
		written[name] = attr.Value
	}
}

func namespaceDeclarationAttrName(attr xml.Attr) (string, bool) {
	if attr.Name.Space == "" && attr.Name.Local == "xmlns" {
		return "xmlns", true
	}
	if attr.Name.Space == "xmlns" {
		return "xmlns:" + attr.Name.Local, true
	}
	return "", false
}

var rpcOperationElementPaths = map[string]map[string]struct{}{
	"get-config": {
		"get-config":                  {},
		"get-config/source":           {},
		"get-config/source/running":   {},
		"get-config/source/candidate": {},
		"get-config/source/startup":   {},
		"get-config/filter":           {},
	},
	"edit-config": {
		"edit-config":                   {},
		"edit-config/target":            {},
		"edit-config/target/running":    {},
		"edit-config/target/candidate":  {},
		"edit-config/target/startup":    {},
		"edit-config/default-operation": {},
		"edit-config/test-option":       {},
		"edit-config/error-option":      {},
		"edit-config/config":            {},
	},
	"copy-config": {
		"copy-config":                  {},
		"copy-config/target":           {},
		"copy-config/target/running":   {},
		"copy-config/target/candidate": {},
		"copy-config/target/startup":   {},
		"copy-config/source":           {},
		"copy-config/source/running":   {},
		"copy-config/source/candidate": {},
		"copy-config/source/startup":   {},
	},
	"delete-config": {
		"delete-config":                  {},
		"delete-config/target":           {},
		"delete-config/target/running":   {},
		"delete-config/target/candidate": {},
		"delete-config/target/startup":   {},
	},
	"lock": {
		"lock":                  {},
		"lock/target":           {},
		"lock/target/running":   {},
		"lock/target/candidate": {},
		"lock/target/startup":   {},
	},
	"unlock": {
		"unlock":                  {},
		"unlock/target":           {},
		"unlock/target/running":   {},
		"unlock/target/candidate": {},
		"unlock/target/startup":   {},
	},
	"commit": {
		"commit": {},
	},
	"discard-changes": {
		"discard-changes": {},
	},
	"validate": {
		"validate":                  {},
		"validate/source":           {},
		"validate/source/running":   {},
		"validate/source/candidate": {},
		"validate/source/startup":   {},
	},
	"get": {
		"get":        {},
		"get/filter": {},
	},
	"close-session": {
		"close-session": {},
	},
	"kill-session": {
		"kill-session":            {},
		"kill-session/session-id": {},
	},
}

var rpcFilterAttrs = map[string]bool{
	"type":   true,
	"select": true,
}

func rpcElementAllowedAttrs(path string) map[string]bool {
	if strings.HasSuffix(path, "/filter") {
		return rpcFilterAttrs
	}
	return nil
}

func isOpenRPCContentPath(path []string) bool {
	key := rpcPathKey(path)
	return key == "edit-config/config" ||
		key == "get-config/filter" ||
		key == "get/filter"
}

func allowsAnyElementNamespace(path []string) bool {
	return rpcPathKey(path) == "edit-config/config"
}

func rpcPathKey(path []string) string {
	return strings.Join(path, "/")
}

func rpcElementRPCPath(path []string) string {
	if len(path) == 0 {
		return "/rpc"
	}
	return "/rpc/" + strings.Join(path, "/")
}

// ValidateOperationNamespace checks if operation is in NETCONF namespace
func (r *RPC) ValidateOperationNamespace() error {
	return ValidateProtocolNamespace(r.Operation)
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
	Type           string     `xml:"type,attr,omitempty"`
	Select         string     `xml:"select,attr,omitempty"` // For xpath (not supported)
	Attrs          []xml.Attr `xml:",any,attr"`
	InheritedAttrs []xml.Attr `xml:"-"`
	Content        []byte     `xml:",innerxml"`
}

// Validate validates filter constraints per design document
func (f *Filter) Validate(rpcName string) error {
	if f == nil {
		return nil // Filter is optional
	}
	for _, attr := range f.Attrs {
		if isNamespaceDeclarationAttribute(attr) {
			continue
		}
		rpcErr := ErrUnknownAttribute("/rpc/"+rpcName+"/filter", attr.Name.Local)
		if attr.Name.Space != "" {
			rpcErr = rpcErr.WithBadNamespace(attr.Name.Space)
		}
		return rpcErr
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
	TestSet         TestOption = "set"
	TestTestThenSet TestOption = "test-then-set"
	TestTestOnly    TestOption = "test-only"
)

// ErrorOption for edit-config
type ErrorOption string

const (
	ErrorStop            ErrorOption = "stop-on-error"
	ErrorContinue        ErrorOption = "continue-on-error"
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
