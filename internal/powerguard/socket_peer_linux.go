//go:build linux

package powerguard

import (
	"context"
	"net"
	"os/user"
	"strconv"
	"syscall"
)

// Group membership is deliberately insufficient: only root and the gateway UID
// may reach the management API. The gateway must overwrite the admin header.
func socketPeerContext(ctx context.Context, conn net.Conn) context.Context {
	trusted := false
	if unix, ok := conn.(*net.UnixConn); ok {
		raw, err := unix.SyscallConn()
		if err == nil {
			var cred *syscall.Ucred
			var credErr error
			err = raw.Control(func(fd uintptr) {
				cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
			})
			if err == nil && credErr == nil && cred != nil {
				trusted = cred.Uid == 0
				if gateway, err := user.Lookup("www-data"); err == nil {
					uid, err := strconv.ParseUint(gateway.Uid, 10, 32)
					trusted = trusted || (err == nil && uint32(uid) == cred.Uid)
				}
			}
		}
	}
	return context.WithValue(ctx, socketPeerKey{}, trusted)
}
