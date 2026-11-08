package gateway

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// The fnOS gateway forwards $host, which can omit the browser's port. Keep
// browser origins explicit; never trust client-supplied forwarded headers.
func parseOrigin(value string) (*url.URL, bool) {
	u, err := url.Parse(value)
	return u, err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == ""
}

func validateAccessConfig(cfg Config) error {
	for _, origin := range cfg.AllowedOrigins {
		u, ok := parseOrigin(origin)
		if !ok || origin != strings.ToLower(origin) || !contains(cfg.PublicHosts, u.Host) {
			return errors.New("explicit allowed origin must match a public host")
		}
		if u.Scheme == "http" && !contains(cfg.HTTPCookieIPs, u.Hostname()) {
			return errors.New("HTTP origin requires an explicit private cookie IP")
		}
	}
	for _, host := range cfg.HTTPCookieIPs {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsPrivate() || !cfg.SecureCookie || len(cfg.AllowedOrigins) == 0 {
			return errors.New("HTTP cookie exception requires a private literal IP and a secure default")
		}
		matched := false
		for _, origin := range cfg.AllowedOrigins {
			u, _ := parseOrigin(origin) // Validated above.
			matched = matched || u.Scheme == "http" && u.Hostname() == host
		}
		if !matched {
			return errors.New("HTTP cookie IP requires an explicit HTTP origin")
		}
	}
	return nil
}

func (s *Server) originAllowed(r *http.Request, origin string) bool {
	u, ok := parseOrigin(origin)
	if !ok || len(r.Header.Values("Origin")) != 1 {
		return false
	}
	if len(s.cfg.AllowedOrigins) == 0 {
		return strings.EqualFold(u.Host, r.Host)
	}
	if !contains(s.cfg.AllowedOrigins, strings.ToLower(origin)) {
		return false
	}
	// A bare forwarded host is accepted only for an explicitly listed original
	// origin on that same host. Other ports/schemes are not inferred or allowed.
	return strings.EqualFold(u.Host, r.Host) || strings.EqualFold(u.Hostname(), r.Host)
}

func (s *Server) cookiePolicy(r *http.Request) (string, bool) {
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if contains(s.cfg.HTTPCookieIPs, host) {
		// A separate name avoids a previous Secure IP cookie blocking issuance
		// over HTTP. Cookies share ports: the IP exception applies on both NAS
		// ports, but never to the HTTPS domain's cookie.
		return cookieName + "LAN", false
	}
	return cookieName, s.cfg.SecureCookie
}
