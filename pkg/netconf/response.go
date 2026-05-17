package netconf

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
)

const (
	netconfNamespace = "urn:ietf:params:xml:ns:netconf:base:1.0"
	xmlNamespace     = "http://www.w3.org/XML/1998/namespace"
	xmlnsNamespace   = "http://www.w3.org/2000/xmlns/"
)

// RPCReply represents a NETCONF <rpc-reply> envelope
type RPCReply struct {
	XMLName   xml.Name    `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc-reply"`
	MessageID string      `xml:"message-id,attr"`
	OK        *struct{}   `xml:"ok,omitempty"`
	Data      *DataReply  `xml:"data,omitempty"`
	Errors    []*RPCError `xml:"rpc-error,omitempty"`
	Attrs     []xml.Attr  `xml:"-"`
}

// DataReply represents <data> element in response
type DataReply struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 data"`
	Content []byte   `xml:",innerxml"`
}

// NewOKReply creates a successful <rpc-reply> with <ok/>
func NewOKReply(messageID string) *RPCReply {
	return &RPCReply{
		MessageID: messageID,
		OK:        &struct{}{},
	}
}

// NewDataReply creates a successful <rpc-reply> with <data>
func NewDataReply(messageID string, data []byte) *RPCReply {
	return &RPCReply{
		MessageID: messageID,
		Data: &DataReply{
			Content: append([]byte(nil), data...),
		},
	}
}

// NewErrorReply creates an error <rpc-reply> with one <rpc-error>
func NewErrorReply(messageID string, err *RPCError) *RPCReply {
	return &RPCReply{
		MessageID: messageID,
		Errors:    []*RPCError{cloneRPCError(err)},
	}
}

// NewMultiErrorReply creates an error <rpc-reply> with multiple <rpc-error>
func NewMultiErrorReply(messageID string, errors []*RPCError) *RPCReply {
	if len(errors) == 0 {
		errors = []*RPCError{nil}
	}
	normalized := make([]*RPCError, len(errors))
	for i, err := range errors {
		normalized[i] = cloneRPCError(err)
	}
	return &RPCReply{
		MessageID: messageID,
		Errors:    normalized,
	}
}

func normalizeRPCError(err *RPCError) *RPCError {
	if err == nil {
		return ErrOperationFailed("rpc error unavailable")
	}
	if err.ErrorType != "" && err.ErrorTag != "" && err.ErrorSeverity != "" && !isEmptyErrorInfo(err.ErrorInfo) {
		return err
	}

	normalized := *err
	if isEmptyErrorInfo(err.ErrorInfo) {
		normalized.ErrorInfo = nil
	} else {
		info := *err.ErrorInfo
		normalized.ErrorInfo = &info
	}
	if normalized.ErrorType == "" {
		normalized.ErrorType = ErrorTypeRPC
	}
	if normalized.ErrorTag == "" {
		normalized.ErrorTag = ErrorTagOperationFailed
	}
	if normalized.ErrorSeverity == "" {
		normalized.ErrorSeverity = ErrorSeverityError
	}
	return &normalized
}

func isEmptyErrorInfo(info *ErrorInfo) bool {
	return info != nil &&
		info.BadElement == "" &&
		info.BadAttribute == "" &&
		info.BadNamespace == "" &&
		info.LockOwnerSession == ""
}

func validateRPCErrorFields(err *RPCError) error {
	if !isValidErrorType(err.ErrorType) {
		return fmt.Errorf("invalid RPC error type %q", err.ErrorType)
	}
	if !isValidErrorTag(err.ErrorTag) {
		return fmt.Errorf("invalid RPC error tag %q", err.ErrorTag)
	}
	if !isValidErrorSeverity(err.ErrorSeverity) {
		return fmt.Errorf("invalid RPC error severity %q", err.ErrorSeverity)
	}
	return nil
}

func isValidErrorType(errType ErrorType) bool {
	switch errType {
	case ErrorTypeProtocol, ErrorTypeApplication, ErrorTypeTransport, ErrorTypeRPC:
		return true
	default:
		return false
	}
}

func isValidErrorTag(errTag ErrorTag) bool {
	switch errTag {
	case ErrorTagInvalidValue,
		ErrorTagMalformedMessage,
		ErrorTagOperationNotSupported,
		ErrorTagAccessDenied,
		ErrorTagLockDenied,
		ErrorTagInUse,
		ErrorTagOperationFailed,
		ErrorTagMissingElement,
		ErrorTagMissingAttribute,
		ErrorTagUnknownElement,
		ErrorTagUnknownAttribute,
		ErrorTagUnknownNamespace:
		return true
	default:
		return false
	}
}

func isValidErrorSeverity(severity ErrorSeverity) bool {
	switch severity {
	case ErrorSeverityError, ErrorSeverityWarning:
		return true
	default:
		return false
	}
}

func cloneRPCError(err *RPCError) *RPCError {
	normalized := normalizeRPCError(err)
	clone := *normalized
	if normalized.ErrorInfo != nil {
		info := *normalized.ErrorInfo
		clone.ErrorInfo = &info
	}
	return &clone
}

func (r *RPCReply) WithAttributes(attrs []xml.Attr) *RPCReply {
	if r == nil {
		return nil
	}
	r.Attrs = cloneXMLAttrs(attrs)
	return r
}

// MarshalReply serializes RPCReply to XML bytes
func MarshalReply(reply *RPCReply) ([]byte, error) {
	if reply == nil {
		return nil, fmt.Errorf("nil RPC reply")
	}
	if err := validateReplyPayload(reply); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("<rpc-reply")
	writeXMLAttribute(&buf, "xmlns", netconfNamespace)
	if reply.MessageID != "" {
		writeXMLAttribute(&buf, "message-id", reply.MessageID)
	}
	if err := writeReplyAttributes(&buf, reply.Attrs); err != nil {
		return nil, err
	}
	buf.WriteByte('>')

	if reply.OK != nil {
		buf.WriteString("<ok/>")
	}
	if reply.Data != nil {
		buf.WriteString("<data>")
		buf.Write(reply.Data.Content)
		buf.WriteString("</data>")
	}
	for _, rpcErr := range reply.Errors {
		normalizedErr := normalizeRPCError(rpcErr)
		if err := validateRPCErrorFields(normalizedErr); err != nil {
			return nil, err
		}
		data, err := xml.Marshal(normalizedErr)
		if err != nil {
			return nil, err
		}
		buf.Write(data)
	}

	buf.WriteString("</rpc-reply>")
	if buf.Len() > MaxXMLSize {
		return nil, fmt.Errorf("RPC reply exceeds maximum (%d bytes)", MaxXMLSize)
	}
	return buf.Bytes(), nil
}

func validateReplyPayload(reply *RPCReply) error {
	payloads := 0
	if reply.OK != nil {
		payloads++
	}
	if reply.Data != nil {
		payloads++
	}
	if len(reply.Errors) > 0 {
		payloads++
	}
	if payloads == 0 {
		return fmt.Errorf("RPC reply has no payload")
	}
	if payloads > 1 {
		return fmt.Errorf("RPC reply has multiple payloads")
	}
	if reply.Data != nil {
		if err := validateDataReplyContent(reply.Data.Content); err != nil {
			return err
		}
	}
	return nil
}

func validateDataReplyContent(content []byte) error {
	if len(content) > MaxXMLSize {
		return fmt.Errorf("data reply content exceeds maximum (%d bytes)", MaxXMLSize)
	}
	if containsUnsafeXMLDirective(content) {
		return fmt.Errorf("data reply content contains unsafe XML directives")
	}

	var wrapped bytes.Buffer
	wrapped.WriteString("<data")
	writeXMLAttribute(&wrapped, "xmlns", netconfNamespace)
	wrapped.WriteByte('>')
	wrapped.Write(content)
	wrapped.WriteString("</data>")

	decoder := xml.NewDecoder(bytes.NewReader(wrapped.Bytes()))
	decoder.Strict = true
	decoder.Entity = nil

	depth := 0
	elementCount := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("data reply content is malformed: %w", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if depth > 1 {
				elementCount++
				if elementCount > MaxXMLElements {
					return fmt.Errorf("data reply content exceeds maximum element limit (%d)", MaxXMLElements)
				}
				if depth-1 > MaxXMLDepth {
					return fmt.Errorf("data reply content exceeds maximum depth limit (%d)", MaxXMLDepth)
				}
				if len(t.Attr) > MaxXMLAttributes {
					return fmt.Errorf("data reply element %s exceeds maximum attribute limit (%d)", t.Name.Local, MaxXMLAttributes)
				}
			}
			for _, attr := range t.Attr {
				if err := validateNamespaceDeclarationAttr(attr); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
		case xml.CharData:
			if depth == 1 && len(bytes.TrimSpace(t)) > 0 {
				return fmt.Errorf("data reply content contains text outside elements")
			}
		}
	}
}

func writeReplyAttributes(buf *bytes.Buffer, attrs []xml.Attr) error {
	namespacePrefixes := map[string]string{
		xmlNamespace: "xml",
	}
	written := map[string]string{
		"xmlns": netconfNamespace,
	}

	for _, attr := range attrs {
		if err := validateNamespaceDeclarationAttr(attr); err != nil {
			return err
		}
		name, ok := namespaceDeclarationAttrName(attr)
		if !ok {
			continue
		}
		if name == "xmlns" && attr.Value == netconfNamespace {
			continue
		}
		if _, exists := written[name]; exists {
			continue
		}
		writeXMLAttribute(buf, name, attr.Value)
		written[name] = attr.Value

		if attr.Name.Space == "xmlns" && attr.Name.Local != "" {
			if _, exists := namespacePrefixes[attr.Value]; !exists {
				namespacePrefixes[attr.Value] = attr.Name.Local
			}
		}
	}

	for _, attr := range attrs {
		if isNamespaceDeclarationAttribute(attr) || isMessageIDAttribute(attr) {
			continue
		}
		if attr.Name.Local == "" {
			return fmt.Errorf("reply attribute name must not be empty")
		}

		name := attr.Name.Local
		if attr.Name.Space != "" {
			prefix, ok := namespacePrefixes[attr.Name.Space]
			if !ok || prefix == "" {
				return fmt.Errorf("missing namespace declaration for reply attribute %s", attr.Name.Local)
			}
			name = prefix + ":" + attr.Name.Local
		}
		writeXMLAttribute(buf, name, attr.Value)
	}

	return nil
}

func validateNamespaceDeclarationAttr(attr xml.Attr) error {
	name, ok := namespaceDeclarationAttrName(attr)
	if !ok {
		return nil
	}
	switch {
	case name == "xmlns:xmlns":
		return fmt.Errorf("namespace prefix xmlns must not be declared")
	case name == "xmlns:xml" && attr.Value != xmlNamespace:
		return fmt.Errorf("namespace prefix xml must be bound to %s", xmlNamespace)
	case name != "xmlns:xml" && attr.Value == xmlNamespace:
		return fmt.Errorf("xml namespace must use xml prefix")
	case attr.Value == xmlnsNamespace:
		return fmt.Errorf("xmlns namespace must not be declared")
	default:
		return nil
	}
}
