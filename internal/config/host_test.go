package config_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/url"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/token"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestResolveHostPrecedence(t *testing.T) {
	t.Parallel()
	hosts := map[string]config.HostProfile{
		"ssh.gitlab.example.com": {API: mustURL(t, "https://gitlab.example.com"), Token: token.Source{Command: []string{"pass", "x"}}},
		"gitlab.lan":             {API: mustURL(t, "http://gitlab.lan:8080"), Insecure: true, Token: token.Source{Env: "LAN"}},
	}
	cases := []struct {
		name   string
		s      config.Settings
		remote string
		want   string
	}{
		{"--host wins", config.Settings{Host: mustURL(t, "https://flag.example.com"), FallbackHost: mustURL(t, "https://env.example.com"), Hosts: hosts}, "ssh.gitlab.example.com", "https://flag.example.com"},
		{"hosts mapping", config.Settings{FallbackHost: mustURL(t, "https://env.example.com"), Hosts: hosts}, "ssh.gitlab.example.com", "https://gitlab.example.com"},
		{"remote host", config.Settings{FallbackHost: mustURL(t, "https://env.example.com"), Hosts: hosts}, "gitlab.com", "https://gitlab.com"},
		{"GITLAB_HOST", config.Settings{FallbackHost: mustURL(t, "https://env.example.com"), Hosts: hosts}, "", "https://env.example.com"},
		{"insecure profile", config.Settings{Hosts: hosts}, "gitlab.lan", "http://gitlab.lan:8080"},
	}
	for _, c := range cases {
		e, err := config.ResolveHost(c.s, c.remote)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if e.API.String() != c.want || e.Platform != status.PlatformGitLab {
			t.Errorf("%s: API = %s (%s), want %s (gitlab)", c.name, e.API, e.Platform, c.want)
		}
	}
}

func TestResolveHostProfileFields(t *testing.T) {
	t.Parallel()
	s := config.Settings{Hosts: map[string]config.HostProfile{
		"ssh.gitlab.example.com": {API: mustURL(t, "https://gitlab.example.com"), Token: token.Source{Command: []string{"pass", "x"}}},
		"gitlab.lan":             {API: mustURL(t, "http://gitlab.lan"), Insecure: true, Token: token.Source{Env: "LAN"}},
	}}
	e, err := config.ResolveHost(s, "ssh.gitlab.example.com")
	if err != nil || e.Insecure || !slices.Equal(e.Token.Command, []string{"pass", "x"}) {
		t.Fatalf("ssh: %+v, %v", e, err)
	}
	e, err = config.ResolveHost(s, "gitlab.lan")
	if err != nil || !e.Insecure || e.Token.Env != "LAN" {
		t.Fatalf("lan: %+v, %v", e, err)
	}
	// --host matching a profile key takes that profile's token source and insecure flag.
	s.Host = mustURL(t, "http://gitlab.lan")
	e, err = config.ResolveHost(s, "")
	if err != nil || !e.Insecure || e.Token.Env != "LAN" {
		t.Fatalf("--host profile: %+v, %v", e, err)
	}
}

func TestResolveHostErrors(t *testing.T) {
	t.Parallel()
	if _, err := config.ResolveHost(config.Settings{}, ""); !errors.Is(err, config.ErrNoHost) {
		t.Fatalf("no host: %v", err)
	}
	cases := []config.Settings{
		{Host: mustURL(t, "http://gitlab.lan")},
		{FallbackHost: mustURL(t, "http://gitlab.lan")},
		{Hosts: map[string]config.HostProfile{"gitlab.lan": {API: mustURL(t, "http://gitlab.lan")}}},
	}
	for i, s := range cases {
		_, err := config.ResolveHost(s, "gitlab.lan")
		if i < 2 {
			_, err = config.ResolveHost(s, "")
		}
		if !errors.Is(err, config.ErrInsecureHost) {
			t.Errorf("case %d: got %v, want ErrInsecureHost", i, err)
			continue
		}
		want := "refusing to send the token over http://gitlab.lan: set insecure: true for this host in the config file"
		if err.Error() != want {
			t.Errorf("case %d: message = %q", i, err)
		}
	}
}

func TestProfileMatchesAPIHost(t *testing.T) {
	t.Parallel()
	hosts := map[string]config.HostProfile{
		"ssh.gitlab.example.com": {API: mustURL(t, "https://gitlab.example.com"), Token: token.Source{Env: "EX"}},
		"gitlab.lan":             {API: mustURL(t, "http://gitlab.lan:8080"), Insecure: true, Token: token.Source{Env: "LAN"}},
	}
	for _, s := range []config.Settings{
		{Host: mustURL(t, "https://gitlab.example.com"), Hosts: hosts},
		{FallbackHost: mustURL(t, "https://gitlab.example.com:443"), Hosts: hosts},
	} {
		e, err := config.ResolveHost(s, "")
		if err != nil || e.Token.Env != "EX" {
			t.Errorf("api host match: %+v, %v", e, err)
		}
	}
	e, err := config.ResolveHost(config.Settings{Host: mustURL(t, "http://gitlab.lan:8080"), Hosts: hosts}, "")
	if err != nil || !e.Insecure || e.Token.Env != "LAN" {
		t.Errorf("insecure api host match: %+v, %v", e, err)
	}
	if p, ok := (config.Settings{Hosts: hosts}).Profile("gitlab.example.com"); !ok || p.Token.Env != "EX" {
		t.Errorf("Profile by api host = %+v, %v", p, ok)
	}
}

func TestResolveHostOutsideRepository(t *testing.T) {
	t.Parallel()
	s, err := config.Load(config.Overrides{Project: "group/app"}, map[string]string{}, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.ResolveHost(s, ""); !errors.Is(err, config.ErrNoHost) {
		t.Fatalf("got %v, want ErrNoHost", err)
	}
}

// scanSettings has one mapped self-hosted instance and GITLAB_HOST.
func scanSettings(t *testing.T) config.Settings {
	t.Helper()
	return config.Settings{
		Hosts:        map[string]config.HostProfile{"ssh.gitlab.example.com": {API: mustURL(t, "https://gitlab.example.com")}},
		FallbackHost: mustURL(t, "https://git.corp:8443"),
	}
}

func TestKnownHost(t *testing.T) {
	t.Parallel()
	s := scanSettings(t)
	for _, h := range []string{"gitlab.com", "GitLab.com", "gitlab.com:22", "ssh.gitlab.example.com", "gitlab.example.com", "git.corp",
		"github.com", "www.github.com", "ssh.github.com", "api.github.com"} {
		if !s.KnownHost(h) {
			t.Errorf("KnownHost(%q) = false", h)
		}
	}
	for _, h := range []string{"gitlab.evil.com", "gitlab.example.org", "", "gist.github.com", "github.evil.com", "notgithub.com"} {
		if s.KnownHost(h) {
			t.Errorf("KnownHost(%q) = true", h)
		}
	}
}

func TestOnHost(t *testing.T) {
	t.Parallel()
	s := scanSettings(t)
	cases := []struct {
		remote, base string
		want         bool
	}{
		{"gitlab.example.com", "https://gitlab.example.com", true},
		{"GitLab.Example.com", "https://gitlab.example.com", true},
		{"ssh.gitlab.example.com", "https://gitlab.example.com", true},
		{"ssh.gitlab.example.com", "https://gitlab.example.com:8443", false},
		{"ssh.gitlab.example.com", "http://gitlab.example.com", false},
		{"ssh.gitlab.example.com", "https://gitlab.example.com/sub", false},
		{"gitlab.com", "https://gitlab.example.com", false},
		{"git.corp", "https://gitlab.example.com", false},
	}
	for _, c := range cases {
		if got := s.OnHost(c.remote, mustURL(t, c.base)); got != c.want {
			t.Errorf("OnHost(%q, %s) = %v, want %v", c.remote, c.base, got, c.want)
		}
	}
}

// selfSignedPEM returns a PEM-encoded self-signed CA certificate.
func selfSignedPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "vigie test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestSSLCertFile(t *testing.T) {
	t.Parallel()
	ca := selfSignedPEM(t)
	fsys := fstest.MapFS{"certs/ca.pem": {Data: ca}, "certs/junk.pem": {Data: []byte("not a certificate")}}
	s, err := config.Load(config.Overrides{}, map[string]string{"SSL_CERT_FILE": "/certs/ca.pem"}, fsys)
	if err != nil {
		t.Fatal(err)
	}
	e, err := config.ResolveHost(s, "gitlab.com")
	if err != nil || !bytes.Equal(e.ExtraRootsPEM, ca) {
		t.Fatalf("ExtraRootsPEM not filled: %v", err)
	}
	for _, p := range []string{"/certs/missing.pem", "/certs/junk.pem"} {
		_, err := config.Load(config.Overrides{}, map[string]string{"SSL_CERT_FILE": p}, fsys)
		if !errors.Is(err, config.ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", p, err)
		}
	}
}

func TestOnHostDefaultPort(t *testing.T) {
	t.Parallel()
	if !scanSettings(t).OnHost("ssh.gitlab.example.com", mustURL(t, "https://gitlab.example.com:443")) {
		t.Fatal("an explicit default port must match the profile api")
	}
}

// gitHubSettings has a github.com entry, an SSH alias of github.com and an Enterprise
// Server, next to the GitLab ones of scanSettings.
func gitHubSettings(t *testing.T) config.Settings {
	t.Helper()
	s := scanSettings(t)
	s.Hosts["github.com"] = config.HostProfile{Token: token.Source{Command: []string{"gh", "auth", "token"}}, Platform: status.PlatformGitHub}
	s.Hosts["github-work"] = config.HostProfile{API: mustURL(t, "https://github.com"), Token: token.Source{Env: "WORK_GH_TOKEN"}}
	s.Hosts["github.example.com"] = config.HostProfile{Platform: status.PlatformGitHub}
	return s
}

func TestResolveHostPlatform(t *testing.T) {
	t.Parallel()
	s := gitHubSettings(t)
	for _, h := range []string{"github.com", "www.github.com", "ssh.github.com", "api.github.com", "GitHub.com:443"} {
		e, err := config.ResolveHost(s, h)
		if err != nil || e.API.String() != "https://github.com" || e.Platform != status.PlatformGitHub ||
			!slices.Equal(e.Token.Vars, token.GitHubVars) || !slices.Equal(e.Token.Command, []string{"gh", "auth", "token"}) {
			t.Errorf("%s: %+v, %v", h, e, err)
		}
	}
	noProfile := scanSettings(t)
	if e, err := config.ResolveHost(noProfile, "ssh.github.com"); err != nil || e.API.String() != "https://github.com" ||
		e.Platform != status.PlatformGitHub || e.Token.Env != "" || len(e.Token.Command) != 0 || !slices.Equal(e.Token.Vars, token.GitHubVars) {
		t.Errorf("github.com without a profile: %+v, %v", e, err)
	}

	e, err := config.ResolveHost(s, "github-work")
	if err != nil || e.API.String() != "https://github.com" || e.Platform != status.PlatformGitHub ||
		e.Token.Env != "WORK_GH_TOKEN" || !slices.Equal(e.Token.Vars, token.GitHubVars) {
		t.Errorf("github-work: %+v, %v", e, err)
	}

	e, err = config.ResolveHost(s, "github.example.com")
	if err != nil || e.API.String() != "https://github.example.com" || e.Platform != status.PlatformGitHub ||
		!slices.Equal(e.Token.Vars, token.EnterpriseVars) || e.Token.Host != "github.example.com" {
		t.Errorf("Enterprise Server: %+v, %v", e, err)
	}

	for _, h := range []string{"gitlab.com", "git.corp", "ssh.gitlab.example.com"} {
		e, err := config.ResolveHost(s, h)
		if err != nil || e.Platform != status.PlatformGitLab || !slices.Equal(e.Token.Vars, token.GitLabVars) {
			t.Errorf("%s: %+v, %v", h, e, err)
		}
	}

	_, err = config.ResolveHost(s, "gist.github.com")
	if !errors.Is(err, config.ErrInvalid) || err.Error() != "gist.github.com is not a repository host: check the git remote" {
		t.Errorf("gist: %v", err)
	}
}

func TestResolveHostFlagPlatform(t *testing.T) {
	t.Parallel()
	s := scanSettings(t)
	s.Host = mustURL(t, "https://github.com")
	if e, err := config.ResolveHost(s, ""); err != nil || e.Platform != status.PlatformGitHub || !slices.Equal(e.Token.Vars, token.GitHubVars) {
		t.Errorf("--host https://github.com: %+v, %v", e, err)
	}
	// No guessing: an Enterprise Server needs a hosts entry.
	s.Host = mustURL(t, "https://github.example.com")
	if e, err := config.ResolveHost(s, ""); err != nil || e.Platform != status.PlatformGitLab {
		t.Errorf("--host https://github.example.com: %+v, %v", e, err)
	}
	g := gitHubSettings(t)
	g.Host = mustURL(t, "https://github.example.com")
	if e, err := config.ResolveHost(g, ""); err != nil || e.Platform != status.PlatformGitHub || e.Token.Host != "github.example.com" {
		t.Errorf("--host with an Enterprise entry: %+v, %v", e, err)
	}
}

func TestResolveHostInsecureGitHub(t *testing.T) {
	t.Parallel()
	s := config.Settings{Hosts: map[string]config.HostProfile{
		"ghe.lan": {API: mustURL(t, "http://ghe.lan"), Platform: status.PlatformGitHub},
	}}
	e, err := config.ResolveHost(s, "ghe.lan")
	if !errors.Is(err, config.ErrInsecureHost) || e.Platform != status.PlatformGitHub {
		t.Fatalf("got %+v, %v", e, err)
	}
}

func TestGitHubOnHost(t *testing.T) {
	t.Parallel()
	s := gitHubSettings(t)
	cases := []struct {
		remote, base string
		want         bool
	}{
		{"ssh.github.com", "https://github.com", true},
		{"github-work", "https://github.com", true},
		{"github.com", "https://github.example.com", false},
	}
	for _, c := range cases {
		if got := s.OnHost(c.remote, mustURL(t, c.base)); got != c.want {
			t.Errorf("OnHost(%q, %s) = %v, want %v", c.remote, c.base, got, c.want)
		}
	}
}

func TestGitHubComTakesNoAliasToken(t *testing.T) {
	t.Parallel()
	s := config.Settings{Hosts: map[string]config.HostProfile{
		"github-work":  {API: mustURL(t, "https://github.com"), Token: token.Source{Env: "WORK"}},
		"github-perso": {API: mustURL(t, "https://github.com"), Token: token.Source{Env: "PERSO"}},
	}}
	e, err := config.ResolveHost(s, "github.com")
	if err != nil || e.Token.Env != "" || !slices.Equal(e.Token.Vars, token.GitHubVars) {
		t.Fatalf("github.com: %+v, %v", e, err)
	}
}

func TestResolveHostAmbiguousAPI(t *testing.T) {
	t.Parallel()
	s := config.Settings{Hosts: map[string]config.HostProfile{
		"b": {API: mustURL(t, "https://ghe.corp"), Token: token.Source{Env: "B"}},
		"a": {API: mustURL(t, "https://ghe.corp"), Token: token.Source{Env: "A"}},
	}}
	e, err := config.ResolveHost(s, "ghe.corp")
	want := "remote host ghe.corp matches hosts a and b in the config file: add an entry keyed ghe.corp"
	if !errors.Is(err, config.ErrInvalid) || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	// A scan row failing with the error still names a platform.
	if e.Platform != status.PlatformGitLab {
		t.Fatalf("platform = %q, want gitlab", e.Platform)
	}
	gh := config.Settings{Hosts: map[string]config.HostProfile{
		"a": {API: mustURL(t, "https://ghe.corp"), Platform: status.PlatformGitHub},
		"b": {API: mustURL(t, "https://ghe.corp"), Platform: status.PlatformGitHub},
	}}
	if e, err := config.ResolveHost(gh, "ghe.corp"); err == nil || e.Platform != status.PlatformGitHub {
		t.Fatalf("GitHub entries: %+v, %v", e, err)
	}
	if !s.KnownHost("ghe.corp") {
		t.Fatal("an ambiguous host must stay known, so its row fails with the error")
	}
	delete(s.Hosts, "b")
	if e, err := config.ResolveHost(s, "ghe.corp"); err != nil || e.Token.Env != "A" {
		t.Fatalf("single api match: %+v, %v", e, err)
	}
}

func TestResolveHostFlagPicksExactAPI(t *testing.T) {
	t.Parallel()
	// Two instances on one host name, told apart by port: --host names one of them.
	s := config.Settings{Hosts: map[string]config.HostProfile{
		"one.test": {API: mustURL(t, "http://127.0.0.1:8001"), Insecure: true, Token: token.Source{Env: "ONE"}},
		"two.test": {API: mustURL(t, "http://127.0.0.1:8002"), Insecure: true, Token: token.Source{Env: "TWO"}},
	}}
	s.Host = mustURL(t, "http://127.0.0.1:8002")
	if e, err := config.ResolveHost(s, ""); err != nil || e.Token.Env != "TWO" {
		t.Fatalf("got %+v, %v", e, err)
	}
}

func TestCheckProject(t *testing.T) {
	t.Parallel()
	s := gitHubSettings(t)
	dotCom, err := config.ResolveHost(s, "github.com")
	if err != nil {
		t.Fatal(err)
	}
	ghe, err := config.ResolveHost(s, "github.example.com")
	if err != nil {
		t.Fatal(err)
	}
	gl, err := config.ResolveHost(s, "gitlab.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"owner/repo", "123456"} {
		if err := config.CheckProject(dotCom, p); err != nil {
			t.Errorf("%s on github.com: %v", p, err)
		}
	}
	for _, p := range []string{"a/b/c", "a"} {
		err := config.CheckProject(dotCom, p)
		if want := "project path " + p + " on github.com: GitHub repositories are owner/repo"; !errors.Is(err, config.ErrInvalid) || err.Error() != want {
			t.Errorf("%s: got %v, want %q", p, err, want)
		}
	}
	if err := config.CheckProject(ghe, "a/b/c"); err == nil || err.Error() != "project path a/b/c on github.example.com: GitHub repositories are owner/repo" {
		t.Errorf("Enterprise Server: %v", err)
	}
	if err := config.CheckProject(gl, "a/b/c"); err != nil {
		t.Errorf("GitLab: %v", err)
	}
}
