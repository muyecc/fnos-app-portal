//go:build linux

package gateway

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

type peerKey struct{}

func ConnContext(ctx context.Context, c net.Conn) context.Context {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return ctx
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return ctx
	}
	var cred *syscall.Ucred
	if raw.Control(func(fd uintptr) { cred, _ = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED) }) != nil || cred == nil {
		return ctx
	}
	return context.WithValue(ctx, peerKey{}, cred.Uid)
}

func peerUID(ctx context.Context) (uint32, bool) {
	v, ok := ctx.Value(peerKey{}).(uint32)
	return v, ok
}

func Listen(path string) (net.Listener, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("socket must be absolute")
	}
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refusing to replace a non-socket file")
		}
		if c, e := net.Dial("unix", path); e == nil {
			c.Close()
			return nil, errors.New("socket already active")
		}
		if err = os.Remove(path); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0660); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}
