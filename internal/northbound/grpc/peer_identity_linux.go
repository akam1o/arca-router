//go:build linux

package grpc

import (
	"net"

	"golang.org/x/sys/unix"
)

func peerIdentityFromConn(conn net.Conn) (peerIdentity, bool) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return peerIdentity{}, false
	}
	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return peerIdentity{}, false
	}

	var (
		cred *unix.Ucred
		cerr error
	)
	if err := rawConn.Control(func(fd uintptr) {
		cred, cerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || cerr != nil || cred == nil {
		return peerIdentity{}, false
	}

	uid := uint32(cred.Uid)
	return peerIdentity{
		UID:      uid,
		GID:      uint32(cred.Gid),
		PID:      cred.Pid,
		Username: usernameFromUID(uid),
	}, true
}
