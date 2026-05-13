// Package mpls contains minimal bindings for the subset of mpls.api used by arca-router.
package mpls

import (
	api "go.fd.io/govpp/api"
	codec "go.fd.io/govpp/codec"

	"github.com/akam1o/arca-router/pkg/vpp/binapi/interface_types"
)

const (
	APIFile    = "mpls"
	APIVersion = "1.1.1"
	VersionCrc = 0xdf2aeee2
)

// SwInterfaceSetMplsEnable defines message 'sw_interface_set_mpls_enable'.
type SwInterfaceSetMplsEnable struct {
	SwIfIndex interface_types.InterfaceIndex `binapi:"interface_index,name=sw_if_index" json:"sw_if_index,omitempty"`
	Enable    bool                           `binapi:"bool,name=enable,default=true" json:"enable,omitempty"`
}

func (m *SwInterfaceSetMplsEnable) Reset()               { *m = SwInterfaceSetMplsEnable{} }
func (*SwInterfaceSetMplsEnable) GetMessageName() string { return "sw_interface_set_mpls_enable" }
func (*SwInterfaceSetMplsEnable) GetCrcString() string   { return "ae6cfcfb" }
func (*SwInterfaceSetMplsEnable) GetMessageType() api.MessageType {
	return api.RequestMessage
}

func (m *SwInterfaceSetMplsEnable) Size() (size int) {
	if m == nil {
		return 0
	}
	size += 4
	size += 1
	return size
}

func (m *SwInterfaceSetMplsEnable) Marshal(b []byte) ([]byte, error) {
	if b == nil {
		b = make([]byte, m.Size())
	}
	buf := codec.NewBuffer(b)
	buf.EncodeUint32(uint32(m.SwIfIndex))
	buf.EncodeBool(m.Enable)
	return buf.Bytes(), nil
}

func (m *SwInterfaceSetMplsEnable) Unmarshal(b []byte) error {
	buf := codec.NewBuffer(b)
	m.SwIfIndex = interface_types.InterfaceIndex(buf.DecodeUint32())
	m.Enable = buf.DecodeBool()
	return nil
}

// SwInterfaceSetMplsEnableReply defines message 'sw_interface_set_mpls_enable_reply'.
type SwInterfaceSetMplsEnableReply struct {
	Retval int32 `binapi:"i32,name=retval" json:"retval,omitempty"`
}

func (m *SwInterfaceSetMplsEnableReply) Reset() { *m = SwInterfaceSetMplsEnableReply{} }
func (*SwInterfaceSetMplsEnableReply) GetMessageName() string {
	return "sw_interface_set_mpls_enable_reply"
}
func (*SwInterfaceSetMplsEnableReply) GetCrcString() string { return "e8d4e804" }
func (*SwInterfaceSetMplsEnableReply) GetMessageType() api.MessageType {
	return api.ReplyMessage
}

func (m *SwInterfaceSetMplsEnableReply) Size() (size int) {
	if m == nil {
		return 0
	}
	size += 4
	return size
}

func (m *SwInterfaceSetMplsEnableReply) Marshal(b []byte) ([]byte, error) {
	if b == nil {
		b = make([]byte, m.Size())
	}
	buf := codec.NewBuffer(b)
	buf.EncodeInt32(m.Retval)
	return buf.Bytes(), nil
}

func (m *SwInterfaceSetMplsEnableReply) Unmarshal(b []byte) error {
	buf := codec.NewBuffer(b)
	m.Retval = buf.DecodeInt32()
	return nil
}

func init() {
	api.RegisterMessage((*SwInterfaceSetMplsEnable)(nil), "sw_interface_set_mpls_enable_ae6cfcfb")
	api.RegisterMessage((*SwInterfaceSetMplsEnableReply)(nil), "sw_interface_set_mpls_enable_reply_e8d4e804")
}
