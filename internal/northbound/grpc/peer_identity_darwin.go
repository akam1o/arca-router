//go:build darwin

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
		cred *unix.Xucred
		cerr error
	)
	if err := rawConn.Control(func(fd uintptr) {
		cred, cerr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil || cerr != nil || cred == nil {
		return peerIdentity{}, false
	}

	uid := cred.Uid
	return peerIdentity{
		UID:      uid,
		GID:      peerGroupID(cred),
		PID:      0,
		Username: usernameFromUID(uid),
	}, true
}

func peerGroupID(cred *unix.Xucred) uint32 {
	if cred.Ngroups <= 0 {
		return 0
	}
	return cred.Groups[0]
}
