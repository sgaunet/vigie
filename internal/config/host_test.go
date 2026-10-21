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
		if e.API.String() != c.want {
			t.Errorf("%s: API = %s, want %s", c.name, e.API, c.want)
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
	for _, h := range []string{"gitlab.com", "GitLab.com", "gitlab.com:22", "ssh.gitlab.example.com", "gitlab.example.com", "git.corp"} {
		if !s.KnownHost(h) {
			t.Errorf("KnownHost(%q) = false", h)
		}
	}
	for _, h := range []string{"github.com", "gitlab.evil.com", "gitlab.example.org", ""} {
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
