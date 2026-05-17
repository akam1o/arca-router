package netconf

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

const (
	netconfNamespace = "urn:ietf:params:xml:ns:netconf:base:1.0"
	xmlNamespace     = "http://www.w3.org/XML/1998/namespace"
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
			Content: data,
		},
	}
}

// NewErrorReply creates an error <rpc-reply> with one <rpc-error>
func NewErrorReply(messageID string, err *RPCError) *RPCReply {
	return &RPCReply{
		MessageID: messageID,
		Errors:    []*RPCError{normalizeRPCError(err)},
	}
}

// NewMultiErrorReply creates an error <rpc-reply> with multiple <rpc-error>
func NewMultiErrorReply(messageID string, errors []*RPCError) *RPCReply {
	if len(errors) == 0 {
		errors = []*RPCError{nil}
	}
	normalized := make([]*RPCError, len(errors))
	for i, err := range errors {
		normalized[i] = normalizeRPCError(err)
	}
	return &RPCReply{
		MessageID: messageID,
		Errors:    normalized,
	}
}

func normalizeRPCError(err *RPCError) *RPCError {
	if err != nil {
		return err
	}
	return ErrOperationFailed("rpc error unavailable")
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
		data, err := xml.Marshal(rpcErr)
		if err != nil {
			return nil, err
		}
		buf.Write(data)
	}

	buf.WriteString("</rpc-reply>")
	return buf.Bytes(), nil
}

func writeReplyAttributes(buf *bytes.Buffer, attrs []xml.Attr) error {
	namespacePrefixes := map[string]string{
		xmlNamespace: "xml",
	}
	written := map[string]string{
		"xmlns": netconfNamespace,
	}

	for _, attr := range attrs {
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
