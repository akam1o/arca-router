package vpp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/vpp/binapi/avf"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/ethernet_types"
	vppif "github.com/akam1o/arca-router/pkg/vpp/binapi/interface"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/interface_types"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/ip_types"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/rdma"
	"github.com/akam1o/arca-router/pkg/vpp/binapi/vpe"
	"go.fd.io/govpp/api"
)

// TestParsePCIAddress tests PCI address parsing
func TestParsePCIAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		want    uint32
		wantErr bool
	}{
		{
			name: "valid address",
			addr: "0000:00:06.0",
			want: 0x00000030, // domain(0) << 16 | bus(0) << 8 | slot(6) << 3 | func(0)
		},
		{
			name: "valid address with non-zero domain",
			addr: "0001:00:04.0",
			want: 0x00010020, // domain(1) << 16 | bus(0) << 8 | slot(4) << 3 | func(0)
		},
		{
			name: "valid address with function",
			addr: "0000:05:00.1",
			want: 0x00000501, // domain(0) << 16 | bus(5) << 8 | slot(0) << 3 | func(1)
		},
		{
			name:    "invalid format - missing colon",
			addr:    "000000:06.0",
			wantErr: true,
		},
		{
			name:    "invalid format - missing dot",
			addr:    "0000:00:060",
			wantErr: true,
		},
		{
			name:    "invalid domain",
			addr:    "ZZZZ:00:06.0",
			wantErr: true,
		},
		{
			name:    "invalid bus",
			addr:    "0000:ZZ:06.0",
			wantErr: true,
		},
		{
			name:    "invalid slot",
			addr:    "0000:00:ZZ.0",
			wantErr: true,
		},
		{
			name:    "invalid function",
			addr:    "0000:00:06.Z",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePCIAddress(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePCIAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parsePCIAddress() = 0x%08x, want 0x%08x", got, tt.want)
			}
		})
	}
}

// TestConvertToInterface tests interface conversion
func TestConvertToInterface(t *testing.T) {
	tests := []struct {
		name string
		msg  *vppif.SwInterfaceDetails
		want *Interface
	}{
		{
			name: "admin up and link up",
			msg: &vppif.SwInterfaceDetails{
				SwIfIndex:     1,
				InterfaceName: "test-if",
				Flags:         interface_types.IF_STATUS_API_FLAG_ADMIN_UP | interface_types.IF_STATUS_API_FLAG_LINK_UP,
				L2Address:     ethernet_types.MacAddress{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
			},
			want: &Interface{
				SwIfIndex: 1,
				Name:      "test-if",
				AdminUp:   true,
				LinkUp:    true,
				MAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
				Addresses: nil,
			},
		},
		{
			name: "admin down and link down",
			msg: &vppif.SwInterfaceDetails{
				SwIfIndex:     2,
				InterfaceName: "test-if-2",
				Flags:         0,
				L2Address:     ethernet_types.MacAddress{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
			},
			want: &Interface{
				SwIfIndex: 2,
				Name:      "test-if-2",
				AdminUp:   false,
				LinkUp:    false,
				MAC:       net.HardwareAddr{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
				Addresses: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertToInterface(tt.msg)
			if got.SwIfIndex != tt.want.SwIfIndex {
				t.Errorf("SwIfIndex = %d, want %d", got.SwIfIndex, tt.want.SwIfIndex)
			}
			if got.Name != tt.want.Name {
				t.Errorf("Name = %s, want %s", got.Name, tt.want.Name)
			}
			if got.AdminUp != tt.want.AdminUp {
				t.Errorf("AdminUp = %v, want %v", got.AdminUp, tt.want.AdminUp)
			}
			if got.LinkUp != tt.want.LinkUp {
				t.Errorf("LinkUp = %v, want %v", got.LinkUp, tt.want.LinkUp)
			}
			if got.MAC.String() != tt.want.MAC.String() {
				t.Errorf("MAC = %s, want %s", got.MAC.String(), tt.want.MAC.String())
			}
		})
	}
}

// TestCheckSocketAccess tests socket access checking
func TestCheckSocketAccess(t *testing.T) {
	// Create a temporary directory for test socket in the current working directory.
	// Note: In sandboxed environments, os.TempDir()/t.TempDir() may be outside writable roots.
	tmpDir, err := os.MkdirTemp(".", "govpp-client-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Test non-existent file
	err = checkSocketAccess(socketPath)
	if err == nil {
		t.Error("checkSocketAccess() expected error for non-existent socket, got nil")
	}

	// Create a socket
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("Skipping unix socket bind in restricted environment: %v", err)
		}
		t.Fatalf("Failed to create test socket: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			_ = err
		}
	}()

	// Test valid socket
	err = checkSocketAccess(socketPath)
	if err != nil {
		t.Errorf("checkSocketAccess() error = %v, want nil", err)
	}

	// Test non-socket file
	regularFile := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create regular file: %v", err)
	}

	err = checkSocketAccess(regularFile)
	if err == nil {
		t.Error("checkSocketAccess() expected error for non-socket file, got nil")
	}
}

// Fake implementations for testing

// fakeRequestCtx is a fake implementation of api.RequestCtx
type fakeRequestCtx struct {
	reply any
	err   error
}

func (f *fakeRequestCtx) ReceiveReply(msg api.Message) error {
	if f.err != nil {
		return f.err
	}
	// Copy reply to msg with type validation
	switch r := f.reply.(type) {
	case *avf.AvfCreateReply:
		if _, ok := msg.(*avf.AvfCreateReply); !ok {
			return fmt.Errorf("unexpected message type: expected *avf.AvfCreateReply, got %T", msg)
		}
		*msg.(*avf.AvfCreateReply) = *r
	case *rdma.RdmaCreateV4Reply:
		if _, ok := msg.(*rdma.RdmaCreateV4Reply); !ok {
			return fmt.Errorf("unexpected message type: expected *rdma.RdmaCreateV4Reply, got %T", msg)
		}
		*msg.(*rdma.RdmaCreateV4Reply) = *r
	case *vppif.SwInterfaceSetFlagsReply:
		if _, ok := msg.(*vppif.SwInterfaceSetFlagsReply); !ok {
			return fmt.Errorf("unexpected message type: expected *vppif.SwInterfaceSetFlagsReply, got %T", msg)
		}
		*msg.(*vppif.SwInterfaceSetFlagsReply) = *r
	case *vppif.SwInterfaceAddDelAddressReply:
		if _, ok := msg.(*vppif.SwInterfaceAddDelAddressReply); !ok {
			return fmt.Errorf("unexpected message type: expected *vppif.SwInterfaceAddDelAddressReply, got %T", msg)
		}
		*msg.(*vppif.SwInterfaceAddDelAddressReply) = *r
	case *vpe.ShowVersionReply:
		if _, ok := msg.(*vpe.ShowVersionReply); !ok {
			return fmt.Errorf("unexpected message type: expected *vpe.ShowVersionReply, got %T", msg)
		}
		*msg.(*vpe.ShowVersionReply) = *r
	default:
		return fmt.Errorf("unsupported reply type in fake: %T", f.reply)
	}
	return nil
}

// fakeMultiRequestCtx is a fake implementation of api.MultiRequestCtx
type fakeMultiRequestCtx struct {
	replies []api.Message
	index   int
	err     error
}

func (f *fakeMultiRequestCtx) ReceiveReply(msg api.Message) (bool, error) {
	if f.err != nil {
		return true, f.err
	}
	if f.index >= len(f.replies) {
		return true, nil // Stop iteration (no more data)
	}

	// Copy current reply to msg with type validation
	switch r := f.replies[f.index].(type) {
	case *vppif.SwInterfaceDetails:
		details, ok := msg.(*vppif.SwInterfaceDetails)
		if !ok {
			return true, fmt.Errorf("unexpected message type: expected *vppif.SwInterfaceDetails, got %T", msg)
		}
		*details = *r
	default:
		return true, fmt.Errorf("unsupported reply type in fake: %T", r)
	}

	f.index++

	// Real govpp behavior: return stop=false with data, caller checks again, then returns stop=true
	// This matches the loop pattern in GetInterface/ListInterfaces where stop is checked AFTER processing
	return false, nil
}

// fakeChannel is a fake implementation of api.Channel
type fakeChannel struct {
	sendRequestFunc      func(api.Message) api.RequestCtx
	sendMultiRequestFunc func(api.Message) api.MultiRequestCtx
	closed               bool
}

func (f *fakeChannel) SendRequest(msg api.Message) api.RequestCtx {
	if f.sendRequestFunc != nil {
		return f.sendRequestFunc(msg)
	}
	return &fakeRequestCtx{}
}

func (f *fakeChannel) SendMultiRequest(msg api.Message) api.MultiRequestCtx {
	if f.sendMultiRequestFunc != nil {
		return f.sendMultiRequestFunc(msg)
	}
	return &fakeMultiRequestCtx{}
}

func (f *fakeChannel) SetReplyTimeout(timeout time.Duration) {
	// No-op for fake
}

func (f *fakeChannel) CheckCompatiblity(msgs ...api.Message) error {
	return nil
}

func (f *fakeChannel) SubscribeNotification(notifChan chan api.Message, event api.Message) (api.SubscriptionCtx, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeChannel) GetRequestCtx() api.RequestCtx {
	return nil
}

func (f *fakeChannel) Close() {
	f.closed = true
}

// TestGovppClient_CreateInterface_NilRequest tests CreateInterface with nil request
func TestGovppClient_CreateInterface_NilRequest(t *testing.T) {
	client := &govppClient{
		ch: &fakeChannel{},
	}

	ctx := context.Background()
	_, err := client.CreateInterface(ctx, nil)
	if err == nil {
		t.Error("CreateInterface(nil) expected error, got nil")
	}
}

// TestGovppClient_CreateInterface_NotConnected tests CreateInterface when not connected
func TestGovppClient_CreateInterface_NotConnected(t *testing.T) {
	client := &govppClient{}

	ctx := context.Background()
	_, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
	if err == nil {
		t.Error("CreateInterface() expected error when not connected, got nil")
	}
}

// TestGovppClient_CreateInterface_UnsupportedType tests CreateInterface with unsupported type
func TestGovppClient_CreateInterface_UnsupportedType(t *testing.T) {
	client := &govppClient{
		ch: &fakeChannel{},
	}

	ctx := context.Background()
	_, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: "unsupported"})
	if err == nil {
		t.Error("CreateInterface() expected error for unsupported type, got nil")
	}
}

// TestGovppClient_CreateInterface_AVF tests AVF interface creation
func TestGovppClient_CreateInterface_AVF(t *testing.T) {
	expectedSwIfIndex := interface_types.InterfaceIndex(1)

	fakeChannel := &fakeChannel{
		sendRequestFunc: func(msg api.Message) api.RequestCtx {
			switch msg.(type) {
			case *avf.AvfCreate:
				return &fakeRequestCtx{
					reply: &avf.AvfCreateReply{
						SwIfIndex: expectedSwIfIndex,
						Retval:    0,
					},
				}
			}
			return &fakeRequestCtx{err: fmt.Errorf("unexpected message type")}
		},
		sendMultiRequestFunc: func(msg api.Message) api.MultiRequestCtx {
			// Create new instance for each call to reset index
			return &fakeMultiRequestCtx{
				replies: []api.Message{
					&vppif.SwInterfaceDetails{
						SwIfIndex:     expectedSwIfIndex,
						InterfaceName: "AVF0/0/6/0",
						Flags:         0,
						L2Address:     ethernet_types.MacAddress{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
					},
				},
				index: 0,
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type:           InterfaceTypeAVF,
		DeviceInstance: "0000:00:06.0",
		NumRxQueues:    1,
		RxqSize:        1024,
		TxqSize:        1024,
	})

	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	if iface == nil {
		t.Fatal("CreateInterface() returned nil interface")
	}

	if iface.SwIfIndex != uint32(expectedSwIfIndex) {
		t.Errorf("SwIfIndex = %d, want %d", iface.SwIfIndex, expectedSwIfIndex)
	}
}

// TestGovppClient_CreateInterface_RDMA tests RDMA interface creation
func TestGovppClient_CreateInterface_RDMA(t *testing.T) {
	expectedSwIfIndex := interface_types.InterfaceIndex(2)

	fakeChannel := &fakeChannel{
		sendRequestFunc: func(msg api.Message) api.RequestCtx {
			switch msg.(type) {
			case *rdma.RdmaCreateV4:
				return &fakeRequestCtx{
					reply: &rdma.RdmaCreateV4Reply{
						SwIfIndex: expectedSwIfIndex,
						Retval:    0,
					},
				}
			}
			return &fakeRequestCtx{err: fmt.Errorf("unexpected message type")}
		},
		sendMultiRequestFunc: func(msg api.Message) api.MultiRequestCtx {
			// Create new instance for each call to reset index
			return &fakeMultiRequestCtx{
				replies: []api.Message{
					&vppif.SwInterfaceDetails{
						SwIfIndex:     expectedSwIfIndex,
						InterfaceName: "rdma-0",
						Flags:         0,
						L2Address:     ethernet_types.MacAddress{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
					},
				},
				index: 0,
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type:           InterfaceTypeRDMA,
		DeviceInstance: "eth1",
		Name:           "rdma-0",
		NumRxQueues:    2,
		RxqSize:        2048,
		TxqSize:        2048,
	})

	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	if iface == nil {
		t.Fatal("CreateInterface() returned nil interface")
	}

	if iface.SwIfIndex != uint32(expectedSwIfIndex) {
		t.Errorf("SwIfIndex = %d, want %d", iface.SwIfIndex, expectedSwIfIndex)
	}
}

// TestGovppClient_SetInterfaceUp tests setting interface up
func TestGovppClient_SetInterfaceUp(t *testing.T) {
	fakeChannel := &fakeChannel{
		sendRequestFunc: func(msg api.Message) api.RequestCtx {
			return &fakeRequestCtx{
				reply: &vppif.SwInterfaceSetFlagsReply{
					Retval: 0,
				},
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	err := client.SetInterfaceUp(ctx, 1)
	if err != nil {
		t.Errorf("SetInterfaceUp() error = %v, want nil", err)
	}
}

// TestGovppClient_SetInterfaceDown tests setting interface down
func TestGovppClient_SetInterfaceDown(t *testing.T) {
	fakeChannel := &fakeChannel{
		sendRequestFunc: func(msg api.Message) api.RequestCtx {
			return &fakeRequestCtx{
				reply: &vppif.SwInterfaceSetFlagsReply{
					Retval: 0,
				},
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	err := client.SetInterfaceDown(ctx, 1)
	if err != nil {
		t.Errorf("SetInterfaceDown() error = %v, want nil", err)
	}
}

// TestGovppClient_SetInterfaceAddress_IPv4 tests setting IPv4 address
func TestGovppClient_SetInterfaceAddress_IPv4(t *testing.T) {
	fakeChannel := &fakeChannel{
		sendRequestFunc: func(msg api.Message) api.RequestCtx {
			req, ok := msg.(*vppif.SwInterfaceAddDelAddress)
			if !ok {
				return &fakeRequestCtx{err: fmt.Errorf("unexpected message type")}
			}

			// Verify IsAdd is true
			if !req.IsAdd {
				return &fakeRequestCtx{err: fmt.Errorf("expected IsAdd=true")}
			}

			return &fakeRequestCtx{
				reply: &vppif.SwInterfaceAddDelAddressReply{
					Retval: 0,
				},
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	_, ipnet, _ := net.ParseCIDR("192.168.1.1/24")
	err := client.SetInterfaceAddress(ctx, 1, ipnet)
	if err != nil {
		t.Errorf("SetInterfaceAddress() error = %v, want nil", err)
	}
}

// TestGovppClient_SetInterfaceAddress_IPv6 tests setting IPv6 address
func TestGovppClient_SetInterfaceAddress_IPv6(t *testing.T) {
	fakeChannel := &fakeChannel{
		sendRequestFunc: func(msg api.Message) api.RequestCtx {
			return &fakeRequestCtx{
				reply: &vppif.SwInterfaceAddDelAddressReply{
					Retval: 0,
				},
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	_, ipnet, _ := net.ParseCIDR("2001:db8::1/64")
	err := client.SetInterfaceAddress(ctx, 1, ipnet)
	if err != nil {
		t.Errorf("SetInterfaceAddress() error = %v, want nil", err)
	}
}

// TestGovppClient_SetInterfaceAddress_NilAddress tests setting nil address
func TestGovppClient_SetInterfaceAddress_NilAddress(t *testing.T) {
	client := &govppClient{
		ch: &fakeChannel{},
	}

	ctx := context.Background()
	err := client.SetInterfaceAddress(ctx, 1, nil)
	if err == nil {
		t.Error("SetInterfaceAddress(nil) expected error, got nil")
	}
}

// TestGovppClient_DeleteInterfaceAddress tests deleting interface address
func TestGovppClient_DeleteInterfaceAddress(t *testing.T) {
	fakeChannel := &fakeChannel{
		sendRequestFunc: func(msg api.Message) api.RequestCtx {
			req, ok := msg.(*vppif.SwInterfaceAddDelAddress)
			if !ok {
				return &fakeRequestCtx{err: fmt.Errorf("unexpected message type")}
			}

			// Verify IsAdd is false
			if req.IsAdd {
				return &fakeRequestCtx{err: fmt.Errorf("expected IsAdd=false")}
			}

			return &fakeRequestCtx{
				reply: &vppif.SwInterfaceAddDelAddressReply{
					Retval: 0,
				},
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	_, ipnet, _ := net.ParseCIDR("192.168.1.1/24")
	err := client.DeleteInterfaceAddress(ctx, 1, ipnet)
	if err != nil {
		t.Errorf("DeleteInterfaceAddress() error = %v, want nil", err)
	}
}

// TestGovppClient_GetInterface tests getting interface details
func TestGovppClient_GetInterface(t *testing.T) {
	expectedIfIndex := interface_types.InterfaceIndex(1)

	fakeChannel := &fakeChannel{
		sendMultiRequestFunc: func(msg api.Message) api.MultiRequestCtx {
			return &fakeMultiRequestCtx{
				replies: []api.Message{
					&vppif.SwInterfaceDetails{
						SwIfIndex:     expectedIfIndex,
						InterfaceName: "test-if",
						Flags:         interface_types.IF_STATUS_API_FLAG_ADMIN_UP,
						L2Address:     ethernet_types.MacAddress{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
					},
				},
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	iface, err := client.GetInterface(ctx, uint32(expectedIfIndex))
	if err != nil {
		t.Fatalf("GetInterface() error = %v, want nil", err)
	}

	if iface.SwIfIndex != uint32(expectedIfIndex) {
		t.Errorf("SwIfIndex = %d, want %d", iface.SwIfIndex, expectedIfIndex)
	}

	if iface.Name != "test-if" {
		t.Errorf("Name = %s, want test-if", iface.Name)
	}

	if !iface.AdminUp {
		t.Error("AdminUp = false, want true")
	}
}

// TestGovppClient_GetInterface_NotFound tests getting non-existent interface
func TestGovppClient_GetInterface_NotFound(t *testing.T) {
	fakeChannel := &fakeChannel{
		sendMultiRequestFunc: func(msg api.Message) api.MultiRequestCtx {
			return &fakeMultiRequestCtx{
				replies: []api.Message{
					&vppif.SwInterfaceDetails{
						SwIfIndex:     interface_types.InterfaceIndex(1),
						InterfaceName: "test-if",
					},
				},
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	_, err := client.GetInterface(ctx, 999)
	if err == nil {
		t.Error("GetInterface() expected error for non-existent interface, got nil")
	}
}

// TestGovppClient_ListInterfaces tests listing all interfaces
func TestGovppClient_ListInterfaces(t *testing.T) {
	fakeChannel := &fakeChannel{
		sendMultiRequestFunc: func(msg api.Message) api.MultiRequestCtx {
			return &fakeMultiRequestCtx{
				replies: []api.Message{
					&vppif.SwInterfaceDetails{
						SwIfIndex:     1,
						InterfaceName: "test-if-1",
						Flags:         interface_types.IF_STATUS_API_FLAG_ADMIN_UP,
					},
					&vppif.SwInterfaceDetails{
						SwIfIndex:     2,
						InterfaceName: "test-if-2",
						Flags:         0,
					},
				},
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	ctx := context.Background()
	interfaces, err := client.ListInterfaces(ctx)
	if err != nil {
		t.Fatalf("ListInterfaces() error = %v, want nil", err)
	}

	if len(interfaces) != 2 {
		t.Fatalf("len(interfaces) = %d, want 2", len(interfaces))
	}

	if interfaces[0].SwIfIndex != 1 {
		t.Errorf("interfaces[0].SwIfIndex = %d, want 1", interfaces[0].SwIfIndex)
	}

	if interfaces[1].SwIfIndex != 2 {
		t.Errorf("interfaces[1].SwIfIndex = %d, want 2", interfaces[1].SwIfIndex)
	}
}

// TestGovppClient_Close tests closing the client
func TestGovppClient_Close(t *testing.T) {
	fakeChannel := &fakeChannel{}
	client := &govppClient{
		ch: fakeChannel,
	}

	err := client.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}

	if !fakeChannel.closed {
		t.Error("Channel was not closed")
	}

	if client.ch != nil {
		t.Error("ch should be nil after Close()")
	}
}

// TestGovppClient_Close_AlreadyClosed tests closing an already closed client
func TestGovppClient_Close_AlreadyClosed(t *testing.T) {
	client := &govppClient{}

	// Close should not panic when already closed
	err := client.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

// TestGovppClient_ContextCancellation tests context cancellation
func TestGovppClient_ContextCancellation(t *testing.T) {
	requestSent := false
	fakeChannel := &fakeChannel{
		sendRequestFunc: func(msg api.Message) api.RequestCtx {
			requestSent = true
			t.Error("SendRequest should not be called when context is cancelled")
			return &fakeRequestCtx{
				reply: &avf.AvfCreateReply{
					SwIfIndex: 1,
					Retval:    0,
				},
			}
		},
	}

	client := &govppClient{
		ch: fakeChannel,
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type:           InterfaceTypeAVF,
		DeviceInstance: "0000:00:06.0",
	})

	if err == nil {
		t.Error("CreateInterface() expected error for cancelled context, got nil")
	}

	if requestSent {
		t.Error("Request was sent despite cancelled context")
	}

	// Verify error is context.Canceled
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}

// TestGovppClient_APIReturnError tests API returning error code
func TestGovppClient_APIReturnError(t *testing.T) {
	tests := []struct {
		name      string
		operation func(client *govppClient, ctx context.Context) error
		retval    int32
	}{
		{
			name: "CreateInterface AVF error",
			operation: func(client *govppClient, ctx context.Context) error {
				_, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
					Type:           InterfaceTypeAVF,
					DeviceInstance: "0000:00:06.0",
				})
				return err
			},
			retval: -1,
		},
		{
			name: "SetInterfaceUp error",
			operation: func(client *govppClient, ctx context.Context) error {
				return client.SetInterfaceUp(ctx, 1)
			},
			retval: -1,
		},
		{
			name: "SetInterfaceAddress error",
			operation: func(client *govppClient, ctx context.Context) error {
				_, ipnet, _ := net.ParseCIDR("192.168.1.1/24")
				return client.SetInterfaceAddress(ctx, 1, ipnet)
			},
			retval: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeChannel := &fakeChannel{
				sendRequestFunc: func(msg api.Message) api.RequestCtx {
					switch msg.(type) {
					case *avf.AvfCreate:
						return &fakeRequestCtx{
							reply: &avf.AvfCreateReply{
								Retval: tt.retval,
							},
						}
					case *vppif.SwInterfaceSetFlags:
						return &fakeRequestCtx{
							reply: &vppif.SwInterfaceSetFlagsReply{
								Retval: tt.retval,
							},
						}
					case *vppif.SwInterfaceAddDelAddress:
						return &fakeRequestCtx{
							reply: &vppif.SwInterfaceAddDelAddressReply{
								Retval: tt.retval,
							},
						}
					}
					return &fakeRequestCtx{err: fmt.Errorf("unexpected message type")}
				},
			}

			client := &govppClient{
				ch: fakeChannel,
			}

			ctx := context.Background()
			err := tt.operation(client, ctx)
			if err == nil {
				t.Errorf("%s expected error for retval=%d, got nil", tt.name, tt.retval)
			}
		})
	}
}

// TestGovppClient_NewAddressWithPrefix tests IP address normalization
func TestGovppClient_NewAddressWithPrefix(t *testing.T) {
	tests := []struct {
		name      string
		cidr      string
		wantError bool
	}{
		{
			name:      "IPv4 address",
			cidr:      "192.168.1.1/24",
			wantError: false,
		},
		{
			name:      "IPv6 address",
			cidr:      "2001:db8::1/64",
			wantError: false,
		},
		{
			name:      "IPv4-mapped IPv6 address",
			cidr:      "::ffff:192.168.1.1/120",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ipnet, err := net.ParseCIDR(tt.cidr)
			if err != nil {
				t.Fatalf("ParseCIDR() error = %v", err)
			}

			// Normalize address (same logic as in SetInterfaceAddress)
			normalizedAddr := *ipnet
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				normalizedAddr.IP = ip4
			} else if ip6 := ipnet.IP.To16(); ip6 != nil {
				normalizedAddr.IP = ip6
			} else {
				if !tt.wantError {
					t.Error("IP normalization failed unexpectedly")
				}
				return
			}

			// Verify NewAddressWithPrefix doesn't panic
			prefix := ip_types.NewAddressWithPrefix(normalizedAddr)
			if prefix.Len == 0 && !tt.wantError {
				t.Error("NewAddressWithPrefix() returned zero-length prefix")
			}
		})
	}
}

// TestCheckVersionCompatibility tests VPP version compatibility checking
func TestCheckVersionCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		retval      int32
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid version 24.10.0",
			version: "24.10.0",
			retval:  0,
			wantErr: false,
		},
		{
			name:    "valid version 24.10.1",
			version: "24.10.1",
			retval:  0,
			wantErr: false,
		},
		{
			name:    "valid version with v prefix",
			version: "v24.10.0",
			retval:  0,
			wantErr: false,
		},
		{
			name:    "valid version with -rc suffix",
			version: "24.10-rc0",
			retval:  0,
			wantErr: false,
		},
		{
			name:    "valid version with -rc and build meta",
			version: "24.10-rc0~123-gabc",
			retval:  0,
			wantErr: false,
		},
		{
			name:    "valid version with ~ build meta",
			version: "24.10.0~456-gdef",
			retval:  0,
			wantErr: false,
		},
		{
			name:        "version mismatch - major",
			version:     "25.10.0",
			retval:      0,
			wantErr:     true,
			errContains: "incompatible",
		},
		{
			name:        "version mismatch - minor",
			version:     "24.06.0",
			retval:      0,
			wantErr:     true,
			errContains: "incompatible",
		},
		{
			name:        "empty version string",
			version:     "",
			retval:      0,
			wantErr:     true,
			errContains: "empty version string",
		},
		{
			name:        "invalid version format",
			version:     "invalid",
			retval:      0,
			wantErr:     true,
			errContains: "invalid VPP version format",
		},
		{
			name:        "invalid major version",
			version:     "XX.10.0",
			retval:      0,
			wantErr:     true,
			errContains: "invalid VPP major version",
		},
		{
			name:        "invalid minor version",
			version:     "24.XX.0",
			retval:      0,
			wantErr:     true,
			errContains: "invalid VPP minor version",
		},
		{
			name:        "API error code",
			version:     "24.10.0",
			retval:      -1,
			wantErr:     true,
			errContains: "error code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake channel that returns ShowVersionReply
			reply := &vpe.ShowVersionReply{
				Retval:  tt.retval,
				Version: tt.version,
				Program: "vpe",
			}

			ch := &fakeChannel{
				sendRequestFunc: func(msg api.Message) api.RequestCtx {
					return &fakeRequestCtx{
						reply: reply,
					}
				},
			}

			client := &govppClient{
				ch: ch,
			}

			err := client.checkVersionCompatibility()

			if (err != nil) != tt.wantErr {
				t.Errorf("checkVersionCompatibility() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("checkVersionCompatibility() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

// TestCheckVersionCompatibility_APIError tests API call failure
func TestCheckVersionCompatibility_APIError(t *testing.T) {
	ch := &fakeChannel{
		sendRequestFunc: func(msg api.Message) api.RequestCtx {
			return &fakeRequestCtx{
				err: errors.New("API call failed"),
			}
		},
	}

	client := &govppClient{
		ch: ch,
	}

	err := client.checkVersionCompatibility()
	if err == nil {
		t.Error("checkVersionCompatibility() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to get VPP version") {
		t.Errorf("checkVersionCompatibility() error = %v, want error containing 'failed to get VPP version'", err)
	}
}
