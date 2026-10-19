package repo

import (
	"fmt"
	"net/url"
	"strings"
)

// Remote is a parsed git remote: a host name (no port, no credentials) and a project path.
type Remote struct {
	Host string // e.g. gitlab.com
	Path string // e.g. group/sub/project, without .git or surrounding slashes
}

const remoteHint = "use git@host:group/project.git, ssh://host/group/project or https://host/group/project"

// ParseRemote parses scp-like (git@host:group/project.git), ssh:// and http(s):// remote
// URLs. Credentials are dropped; the error message quotes the URL without them.
func ParseRemote(raw string) (Remote, error) {
	r, ok := parseRemote(strings.TrimSpace(raw))
	if !ok {
		return Remote{}, fmt.Errorf("%w %q: %s", ErrRemoteURL, redact(raw), remoteHint)
	}
	return r, nil
}

func parseRemote(raw string) (Remote, bool) {
	var host, path string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Remote{}, false
		}
		switch u.Scheme {
		case "ssh", "http", "https":
		default:
			return Remote{}, false
		}
		host, path = u.Hostname(), u.Path
	} else {
		// scp-like: [user@]host:path, where the host part holds no slash.
		h, p, ok := strings.Cut(raw, ":")
		if !ok || strings.Contains(h, "/") {
			return Remote{}, false
		}
		if i := strings.LastIndex(h, "@"); i >= 0 {
			h = h[i+1:]
		}
		host, path = h, p
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	path = strings.TrimSuffix(path, "/")
	if host == "" || !validPath(path) {
		return Remote{}, false
	}
	return Remote{Host: strings.ToLower(host), Path: path}, true
}

func validPath(p string) bool {
	segs := strings.Split(p, "/")
	if len(segs) < 2 {
		return false
	}
	for _, s := range segs {
		if s == "" {
			return false
		}
	}
	return true
}

// redact removes anything that may be a credential: the userinfo of a URL, or the part
// before "@" in any other form.
func redact(raw string) string {
	if scheme, rest, ok := strings.Cut(raw, "://"); ok {
		hostEnd := strings.IndexAny(rest, "/?#")
		if hostEnd < 0 {
			hostEnd = len(rest)
		}
		if at := strings.LastIndex(rest[:hostEnd], "@"); at >= 0 {
			rest = rest[at+1:]
		}
		return scheme + "://" + rest
	}
	if h, p, ok := strings.Cut(raw, ":"); ok {
		if at := strings.LastIndex(h, "@"); at >= 0 {
			return h[at+1:] + ":" + p
		}
	}
	return raw
}
