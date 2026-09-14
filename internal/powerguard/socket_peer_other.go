//go:build !linux

package powerguard

import (
	"context"
	"net"
)

// Management sockets are supported only on the Linux deployment target.
func socketPeerContext(ctx context.Context, conn net.Conn) context.Context {
	return context.WithValue(ctx, socketPeerKey{}, false)
}
