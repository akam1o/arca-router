package netconf

import (
	"encoding/xml"
)

const (
	netconfNamespace = "urn:ietf:params:xml:ns:netconf:base:1.0"
)

// RPCReply represents a NETCONF <rpc-reply> envelope
type RPCReply struct {
	XMLName   xml.Name    `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc-reply"`
	MessageID string      `xml:"message-id,attr"`
	OK        *struct{}   `xml:"ok,omitempty"`
	Data      *DataReply  `xml:"data,omitempty"`
	Errors    []*RPCError `xml:"rpc-error,omitempty"`
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
		Errors:    []*RPCError{err},
	}
}

// NewMultiErrorReply creates an error <rpc-reply> with multiple <rpc-error>
func NewMultiErrorReply(messageID string, errors []*RPCError) *RPCReply {
	return &RPCReply{
		MessageID: messageID,
		Errors:    errors,
	}
}

// MarshalReply serializes RPCReply to XML bytes
func MarshalReply(reply *RPCReply) ([]byte, error) {
	data, err := xml.Marshal(reply)
	if err != nil {
		return nil, err
	}
	return data, nil
}
