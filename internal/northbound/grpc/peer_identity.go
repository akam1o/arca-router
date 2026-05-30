package grpc

import (
	"fmt"
	"net"
	"os/user"
	"strconv"
	"strings"
)

type peerIdentity struct {
	UID      uint32
	GID      uint32
	PID      int32
	Username string
}

type peerCredentialListener struct {
	net.Listener
}

func wrapPeerCredentialListener(lis net.Listener) net.Listener {
	if lis == nil || lis.Addr() == nil || !strings.HasPrefix(lis.Addr().Network(), "unix") {
		return lis
	}
	return &peerCredentialListener{Listener: lis}
}

func (l *peerCredentialListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	identity, ok := peerIdentityFromConn(conn)
	if !ok {
		return conn, nil
	}
	return &peerCredentialConn{
		Conn: conn,
		addr: peerCredentialAddr{
			Addr:     conn.RemoteAddr(),
			identity: identity,
		},
	}, nil
}

type peerCredentialConn struct {
	net.Conn
	addr peerCredentialAddr
}

func (c *peerCredentialConn) RemoteAddr() net.Addr {
	return c.addr
}

type peerCredentialAddr struct {
	net.Addr
	identity peerIdentity
}

func (a peerCredentialAddr) Network() string {
	if a.Addr != nil {
		return a.Addr.Network()
	}
	return "unix"
}

func (a peerCredentialAddr) String() string {
	base := "unix"
	if a.Addr != nil && a.Addr.String() != "" {
		base = a.Addr.String()
	}
	return fmt.Sprintf("%s uid=%d gid=%d pid=%d", base, a.identity.UID, a.identity.GID, a.identity.PID)
}

func (a peerCredentialAddr) peerUsername() string {
	return a.identity.Username
}

func usernameFromUID(uid uint32) string {
	uidText := strconv.FormatUint(uint64(uid), 10)
	u, err := user.LookupId(uidText)
	if err != nil || strings.TrimSpace(u.Username) == "" {
		return "uid:" + uidText
	}
	return strings.TrimSpace(u.Username)
}
