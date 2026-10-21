package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/sgaunet/vigie/internal/token"
)

// Endpoint is the resolved GitLab instance for one run.
type Endpoint struct {
	API           *url.URL // web base URL; the API is at API + /api/v4
	Insecure      bool     // the matching profile allows http://
	Token         token.Source
	ExtraRootsPEM []byte
}

// KnownHost reports whether a remote host is a GitLab instance the user named: gitlab.com,
// a key or api host of the hosts map, or the host of GITLAB_HOST (port ignored). No other
// host is recognised, whatever its name, so no token is ever sent to one.
func (s Settings) KnownHost(remoteHost string) bool {
	key := hostKey(remoteHost)
	if key == "gitlab.com" {
		return true
	}
	if _, ok := s.Profile(remoteHost); ok {
		return true
	}
	return s.FallbackHost != nil && hostKey(s.FallbackHost.Host) == key
}

// OnHost reports whether a remote host belongs to the instance at base (--host): its name
// is base's (port ignored), or its profile in the hosts map has base as api.
func (s Settings) OnHost(remoteHost string, base *url.URL) bool {
	if hostKey(remoteHost) == hostKey(base.Host) {
		return true
	}
	p, ok := s.Profile(remoteHost)
	return ok && p.API != nil && sameBase(p.API, base)
}

// sameBase compares two base URLs by scheme, host, port and path.
func sameBase(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) &&
		port(a) == port(b) && a.Path == b.Path
}

func port(u *url.URL) string {
	switch {
	case u.Port() != "":
		return u.Port()
	case strings.EqualFold(u.Scheme, "http"):
		return "80"
	}
	return "443"
}

// ResolveHost picks the GitLab instance: --host, else the remote host translated through
// the hosts map (default https://<host>), else GITLAB_HOST. remoteHost is "" when no remote
// was detected.
func ResolveHost(s Settings, remoteHost string) (Endpoint, error) {
	var (
		api  *url.URL
		prof HostProfile
	)
	switch {
	case s.Host != nil:
		api = s.Host
		prof, _ = s.Profile(s.Host.Host)
	case remoteHost != "":
		var ok bool
		prof, ok = s.Profile(remoteHost)
		api = prof.API
		if !ok || api == nil {
			api = &url.URL{Scheme: "https", Host: remoteHost}
		}
	case s.FallbackHost != nil:
		api = s.FallbackHost
		prof, _ = s.Profile(s.FallbackHost.Host)
	default:
		return Endpoint{}, fmt.Errorf("%w: run vigie inside a clone, pass --host, or set GITLAB_HOST", ErrNoHost)
	}
	if api.Scheme == "http" && !prof.Insecure {
		return Endpoint{}, fmt.Errorf("%w %s: set insecure: true for this host in the config file", ErrInsecureHost, api)
	}
	return Endpoint{API: api, Insecure: prof.Insecure, Token: prof.Token, ExtraRootsPEM: s.ExtraRootsPEM}, nil
}
