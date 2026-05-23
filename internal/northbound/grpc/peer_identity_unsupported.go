//go:build !linux && !darwin

package grpc

import "net"

func peerIdentityFromConn(conn net.Conn) (peerIdentity, bool) {
	return peerIdentity{}, false
}
