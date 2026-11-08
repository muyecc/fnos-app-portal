//go:build !linux

package gateway

import (
	"context"
	"errors"
	"net"
)

type peerKey struct{}

func ConnContext(ctx context.Context, _ net.Conn) context.Context { return ctx }
func peerUID(ctx context.Context) (uint32, bool) {
	v, ok := ctx.Value(peerKey{}).(uint32)
	return v, ok
}
func Listen(string) (net.Listener, error) {
	return nil, errors.New("production requires Linux Unix sockets")
}
