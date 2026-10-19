package config

import (
	"fmt"
	"net/url"

	"github.com/sgaunet/vigie/internal/token"
)

// Endpoint is the resolved GitLab instance for one run.
type Endpoint struct {
	API           *url.URL // web base URL; the API is at API + /api/v4
	Insecure      bool     // the matching profile allows http://
	Token         token.Source
	ExtraRootsPEM []byte
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
