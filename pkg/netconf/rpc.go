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
	ReplyAttrs     []xml.Attr `xml:"-"`
}

type rpcEnvelope struct {
	XMLName    xml.Name       `xml:"rpc"`
	MessageID  string         `xml:"message-id,attr"`
	Attrs      []xml.Attr     `xml:",any,attr"`
	Content    []byte         `xml:",innerxml"`
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
	if err := ensureNoTrailingXML(decoder, "rpc"); err != nil {
		return nil, err
	}

	// Validate NETCONF base namespace
	if envelope.XMLName.Space != netconfNamespace {
		return nil, ErrInvalidNamespace(envelope.XMLName.Space)
	}
	if err := validateRPCRootAttributes(envelope.Attrs); err != nil {
		return nil, err
	}
	if err := validateRPCRootContent(envelope.Content, envelope.Attrs); err != nil {
		return nil, err
	}

	// Validate message-id presence
	if envelope.MessageID == "" {
		return nil, ErrMissingAttribute("rpc", "message-id")
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
		ReplyAttrs:     rpcReplyAttrsFromRootAttrs(envelope.Attrs),
	}
	if err := ValidateProtocolNamespace(rpc.Operation); err != nil {
		return nil, err
	}
	if err := rpc.validateOperationPayload(); err != nil {
		return nil, err
	}

	return rpc, nil
}

func ensureNoTrailingXML(decoder *xml.Decoder, rootElement string) error {
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
		return ErrMalformedMessage(fmt.Sprintf("trailing content after %s element", rootElement))
	}
}

// GetOperationName returns the RPC operation name (e.g., "get-config", "edit-config")
func (r *RPC) GetOperationName() string {
	if r == nil {
		return ""
	}
	return r.Operation.Local
}

// GetOperationNamespace returns the RPC operation namespace
func (r *RPC) GetOperationNamespace() string {
	if r == nil {
		return ""
	}
	return r.Operation.Space
}

// UnmarshalOperation unmarshals the RPC operation content into a specific struct
func (r *RPC) UnmarshalOperation(v interface{}) error {
	if r == nil {
		return ErrOperationFailed("rpc unavailable")
	}
	if r.Operation.Local == "" {
		return ErrOperationFailed("rpc operation unavailable")
	}

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

func validateRPCRootAttributes(_ []xml.Attr) error {
	// RFC 6241 requires peers to accept additional <rpc> attributes and return
	// them unmodified in <rpc-reply>.
	return nil
}

func extractRPCReplyContext(data []byte) (string, []xml.Attr) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	decoder.Entity = nil

	for {
		token, err := decoder.Token()
		if err != nil {
			return "", nil
		}

		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local != "rpc" {
				return "", nil
			}

			messageID := ""
			for _, attr := range t.Attr {
				if isMessageIDAttribute(attr) {
					messageID = attr.Value
					break
				}
			}
			return messageID, rpcReplyAttrsFromRootAttrs(t.Attr)
		case xml.CharData:
			if len(bytes.TrimSpace(t)) == 0 {
				continue
			}
			return "", nil
		}
	}
}

func rpcReplyAttrsFromRootAttrs(attrs []xml.Attr) []xml.Attr {
	if len(attrs) == 0 {
		return nil
	}

	replyAttrs := make([]xml.Attr, 0, len(attrs))
	for _, attr := range attrs {
		if isMessageIDAttribute(attr) {
			continue
		}
		replyAttrs = append(replyAttrs, attr)
	}
	return replyAttrs
}

func isMessageIDAttribute(attr xml.Attr) bool {
	return attr.Name.Space == "" && attr.Name.Local == "message-id"
}

func validateRPCRootContent(content []byte, attrs []xml.Attr) error {
	var wrapped bytes.Buffer
	wrapped.WriteString("<rpc")
	writeNamespaceDeclarationAttrs(&wrapped, collectNamespaceAttrs(attrs), map[string]string{})
	wrapped.WriteByte('>')
	wrapped.Write(content)
	wrapped.WriteString("</rpc>")

	decoder := xml.NewDecoder(bytes.NewReader(wrapped.Bytes()))
	decoder.Strict = true
	decoder.Entity = nil

	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return ErrMalformedMessage(fmt.Sprintf("XML parse error: %v", err))
		}

		switch t := token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
		case xml.CharData:
			if depth == 1 && len(bytes.TrimSpace(t)) > 0 {
				return ErrMalformedMessage("unexpected text in /rpc").WithPath("/rpc")
			}
		}
	}
}

func (r *RPC) validateOperationPayload() error {
	if _, ok := rpcOperationElementPaths[r.Operation.Local]; !ok {
		return nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(r.operationXML()))
	decoder.Strict = true
	decoder.Entity = nil

	stack := []string{}
	counts := map[string]int{}
	openContentDepth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if len(stack) != 0 || openContentDepth != 0 {
				return ErrMalformedMessage("unexpected end of operation payload")
			}
			return r.validateOperationCardinality(counts)
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
			counts[rpcPathKey(path)]++
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
		case xml.CharData:
			if openContentDepth > 0 || len(bytes.TrimSpace(t)) == 0 {
				continue
			}
			if isRPCTextContentPath(stack) {
				continue
			}
			return ErrMalformedMessage(fmt.Sprintf("unexpected text in %s", rpcElementRPCPath(stack))).
				WithPath(rpcElementRPCPath(stack))
		}
	}
}

func (r *RPC) validateOperationElement(start xml.StartElement, path []string) error {
	pathKey := rpcPathKey(path)
	if _, ok := rpcOperationElementPaths[r.Operation.Local][pathKey]; !ok {
		return ErrUnknownElement(rpcElementRPCPath(path), start.Name.Local)
	}

	if !allowsAnyElementNamespace(path) && start.Name.Space != netconfNamespace {
		return NewRPCError(ErrorTypeProtocol, ErrorTagUnknownNamespace,
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

func (r *RPC) validateOperationCardinality(counts map[string]int) error {
	for _, rule := range rpcOperationCardinalityRules[r.Operation.Local] {
		count := counts[rule.path]
		path := rpcPathFromKey(rule.path)
		if count < rule.min {
			return missingRPCElement(path, lastRPCPathPart(rule.path))
		}
		if rule.max >= 0 && count > rule.max {
			return ErrMalformedMessage(fmt.Sprintf("%s must appear at most once", rule.path)).
				WithPath(rpcElementRPCPath(path))
		}
	}

	for _, choicePath := range rpcDatastoreChoicePaths[r.Operation.Local] {
		count := 0
		for _, datastore := range rpcDatastoreElements {
			count += counts[choicePath+"/"+datastore]
		}
		choiceName := "datastore"
		if allowsConfigSourceChoice(choicePath) {
			count += counts[choicePath+"/config"]
			choiceName = "source choice"
		}
		path := rpcPathFromKey(choicePath)
		switch {
		case count == 0:
			return missingRPCElement(path, "datastore")
		case count > 1:
			return ErrMalformedMessage(fmt.Sprintf("%s must contain exactly one %s", choicePath, choiceName)).
				WithPath(rpcElementRPCPath(path))
		}
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
		if attr.Name.Local == "" {
			return "", false
		}
		return "xmlns:" + attr.Name.Local, true
	}
	return "", false
}

type rpcCardinalityRule struct {
	path string
	min  int
	max  int
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
		"copy-config/source/config":    {},
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
		"commit":                 {},
		"commit/confirmed":       {},
		"commit/confirm-timeout": {},
		"commit/persist":         {},
		"commit/persist-id":      {},
	},
	"discard-changes": {
		"discard-changes": {},
	},
	"validate": {
		"validate":                  {},
		"validate/source":           {},
		"validate/source/config":    {},
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

var rpcOperationCardinalityRules = map[string][]rpcCardinalityRule{
	"get-config": {
		{path: "get-config/source", min: 1, max: 1},
		{path: "get-config/filter", min: 0, max: 1},
	},
	"edit-config": {
		{path: "edit-config/target", min: 1, max: 1},
		{path: "edit-config/default-operation", min: 0, max: 1},
		{path: "edit-config/test-option", min: 0, max: 1},
		{path: "edit-config/error-option", min: 0, max: 1},
		{path: "edit-config/config", min: 1, max: 1},
	},
	"copy-config": {
		{path: "copy-config/target", min: 1, max: 1},
		{path: "copy-config/source", min: 1, max: 1},
	},
	"delete-config": {
		{path: "delete-config/target", min: 1, max: 1},
	},
	"lock": {
		{path: "lock/target", min: 1, max: 1},
	},
	"unlock": {
		{path: "unlock/target", min: 1, max: 1},
	},
	"validate": {
		{path: "validate/source", min: 1, max: 1},
	},
	"get": {
		{path: "get/filter", min: 0, max: 1},
	},
	"kill-session": {
		{path: "kill-session/session-id", min: 1, max: 1},
	},
	"commit": {
		{path: "commit/confirmed", min: 0, max: 1},
		{path: "commit/confirm-timeout", min: 0, max: 1},
		{path: "commit/persist", min: 0, max: 1},
		{path: "commit/persist-id", min: 0, max: 1},
	},
}

var rpcDatastoreChoicePaths = map[string][]string{
	"get-config":    {"get-config/source"},
	"edit-config":   {"edit-config/target"},
	"copy-config":   {"copy-config/target", "copy-config/source"},
	"delete-config": {"delete-config/target"},
	"lock":          {"lock/target"},
	"unlock":        {"unlock/target"},
	"validate":      {"validate/source"},
}

var rpcDatastoreElements = []string{
	"running",
	"candidate",
	"startup",
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
		key == "copy-config/source/config" ||
		key == "validate/source/config" ||
		key == "get-config/filter" ||
		key == "get/filter"
}

func allowsAnyElementNamespace(path []string) bool {
	return rpcPathKey(path) == "edit-config/config"
}

func isRPCTextContentPath(path []string) bool {
	_, ok := rpcTextContentPaths[rpcPathKey(path)]
	return ok
}

var rpcTextContentPaths = map[string]struct{}{
	"edit-config/default-operation": {},
	"edit-config/test-option":       {},
	"edit-config/error-option":      {},
	"commit/confirm-timeout":        {},
	"commit/persist":                {},
	"commit/persist-id":             {},
	"kill-session/session-id":       {},
}

func allowsConfigSourceChoice(path string) bool {
	return path == "copy-config/source" ||
		path == "validate/source"
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

func rpcPathFromKey(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func lastRPCPathPart(path string) string {
	parts := rpcPathFromKey(path)
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func missingRPCElement(path []string, element string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagMissingElement, fmt.Sprintf("missing required element: %s", element)).
		WithPath(rpcElementRPCPath(path)).
		WithBadElement(element)
}

// ValidateOperationNamespace checks if operation is in NETCONF namespace
func (r *RPC) ValidateOperationNamespace() error {
	if r == nil {
		return ErrOperationFailed("rpc unavailable")
	}
	if r.Operation.Local == "" {
		return ErrOperationFailed("rpc operation unavailable")
	}
	return ValidateProtocolNamespace(r.Operation)
}

// Datastore target constants
const (
	DatastoreRunning   = "running"
	DatastoreCandidate = "candidate"
	DatastoreStartup   = "startup"
)

// Source represents <source> element in get-config, copy-config, and validate.
type Source struct {
	Running   *struct{}      `xml:"running"`
	Candidate *struct{}      `xml:"candidate"`
	Startup   *struct{}      `xml:"startup"`
	Config    *ConfigElement `xml:"config"`
}

// GetDatastore returns the datastore name from Source
func (s *Source) GetDatastore() (string, error) {
	if s == nil {
		return "", ErrMissingElement("source", "datastore")
	}
	return selectDatastore("source", s.Running != nil, s.Candidate != nil, s.Startup != nil)
}

// Target represents <target> element in edit-config/lock/unlock
type Target struct {
	Running   *struct{} `xml:"running"`
	Candidate *struct{} `xml:"candidate"`
	Startup   *struct{} `xml:"startup"`
}

// GetDatastore returns the datastore name from Target
func (t *Target) GetDatastore() (string, error) {
	if t == nil {
		return "", ErrMissingElement("target", "datastore")
	}
	return selectDatastore("target", t.Running != nil, t.Candidate != nil, t.Startup != nil)
}

func selectDatastore(container string, running, candidate, startup bool) (string, error) {
	selected := ""
	count := 0
	if running {
		selected = DatastoreRunning
		count++
	}
	if candidate {
		selected = DatastoreCandidate
		count++
	}
	if startup {
		selected = DatastoreStartup
		count++
	}
	if count == 0 {
		return "", ErrMissingElement(container, "datastore")
	}
	if count > 1 {
		return "", ErrMalformedMessage(fmt.Sprintf("%s must contain exactly one datastore", container)).
			WithPath("/rpc/" + container)
	}
	return selected, nil
}

// Filter represents optional <filter> element in get-config/get
type Filter struct {
	Type           string     `xml:"type,attr,omitempty"`
	Select         string     `xml:"select,attr,omitempty"` // For xpath filters
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
		if attr.Name.Local == "" {
			return NewRPCError(ErrorTypeRPC, ErrorTagInvalidValue,
				"filter attribute name must not be empty").
				WithPath("/rpc/" + rpcName + "/filter")
		}
		rpcErr := ErrUnknownAttribute("/rpc/"+rpcName+"/filter", attr.Name.Local)
		if attr.Name.Space != "" {
			rpcErr = rpcErr.WithBadNamespace(attr.Name.Space)
		}
		return rpcErr
	}

	// Check filter type
	filterType := normalizedFilterType(f)
	if filterType == "" {
		// Default to subtree if not specified
		filterType = "subtree"
	}

	switch filterType {
	case "xpath":
		return f.validateXPathFilter(rpcName)
	case "subtree":
	default:
		return ErrUnsupportedFilterType(rpcName, filterType)
	}

	// Validate subtree filter content (basic check)
	if len(f.Content) > 0 {
		// Check for predicates ([ ]) which are not supported
		if bytes.Contains(f.Content, []byte("[")) {
			return ErrInvalidFilter(rpcName, "filter contains unsupported predicates")
		}
		if err := f.validateSubtreeContent(rpcName); err != nil {
			return err
		}
	}

	return nil
}

func (f *Filter) validateXPathFilter(rpcName string) error {
	selectExpr := strings.TrimSpace(f.Select)
	if selectExpr == "" {
		return ErrInvalidFilter(rpcName, "xpath filter requires select attribute")
	}
	if len(bytes.TrimSpace(f.Content)) > 0 {
		return ErrInvalidFilter(rpcName, "xpath filter must not contain subtree content")
	}
	namespaceAttrs := collectNamespaceAttrs(f.InheritedAttrs, f.Attrs)
	xpathFilter, err := ParseXPathFilterWithContext(selectExpr, namespaceAttrs)
	if err != nil {
		return ErrInvalidFilter(rpcName, fmt.Sprintf("invalid xpath filter: %v", err))
	}
	if xpathFilter != nil {
		if err := validateXPathFilterNamespaces(xpathFilter); err != nil {
			return ErrInvalidFilter(rpcName, fmt.Sprintf("invalid xpath filter namespace: %v", err))
		}
		validator, err := GetGlobalValidator()
		if err != nil {
			return ErrInvalidFilter(rpcName, fmt.Sprintf("failed to initialize YANG validator: %v", err))
		}
		if err := validator.validateXPathFilterPath(xpathFilter); err != nil {
			return ErrInvalidFilter(rpcName, fmt.Sprintf("unsupported xpath filter path: %v", err))
		}
	}
	return nil
}

func (f *Filter) validateSubtreeContent(rpcName string) error {
	trimmed := bytes.TrimSpace(f.Content)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '/' {
		return ErrInvalidFilter(rpcName, "subtree filter content must be XML elements")
	}

	var wrapped bytes.Buffer
	wrapped.WriteString("<filter")
	writeNamespaceDeclarationAttrs(&wrapped, collectNamespaceAttrs(f.InheritedAttrs, f.Attrs), map[string]string{})
	wrapped.WriteByte('>')
	wrapped.Write(f.Content)
	wrapped.WriteString("</filter>")

	decoder := xml.NewDecoder(bytes.NewReader(wrapped.Bytes()))
	decoder.Strict = true
	decoder.Entity = nil

	depth := 0
	topLevelElements := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
				fmt.Sprintf("invalid subtree filter XML: %v", err)).
				WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
				WithBadElement("filter")
		}

		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				topLevelElements++
			}
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
		case xml.CharData:
			if depth == 1 && len(bytes.TrimSpace(t)) > 0 {
				return ErrInvalidFilter(rpcName, "subtree filter content must be XML elements")
			}
		}
	}
	if topLevelElements == 0 {
		return ErrInvalidFilter(rpcName, "subtree filter must contain at least one element")
	}
	filterPaths, err := f.parseElementPaths()
	if err != nil {
		return NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
			fmt.Sprintf("invalid subtree filter XML: %v", err)).
			WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
			WithBadElement("filter")
	}
	if err := validateSubtreeFilterPaths(filterPaths); err != nil {
		return ErrInvalidFilter(rpcName, fmt.Sprintf("invalid subtree filter path: %v", err))
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
