//go:build linux

package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRealUnixPeerAndSocketPermissions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.sock")
	ln, err := Listen(p)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0660 {
		t.Fatal("socket permissions", err)
	}
	if other, err := Listen(p); err == nil {
		other.Close()
		t.Fatal("active socket replaced")
	}
	server := &http.Server{ConnContext: ConnContext, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := peerUID(r.Context())
		if !ok || uid != uint32(os.Geteuid()) {
			http.Error(w, "peer mismatch", 403)
			return
		}
		fmt.Fprint(w, "peer verified")
	})}
	go server.Serve(ln)
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", p)
	}}}
	response, err := client.Get("http://unix/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || string(data) != "peer verified" {
		t.Fatal("SO_PEERCRED failed")
	}
}
