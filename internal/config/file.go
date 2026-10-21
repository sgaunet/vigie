package config

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/token"
)

// HostProfile describes how to reach one GitLab or GitHub instance.
type HostProfile struct {
	API      *url.URL // web base URL; nil means https://<key>
	Token    token.Source
	Insecure bool            // allows an http:// API
	Platform status.Platform // GitHub for platform: github or a github.com entry, else GitLab
}

// fileConfig is the YAML schema. Unknown keys are rejected.
type fileConfig struct {
	Interval   string              `yaml:"interval"`
	Fields     []string            `yaml:"fields"`
	Emoji      *bool               `yaml:"emoji"`
	ActiveDays *int                `yaml:"active_days"`
	Hosts      map[string]fileHost `yaml:"hosts"`
}

type fileHost struct {
	API          string   `yaml:"api"`
	TokenEnv     string   `yaml:"token_env"`
	TokenCommand []string `yaml:"token_command"`
	Insecure     bool     `yaml:"insecure"`
	Platform     string   `yaml:"platform"`
}

// HostFS returns the host filesystem in the form Load expects: rooted at "/", or at the
// volume named by the path on Windows.
func HostFS() fs.FS { return hostFS{} }

type hostFS struct{}

func (hostFS) Open(name string) (fs.File, error) {
	p := filepath.FromSlash(name)
	if filepath.VolumeName(p) == "" {
		p = string(filepath.Separator) + p
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	return f, nil
}

// loadCertFile reads SSL_CERT_FILE, whose CAs are trusted in addition to the system ones
// on every platform (Go reads it natively on Linux only).
func (s *Settings) loadCertFile(env map[string]string, fsys fs.FS) error {
	p := env["SSL_CERT_FILE"]
	if p == "" {
		return nil
	}
	data, err := fs.ReadFile(fsys, fsPath(p))
	if err != nil {
		return fmt.Errorf("%w SSL_CERT_FILE %s: cannot read it", ErrInvalid, p)
	}
	if !x509.NewCertPool().AppendCertsFromPEM(data) {
		return fmt.Errorf("%w SSL_CERT_FILE %s: no PEM certificate in it", ErrInvalid, p)
	}
	s.ExtraRootsPEM = data
	return nil
}

// fsPath turns an absolute OS path into a path for Load's fs.FS.
func fsPath(p string) string {
	return strings.TrimPrefix(filepath.ToSlash(p), "/")
}

// configPath returns the config file path and whether the user named it explicitly.
func configPath(env map[string]string) (string, bool) {
	if p := env["VIGIE_CONFIG"]; p != "" {
		return p, true
	}
	if x := env["XDG_CONFIG_HOME"]; x != "" {
		return path.Join(filepath.ToSlash(x), "vigie", "config.yaml"), false
	}
	home := env["HOME"]
	if home == "" {
		home = env["USERPROFILE"]
	}
	if home == "" {
		return "", false
	}
	return path.Join(filepath.ToSlash(home), ".config", "vigie", "config.yaml"), false
}

func (s *Settings) loadFile(env map[string]string, fsys fs.FS) error {
	p, explicit := configPath(env)
	if p == "" {
		return nil
	}
	data, err := fs.ReadFile(fsys, fsPath(p))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !explicit {
			return nil
		}
		return fmt.Errorf("%w config file %s: cannot read it, check VIGIE_CONFIG", ErrInvalid, p)
	}
	cfg, err := decodeFile(data, p)
	if err != nil {
		return err
	}
	s.ConfigPath = p
	return s.applyFile(cfg, p)
}

func decodeFile(data []byte, p string) (fileConfig, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fileConfig{}, fmt.Errorf("%w config file %s: %w", ErrInvalid, p, yamlError(err))
	}
	if hasTokenKey(&root) {
		return fileConfig{}, fmt.Errorf("config file %s %w: use token_env or token_command instead", p, ErrTokenInFile)
	}
	var cfg fileConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return fileConfig{}, fmt.Errorf("%w config file %s: %w", ErrInvalid, p, yamlError(err))
	}
	return cfg, nil
}

// yamlError keeps the decoder's message but drops values it may quote.
func yamlError(err error) error {
	var te *yaml.TypeError
	if errors.As(err, &te) {
		return errors.New(strings.Join(te.Errors, "; "))
	}
	return err
}

// hasTokenKey reports whether any mapping in the document has a "token" key.
func hasTokenKey(n *yaml.Node) bool {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if strings.EqualFold(n.Content[i].Value, "token") {
				return true
			}
		}
	}
	for _, c := range n.Content {
		if hasTokenKey(c) {
			return true
		}
	}
	return false
}

func (s *Settings) applyFile(cfg fileConfig, p string) error {
	if cfg.Interval != "" {
		d, err := time.ParseDuration(cfg.Interval)
		if err != nil {
			return fmt.Errorf("%w interval %q in config file %s: use a duration such as 5s", ErrInvalid, cfg.Interval, p)
		}
		s.Interval = d
	}
	if cfg.Fields != nil {
		f, err := status.ParseFieldList(cfg.Fields)
		if err != nil {
			return fmt.Errorf("%w fields in config file %s: %w", ErrInvalid, p, err)
		}
		s.Fields = f
	}
	if cfg.Emoji != nil {
		s.Emoji = *cfg.Emoji
	}
	if cfg.ActiveDays != nil {
		if *cfg.ActiveDays < 1 {
			return fmt.Errorf("%w active_days %d in config file %s: minimum is 1", ErrInvalid, *cfg.ActiveDays, p)
		}
		s.ActiveDays = *cfg.ActiveDays
	}
	// Sorted, so that errors never depend on map order.
	named := map[string]string{} // host key → hosts key as written
	for _, key := range slices.Sorted(maps.Keys(cfg.Hosts)) {
		k := hostKey(key)
		if first, ok := named[k]; ok {
			return invalidf("hosts %s and %s in config file %s name the same host: keep one", first, key, p)
		}
		named[k] = key
		prof, err := cfg.Hosts[key].profile(key, p)
		if err != nil {
			return err
		}
		s.Hosts[k] = prof
	}
	return nil
}

func (h fileHost) profile(key, p string) (HostProfile, error) {
	if h.TokenEnv != "" && len(h.TokenCommand) > 0 {
		return HostProfile{}, fmt.Errorf("%w host %s in config file %s: set token_env or token_command, not both", ErrInvalid, key, p)
	}
	prof := HostProfile{
		Token:    token.Source{Env: h.TokenEnv, Command: h.TokenCommand},
		Insecure: h.Insecure,
	}
	if h.API != "" {
		u, err := parseBaseURL(h.API)
		if err != nil {
			return HostProfile{}, fmt.Errorf("%w api of host %s in config file %s: %w", ErrInvalid, key, p, err)
		}
		prof.API = u
	}
	dotCom := hostKey(key) == gitHubCom || isGitHubCom(prof.API)
	switch status.Platform(h.Platform) {
	case "", status.PlatformGitLab:
		if h.Platform != "" && dotCom {
			return HostProfile{}, invalidf("platform of host %s in config file %s: github.com is always github", key, p)
		}
		prof.Platform = status.PlatformGitLab
	case status.PlatformGitHub:
		prof.Platform = status.PlatformGitHub
	default:
		return HostProfile{}, invalidf("platform of host %s in config file %s: use gitlab or github", key, p)
	}
	if dotCom {
		prof.Platform = status.PlatformGitHub
	}
	return prof, nil
}

// gitHubCom is the host key of the github.com family.
const gitHubCom = "github.com"

// hostKey normalises a host name for profile lookup: lowercase, no port, and the names of
// the github.com family (www, ssh, api) mapped to github.com.
func hostKey(h string) string {
	h = strings.ToLower(h)
	if u, err := url.Parse("//" + h); err == nil && u.Hostname() != "" {
		h = u.Hostname()
	}
	switch h {
	case "www.github.com", "ssh.github.com", "api.github.com":
		return gitHubCom
	}
	return h
}

// isGitHubCom reports whether u is https://github.com: an entry with that api is github.com
// under another name, such as an SSH alias.
func isGitHubCom(u *url.URL) bool {
	return u != nil && strings.EqualFold(u.Scheme, "https") && strings.EqualFold(u.Hostname(), gitHubCom) &&
		(u.Port() == "" || u.Port() == "443") && u.Path == ""
}

// entry is one hosts entry: its key (lowercase, no port) and its profile.
type entry struct {
	key  string
	prof HostProfile
}

// candidates returns the hosts entries that may serve host (port ignored): the one keyed by
// it, else, outside the github.com family, every entry whose api has that host, by key.
func (s Settings) candidates(host string) []entry {
	key := hostKey(host)
	if p, ok := s.Hosts[key]; ok {
		return []entry{{key, p}}
	}
	if key == gitHubCom {
		return nil
	}
	var out []entry
	for k, p := range s.Hosts {
		if p.API != nil && hostKey(p.API.Host) == key {
			out = append(out, entry{k, p})
		}
	}
	slices.SortFunc(out, func(a, b entry) int { return strings.Compare(a.key, b.key) })
	return out
}

// Profile returns the host profile whose key, or else whose api host, matches host (port
// ignored). A github.com-family host matches an entry by its key only, and an api host
// shared by several entries matches none.
func (s Settings) Profile(host string) (HostProfile, bool) {
	c := s.candidates(host)
	if len(c) != 1 {
		return HostProfile{}, false
	}
	return c[0].prof, true
}

// entryFor returns the entry serving host, and whether there is one. When several entries
// share the api host, base (--host or GITLAB_HOST) picks the one whose api it is; otherwise
// the choice is the user's, and an error says so.
func (s Settings) entryFor(host string, base *url.URL) (entry, bool, error) {
	c := s.candidates(host)
	if len(c) > 1 && base != nil {
		if same := slices.DeleteFunc(slices.Clone(c), func(e entry) bool { return !sameBase(e.prof.API, base) }); len(same) == 1 {
			c = same
		}
	}
	switch len(c) {
	case 0:
		return entry{}, false, nil
	case 1:
		return c[0], true, nil
	}
	keys := make([]string, len(c))
	for i, e := range c {
		keys[i] = e.key
	}
	which := "remote host"
	if base != nil {
		which = "host"
	}
	k := hostKey(host)
	return entry{}, false, invalidf("%s %s matches hosts %s in the config file: add an entry keyed %s", which, k, joinAnd(keys), k)
}

// joinAnd gives "a and b", or "a, b and c".
func joinAnd(s []string) string {
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}
