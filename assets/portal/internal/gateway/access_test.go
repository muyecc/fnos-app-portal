package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

func lanFixture(t *testing.T) *Server {
	t.Helper()
	s, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer example-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	s.cfg.PublicHosts = []string{"nas.example.com", "192.168.1.10", "192.168.1.10:5666", "192.168.1.10:5667"}
	s.cfg.AllowedOrigins = []string{"https://nas.example.com", "http://192.168.1.10:5666", "https://192.168.1.10:5667"}
	s.cfg.HTTPCookieIPs = []string{"192.168.1.10"}
	s.cfg.SecureCookie = true
	if err := validateAccessConfig(s.cfg); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLANOriginAndIdentityBoundaries(t *testing.T) {
	s := lanFixture(t)
	for _, tc := range []struct {
		name, host, origin string
		mutate             func(*http.Request)
		code               int
	}{
		{"stripped HTTP host", "192.168.1.10", "http://192.168.1.10:5666", nil, 200},
		{"original HTTP host", "192.168.1.10:5666", "http://192.168.1.10:5666", nil, 200},
		{"stripped HTTPS IP host", "192.168.1.10", "https://192.168.1.10:5667", nil, 200},
		{"HTTPS domain", "nas.example.com", "https://nas.example.com", nil, 200},
		{"unknown port", "192.168.1.10", "http://192.168.1.10:5668", nil, 403},
		{"wrong scheme", "192.168.1.10", "https://192.168.1.10:5666", nil, 403},
		{"unlisted bare HTTP origin", "192.168.1.10", "http://192.168.1.10", nil, 403},
		{"cross domain to IP", "192.168.1.10", "https://nas.example.com", nil, 403},
		{"cross IP to domain", "nas.example.com", "http://192.168.1.10:5666", nil, 403},
		{"other private IP", "192.168.1.11", "http://192.168.1.11:5666", nil, 403},
		{"unknown forwarded port", "192.168.1.10:5668", "http://192.168.1.10:5666", nil, 403},
		{"untrusted Unix peer", "192.168.1.10", "http://192.168.1.10:5666", func(r *http.Request) { *r = *r.WithContext(context.WithValue(r.Context(), peerKey{}, uint32(999))) }, 403},
		{"missing NAS identity", "192.168.1.10", "http://192.168.1.10:5666", func(r *http.Request) { r.Header.Del("X-Trim-Userid") }, 403},
		{"ordinary NAS user", "192.168.1.10", "http://192.168.1.10:5666", func(r *http.Request) { r.Header.Set("X-Trim-Isadmin", "false") }, 403},
		{"null origin", "192.168.1.10", "null", nil, 403},
		{"origin with path", "192.168.1.10", "http://192.168.1.10:5666/", nil, 403},
		{"duplicate origins", "192.168.1.10", "http://192.168.1.10:5666", func(r *http.Request) { r.Header.Add("Origin", "https://evil.test") }, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := req("/app/example-portal/__portal/lease", true)
			r.Method = "POST"
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-App-Portal", "lease")
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLANBrowserCookieRoundTripAndHTTPSIsolation(t *testing.T) {
	s := lanFixture(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Transport: transportFunc(func(in *http.Request) (*http.Response, error) {
		r := in.Clone(context.WithValue(in.Context(), peerKey{}, uint32(0)))
		r.Host = in.URL.Hostname() // Actual fnOS forwarding behavior.
		r.Header.Set("X-Trim-Userid", "1000")
		r.Header.Set("X-Trim-Username", "admin")
		r.Header.Set("X-Trim-Isadmin", "true")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	request := func(method, origin, path, auth string) *http.Response {
		t.Helper()
		r, _ := http.NewRequest(method, origin+"/app/example-portal/"+path, nil)
		if method == "POST" {
			r.Header.Set("Origin", origin)
			r.Header.Set("X-App-Portal", "lease")
		}
		r.Header.Set("Authorization", auth)
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { response.Body.Close() })
		return response
	}
	lan := "http://192.168.1.10:5666"
	response := request("GET", lan, "", "")
	cs := response.Cookies()
	if response.StatusCode != 200 || len(cs) != 1 || cs[0].Name != cookieName+"LAN" || cs[0].Secure || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteStrictMode || cs[0].Path != s.cfg.Prefix+"/" || cs[0].Domain != "" {
		t.Fatal("incorrect private-IP cookie policy")
	}
	key := cs[0].Value
	response = request("POST", lan, "__portal/lease", "")
	if response.StatusCode != 200 || len(response.Cookies()) != 1 || response.Cookies()[0].Value != key {
		t.Fatal("browser could not retain and renew the HTTP lease")
	}
	if response = request("GET", lan, "api/agents", ""); response.StatusCode != 401 {
		t.Fatal("NAS login bypassed Application authentication")
	}
	if response = request("GET", lan, "api/agents", "Bearer example-test"); response.StatusCode != 204 {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("HTTP browser lease failed: %d %s", response.StatusCode, body)
	}
	response = request("GET", "https://nas.example.com", "", "")
	cs = response.Cookies()
	if response.StatusCode != 200 || len(cs) != 1 || cs[0].Name != cookieName || !cs[0].Secure || !cs[0].HttpOnly || cs[0].Value == key {
		t.Fatal("HTTPS domain cookie was weakened or shared with LAN")
	}
	plainDomain, _ := url.Parse("http://nas.example.com/app/example-portal/")
	if len(jar.Cookies(plainDomain)) != 0 {
		t.Fatal("domain's Secure cookie leaked to HTTP")
	}
	// Forwarded headers cannot select the less strict LAN policy on the domain.
	r := req("/app/example-portal/", true)
	r.Host = "nas.example.com"
	r.Header.Set("X-Forwarded-Host", "192.168.1.10")
	r.Header.Set("X-Forwarded-Proto", "http")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if c := w.Result().Cookies(); len(c) != 1 || !c[0].Secure || c[0].Name != cookieName {
		t.Fatal("client forwarded headers changed cookie policy")
	}
}

func TestHTTPExceptionsRequireExplicitPrivateIPConfiguration(t *testing.T) {
	s := lanFixture(t)
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"domain exception", func(c *Config) { c.HTTPCookieIPs = []string{"nas.example.com"} }},
		{"public IP exception", func(c *Config) { c.HTTPCookieIPs = []string{"8.8.8.8"} }},
		{"subnet exception", func(c *Config) { c.HTTPCookieIPs = []string{"192.168.1.0/24"} }},
		{"global insecure default", func(c *Config) { c.SecureCookie = false }},
		{"missing origins", func(c *Config) { c.AllowedOrigins = nil }},
		{"unlisted HTTP origin", func(c *Config) { c.AllowedOrigins = []string{"http://192.168.1.11:5666"} }},
		{"missing HTTP IP exception", func(c *Config) { c.HTTPCookieIPs = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := s.cfg
			tc.mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
	b, err := os.ReadFile("../../fpk/app/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err = json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if err = validateAccessConfig(cfg); err != nil {
		t.Fatal("production template rejected:", err)
	}
}
