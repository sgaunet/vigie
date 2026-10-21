package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/token"
)

// Endpoint is the resolved GitLab or GitHub instance for one run.
type Endpoint struct {
	API           *url.URL // web base URL; the API is at API + /api/v4 on GitLab, derived by the github client on GitHub
	Insecure      bool     // the matching profile allows http://
	Token         token.Source
	ExtraRootsPEM []byte
	Platform      status.Platform
}

// KnownHost reports whether a remote host is a GitLab or GitHub instance the user named:
// gitlab.com, the github.com family, a key or api host of the hosts map, or the host of
// GITLAB_HOST (port ignored). No other host is recognised, whatever its name, so no token
// is ever sent to one. A host whose api several entries share is known, so that resolving
// it reports the ambiguity.
func (s Settings) KnownHost(remoteHost string) bool {
	key := hostKey(remoteHost)
	if key == "gitlab.com" || key == gitHubCom {
		return true
	}
	if len(s.candidates(remoteHost)) > 0 {
		return true
	}
	return s.FallbackHost != nil && hostKey(s.FallbackHost.Host) == key
}

// OnHost reports whether a remote host belongs to the instance at base (--host): its name
// is base's (port ignored, github.com family as one), or its profile in the hosts map has
// base as api.
func (s Settings) OnHost(remoteHost string, base *url.URL) bool {
	if hostKey(remoteHost) == hostKey(base.Host) {
		return true
	}
	for _, e := range s.candidates(remoteHost) {
		if e.prof.API != nil && sameBase(e.prof.API, base) {
			return true
		}
	}
	return false
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

// ResolveHost picks the instance: --host, else the remote host translated through the hosts
// map (default https://<host>), else GITLAB_HOST. remoteHost is "" when no remote was
// detected. The platform is GitHub for the github.com family and GitHub entries, GitLab
// otherwise and always for GITLAB_HOST; the token's default variables follow the kind of
// host. On ErrInsecureHost, the endpoint still carries the platform.
func ResolveHost(s Settings, remoteHost string) (Endpoint, error) {
	var (
		api      *url.URL
		host     string // the host resolved: --host, remote or GITLAB_HOST
		base     *url.URL
		fallback bool
	)
	switch {
	case s.Host != nil:
		api, host, base = s.Host, s.Host.Host, s.Host
	case remoteHost != "":
		host = remoteHost
	case s.FallbackHost != nil:
		api, host, base, fallback = s.FallbackHost, s.FallbackHost.Host, s.FallbackHost, true
	default:
		return Endpoint{}, fmt.Errorf("%w: run vigie inside a clone, pass --host, or set GITLAB_HOST", ErrNoHost)
	}
	e, found, err := s.entryFor(host, base)
	if err != nil {
		return Endpoint{Platform: s.sharedPlatform(host)}, err
	}
	key := hostKey(host)
	if !found && strings.HasSuffix(key, "."+gitHubCom) {
		return Endpoint{}, invalidf("%s is not a repository host: check the git remote", key)
	}
	dotCom := key == gitHubCom || isGitHubCom(e.prof.API)
	platform := status.PlatformGitLab
	if !fallback && (dotCom || e.prof.Platform == status.PlatformGitHub) {
		platform = status.PlatformGitHub
	}
	switch {
	case api != nil:
	case e.prof.API != nil:
		api = e.prof.API
	case key == gitHubCom:
		api = &url.URL{Scheme: "https", Host: gitHubCom}
	default:
		api = &url.URL{Scheme: "https", Host: remoteHost}
	}
	tok := e.prof.Token
	switch {
	case platform == status.PlatformGitLab:
		tok.Vars = token.GitLabVars
	case dotCom:
		tok.Vars = token.GitHubVars
	default:
		tok.Vars, tok.Host = token.EnterpriseVars, e.key
	}
	if api.Scheme == "http" && !e.prof.Insecure {
		return Endpoint{Platform: platform}, fmt.Errorf("%w %s: set insecure: true for this host in the config file", ErrInsecureHost, api)
	}
	return Endpoint{API: api, Insecure: e.prof.Insecure, Token: tok, ExtraRootsPEM: s.ExtraRootsPEM, Platform: platform}, nil
}

// sharedPlatform is the platform of the entries that may serve host: GitHub when every one
// is, else GitLab. A scan row failing on an ambiguous match still names a platform.
func (s Settings) sharedPlatform(host string) status.Platform {
	c := s.candidates(host)
	for _, e := range c {
		if e.prof.Platform != status.PlatformGitHub && !isGitHubCom(e.prof.API) {
			return status.PlatformGitLab
		}
	}
	if len(c) == 0 {
		return status.PlatformGitLab
	}
	return status.PlatformGitHub
}

// CheckProject checks a project path against the platform of ep: on GitHub, a repository is
// owner/repo. A numeric ID passes on both platforms.
func CheckProject(ep Endpoint, path string) error {
	if ep.Platform != status.PlatformGitHub || strings.Trim(path, "0123456789") == "" {
		return nil
	}
	if owner, repo, ok := strings.Cut(path, "/"); ok && owner != "" && repo != "" && !strings.Contains(repo, "/") {
		return nil
	}
	host := ""
	if ep.API != nil {
		host = hostKey(ep.API.Host)
	}
	return invalidf("project path %s on %s: GitHub repositories are owner/repo", path, host)
}
