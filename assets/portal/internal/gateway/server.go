package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cookieName = "AppPortalSession"

type Config struct {
	Prefix          string   `json:"prefix"`
	Socket          string   `json:"socket"`
	Upstream        string   `json:"upstream"`
	StaticDir       string   `json:"static_dir"`
	FrontendVersion string   `json:"frontend_version"`
	GatewayPeerUIDs []uint32 `json:"gateway_peer_uids"`
	AllowedUserIDs  []string `json:"allowed_user_ids"`
	AdminsOnly      bool     `json:"admins_only"`
	PublicHosts     []string `json:"public_hosts"`
	AllowedOrigins  []string `json:"allowed_origins"`
	HTTPCookieIPs   []string `json:"http_cookie_ips"`
	SecureCookie    bool     `json:"secure_cookie"`
	LeaseSeconds    int      `json:"lease_seconds"`
}
type Server struct {
	cfg    Config
	proxy  *httputil.ReverseProxy
	leases *leases
	done   chan struct{}
	once   sync.Once
}

func New(cfg Config) (*Server, error) {
	if !strings.HasPrefix(cfg.Prefix, "/app/") || strings.HasSuffix(cfg.Prefix, "/") || path.Clean(cfg.Prefix) != cfg.Prefix {
		return nil, errors.New("invalid gateway prefix")
	}
	if len(cfg.GatewayPeerUIDs) == 0 || len(cfg.PublicHosts) == 0 || (!cfg.AdminsOnly && len(cfg.AllowedUserIDs) == 0) {
		return nil, errors.New("explicit peer, host and user policy required")
	}
	if err := validateAccessConfig(cfg); err != nil {
		return nil, err
	}
	u, err := url.Parse(cfg.Upstream)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.Path != "" || u.User != nil {
		return nil, errors.New("invalid local upstream")
	}
	if cfg.LeaseSeconds < 10 || cfg.LeaseSeconds > 60 {
		return nil, errors.New("lease must be 10..60 seconds")
	}
	st, err := os.Stat(filepath.Join(cfg.StaticDir, "index.html"))
	if err != nil || !st.Mode().IsRegular() {
		return nil, errors.New("frontend index missing")
	}
	s := &Server{cfg: cfg, leases: newLeases(time.Duration(cfg.LeaseSeconds) * time.Second), done: make(chan struct{})}
	s.proxy = &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(u)
		pr.Out.Host = pr.In.Host
		pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, cfg.Prefix)
		if pr.In.URL.RawPath != "" {
			pr.Out.URL.RawPath = strings.TrimPrefix(pr.In.URL.RawPath, cfg.Prefix)
		}
		pr.SetXForwarded()
		pr.Out.Header.Set("X-Forwarded-Prefix", cfg.Prefix)
		// NAS session credentials and identity do not belong to the Octop backend.
		pr.Out.Header.Del("Cookie")
		for k := range pr.Out.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-trim-") {
				pr.Out.Header.Del(k)
			}
		}
	}, Transport: &http.Transport{Proxy: nil, MaxIdleConns: 64, IdleConnTimeout: 60 * time.Second, ResponseHeaderTimeout: 300 * time.Second}, FlushInterval: -1,
		ModifyResponse: func(r *http.Response) error {
			r.Header.Set("Cache-Control", "no-store")
			r.Header.Del("Set-Cookie")
			if loc := r.Header.Get("Location"); strings.HasPrefix(loc, "/api/") {
				r.Header.Set("Location", cfg.Prefix+loc)
			}
			return nil
		}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
			if !errors.Is(e, context.Canceled) {
				log.Printf("upstream unavailable method=%s", r.Method)
			}
			http.Error(w, "Octop backend unavailable", http.StatusBadGateway)
		}}
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.leases.expire(false)
			case <-s.done:
				return
			}
		}
	}()
	return s, nil
}
func (s *Server) Close() { s.once.Do(func() { close(s.done); s.leases.expire(true) }) }
func contains[T comparable](xs []T, x T) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func (s *Server) identity(r *http.Request) (string, bool) {
	p, ok := peerUID(r.Context())
	if !ok || !contains(s.cfg.GatewayPeerUIDs, p) {
		return "", false
	}
	if !contains(s.cfg.PublicHosts, strings.ToLower(r.Host)) {
		return "", false
	}
	uid := r.Header.Get("X-Trim-Userid")
	if _, err := strconv.ParseUint(uid, 10, 32); err != nil {
		return "", false
	}
	if r.Header.Get("X-Trim-Username") == "" {
		return "", false
	}
	allowed := contains(s.cfg.AllowedUserIDs, uid) || s.cfg.AdminsOnly && r.Header.Get("X-Trim-Isadmin") == "true"
	return uid, allowed
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'; base-uri 'self'; object-src 'none'")
	if r.URL.Path == "/__health" {
		if uid, ok := peerUID(r.Context()); ok && uid == uint32(os.Geteuid()) {
			w.WriteHeader(200)
			_, _ = io.WriteString(w, "ok")
			return
		}
		http.Error(w, "Forbidden", 403)
		return
	}
	uid, ok := s.identity(r)
	if !ok {
		if peer, exists := peerUID(r.Context()); exists {
			log.Printf("gateway request denied peer_uid=%d host=%s", peer, r.Host)
		}
		http.Error(w, "NAS authorization required", 403)
		return
	}
	if r.URL.Path != s.cfg.Prefix && !strings.HasPrefix(r.URL.Path, s.cfg.Prefix+"/") {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, s.cfg.Prefix)
	for _, seg := range strings.Split(rel, "/") {
		if seg == "." || seg == ".." || strings.Contains(seg, "\\") {
			http.Error(w, "Invalid path", 400)
			return
		}
	}
	if strings.Contains(rel, "//") {
		http.Error(w, "Invalid path", 400)
		return
	}
	if r.URL.Path == s.cfg.Prefix {
		http.Redirect(w, r, s.cfg.Prefix+"/", http.StatusTemporaryRedirect)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		if !s.originAllowed(r, origin) {
			http.Error(w, "Origin rejected", 403)
			return
		}
	}
	key := ""
	sessionName, _ := s.cookiePolicy(r)
	if c, e := r.Cookie(sessionName); e == nil {
		key = c.Value
	}
	if rel == "/__portal/lease" {
		if r.Method != "POST" || r.Header.Get("X-App-Portal") != "lease" {
			http.Error(w, "Invalid lease request", 403)
			return
		}
		s.renew(w, r, key, uid)
		return
	}
	if strings.HasPrefix(rel, "/api/") {
		if !s.leases.valid(key, uid) {
			http.Error(w, "Portal session expired", 401)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		detach, ok := s.leases.attach(key, uid, cancel)
		if !ok {
			http.Error(w, "Portal session expired", 401)
			return
		}
		defer detach()
		s.proxy.ServeHTTP(&leaseWriter{ResponseWriter: w, leases: s.leases, key: key, uid: uid}, r.WithContext(ctx))
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	name := strings.TrimPrefix(rel, "/")
	if !fs.ValidPath(name) && name != "" {
		http.NotFound(w, r)
		return
	}
	if strings.Contains(name, ".map") || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".sock") {
		http.NotFound(w, r)
		return
	}
	file := filepath.Join(s.cfg.StaticDir, filepath.FromSlash(name))
	info, err := os.Stat(file)
	if err != nil || info.IsDir() {
		if name != "" && (strings.HasPrefix(name, "assets/") || path.Ext(name) != "") {
			http.NotFound(w, r)
			return
		}
		file = filepath.Join(s.cfg.StaticDir, "index.html")
		info, err = os.Stat(file)
	}
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if filepath.Base(file) == "index.html" {
		if _, e := s.renewCookie(w, r, key, uid); e != nil {
			http.Error(w, "Session unavailable", 500)
			return
		}
	}
	f, e := os.Open(file)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if ct := mime.TypeByExtension(filepath.Ext(file)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
func (s *Server) renewCookie(w http.ResponseWriter, r *http.Request, key, uid string) (string, error) {
	key, e := s.leases.renew(key, uid)
	if e != nil {
		return "", e
	}
	name, secure := s.cookiePolicy(r)
	http.SetCookie(w, &http.Cookie{Name: name, Value: key, Path: s.cfg.Prefix + "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	return key, nil
}
func (s *Server) renew(w http.ResponseWriter, r *http.Request, key, uid string) {
	if _, e := s.renewCookie(w, r, key, uid); e != nil {
		http.Error(w, "Session unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"portal_session": true, "frontend_version": s.cfg.FrontendVersion, "lease_seconds": s.cfg.LeaseSeconds})
}

type leaseWriter struct {
	http.ResponseWriter
	leases   *leases
	key, uid string
}

func (w *leaseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *leaseWriter) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *leaseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, b, e := http.NewResponseController(w.ResponseWriter).Hijack()
	if e != nil {
		return nil, nil, e
	}
	detach, ok := w.leases.attach(w.key, w.uid, func() { _ = c.Close() })
	if !ok {
		c.Close()
		return nil, nil, fmt.Errorf("portal lease expired")
	}
	return &trackedConn{Conn: c, detach: detach}, b, nil
}

type trackedConn struct {
	net.Conn
	detach func()
	once   sync.Once
}

func (c *trackedConn) Close() error { c.once.Do(c.detach); return c.Conn.Close() }
