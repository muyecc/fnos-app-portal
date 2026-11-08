package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, upstream http.Handler) (*Server, *httptest.Server) {
	t.Helper()
	backend := httptest.NewServer(upstream)
	t.Cleanup(backend.Close)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("portal-shell"), 0600); err != nil {
		t.Fatal(err)
	}
	s, e := New(Config{Prefix: "/app/example-portal", Upstream: backend.URL, StaticDir: dir, GatewayPeerUIDs: []uint32{0}, AdminsOnly: true, PublicHosts: []string{"portal.test"}, LeaseSeconds: 30})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s, backend
}
func req(path string, peer bool) *http.Request {
	r := httptest.NewRequest("GET", "http://portal.test"+path, nil)
	if peer {
		r = r.WithContext(context.WithValue(r.Context(), peerKey{}, uint32(0)))
	}
	r.Header.Set("X-Trim-Userid", "1000")
	r.Header.Set("X-Trim-Username", "admin")
	r.Header.Set("X-Trim-Isadmin", "true")
	return r
}
func session(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req("/app/example-portal/", true))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 || !cs[0].HttpOnly || cs[0].Path != "/app/example-portal/" {
		t.Fatal("missing scoped session cookie")
	}
	return cs[0]
}
func TestAuthorizationAndPaths(t *testing.T) {
	s, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		name, path string
		peer       bool
		mutate     func(*http.Request)
		code       int
	}{
		{"forged NAS identity without Unix peer", "/app/example-portal/", false, nil, 403},
		{"missing NAS identity", "/app/example-portal/", true, func(r *http.Request) { r.Header.Del("X-Trim-Userid") }, 403},
		{"ordinary NAS user", "/app/example-portal/", true, func(r *http.Request) { r.Header.Set("X-Trim-Isadmin", "false") }, 403},
		{"host not allowed", "/app/example-portal/", true, func(r *http.Request) { r.Host = "evil.test" }, 403},
		{"cross-origin", "/app/example-portal/", true, func(r *http.Request) { r.Header.Set("Origin", "https://evil.test") }, 403},
		{"prefix boundary", "/app/example-portal-bypass/", true, nil, 404},
		{"encoded traversal", "/app/example-portal/%2e%2e/config.json", true, nil, 400},
		{"API without portal session", "/app/example-portal/api/items", true, nil, 401},
		{"missing asset is not SPA", "/app/example-portal/assets/missing.js", true, nil, 404},
		{"source maps forbidden", "/app/example-portal/assets/app.js.map", true, nil, 404},
		{"SPA deep route", "/app/example-portal/settings/models", true, nil, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := req(tc.path, tc.peer)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable protected response")
			}
		})
	}
}
func TestProxyPreservesApplicationAuthAndDropsNASSecrets(t *testing.T) {
	var gotPath, gotAuth, gotCookie, gotNAS, gotQuery string
	s, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		gotNAS = r.Header.Get("X-Trim-Userid")
		gotQuery = r.URL.RawQuery
		w.WriteHeader(204)
	}))
	c := session(t, s)
	r := req("/app/example-portal/api/files/download?path=a%2Fb.md", true)
	r.AddCookie(c)
	r.AddCookie(&http.Cookie{Name: "NASSecret", Value: "never-forward"})
	r.Header.Set("Authorization", "Bearer example-token")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 204 || gotPath != "/api/files/download" || gotQuery != "path=a%2Fb.md" || gotAuth != "Bearer example-token" || gotCookie != "" || gotNAS != "" {
		t.Fatalf("incorrect proxy scope: %d path=%s query=%s", w.Code, gotPath, gotQuery)
	}
	// A NAS user cannot reuse a different user's portal cookie.
	r = req("/app/example-portal/api/items", true)
	r.Header.Set("X-Trim-Userid", "1001")
	r.AddCookie(c)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("cross-user lease accepted")
	}
}
func TestExpiredLeaseClosesWebSocketTunnel(t *testing.T) {
	s, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, b, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer c.Close()
		_, _ = b.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = b.Flush()
		_, _ = io.Copy(c, c)
	}))
	cookie := session(t, s)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerKey{}, uint32(0))))
	}))
	defer front.Close()
	c, e := net.Dial("tcp", front.Listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	_, e = fmt.Fprintf(c, "GET /app/example-portal/api/test/ws HTTP/1.1\r\nHost: portal.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nX-Trim-Userid: 1000\r\nX-Trim-Username: admin\r\nX-Trim-Isadmin: true\r\nCookie: %s=%s\r\n\r\n", cookie.Name, cookie.Value)
	if e != nil {
		t.Fatal(e)
	}
	b := bufio.NewReader(c)
	response, e := http.ReadResponse(b, nil)
	if e != nil || response.StatusCode != 101 {
		t.Fatalf("upgrade failed: %v", e)
	}
	_, _ = c.Write([]byte("ping"))
	reply := make([]byte, 4)
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = io.ReadFull(b, reply); e != nil || string(reply) != "ping" {
		t.Fatal("tunnel did not relay")
	}
	s.leases.mu.Lock()
	s.leases.values[cookie.Value].expires = time.Now().Add(-time.Second)
	s.leases.mu.Unlock()
	s.leases.expire(false)
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = b.ReadByte(); e == nil {
		t.Fatal("expired tunnel remained usable")
	}
}
func TestLeaseRenewalRequiresNASIdentity(t *testing.T) {
	s, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cookie := session(t, s)
	r := req("/app/example-portal/__portal/lease", false)
	r.Method = "POST"
	r.AddCookie(cookie)
	r.Header.Set("X-App-Portal", "lease")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("renewal accepted forged gateway identity")
	}
	r = req("/app/example-portal/__portal/lease", true)
	r.Method = "POST"
	r.AddCookie(cookie)
	r.Header.Set("X-App-Portal", "lease")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"portal_session":true`) {
		t.Fatal("authorized renewal failed")
	}
}
