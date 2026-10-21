package scan_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/scan"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// selectSettings maps ssh.gitlab.example.com to https://gitlab.example.com, has GITLAB_HOST
// https://git.corp:8443, and an http profile for plain.test that is not marked insecure.
func selectSettings(t *testing.T) config.Settings {
	t.Helper()
	return config.Settings{
		Hosts: map[string]config.HostProfile{
			"ssh.gitlab.example.com": {API: mustURL(t, "https://gitlab.example.com")},
			"plain.test":             {API: mustURL(t, "http://plain.test")},
		},
		FallbackHost: mustURL(t, "https://git.corp:8443"),
	}
}

func inspectedOn(hosts ...string) []scan.Inspected {
	var in []scan.Inspected
	for _, h := range hosts {
		in = append(in, scan.Inspected{Rel: h, Checkout: repo.Checkout{Branch: "main", Remote: repo.Remote{Host: h, Path: "group/app"}}})
	}
	return in
}

func TestSelectKnownHosts(t *testing.T) {
	t.Parallel()
	in := inspectedOn("gitlab.com", "ssh.gitlab.example.com", "git.corp", "github.com", "gitlab.evil.com", "plain.test")
	repos, skips := scan.Select(in, selectSettings(t))
	if len(repos) != 4 || len(skips) != 2 {
		t.Fatalf("repos %+v, skips %+v", repos, skips)
	}
	wantAPI := []string{"https://gitlab.com", "https://gitlab.example.com", "https://git.corp"}
	for i, want := range wantAPI {
		r := repos[i]
		if r.Rel != in[i].Rel || r.Err != nil || r.Endpoint.API == nil || r.Endpoint.API.String() != want {
			t.Errorf("repo %d = %+v, want API %s", i, r, want)
		}
	}
	if repos[0].Checkout != in[0].Checkout {
		t.Errorf("checkout not kept: %+v", repos[0].Checkout)
	}
	if r := repos[3]; r.Rel != "plain.test" || !errors.Is(r.Err, config.ErrInsecureHost) {
		t.Errorf("plain.test = %+v", r)
	}
	for i, rel := range []string{"github.com", "gitlab.evil.com"} {
		if skips[i].Rel != rel || skips[i].Reason != scan.ReasonUnknownHost {
			t.Errorf("skip %d = %+v, want %s unknown host", i, skips[i], rel)
		}
	}
}

func TestSelectHostFilter(t *testing.T) {
	t.Parallel()
	s := selectSettings(t)
	s.Host = mustURL(t, "https://gitlab.example.com")
	in := inspectedOn("gitlab.example.com", "gitlab.com", "ssh.gitlab.example.com", "git.corp", "github.com")
	repos, skips := scan.Select(in, s)
	if len(repos) != 2 || repos[0].Rel != "gitlab.example.com" || repos[1].Rel != "ssh.gitlab.example.com" {
		t.Fatalf("repos = %+v", repos)
	}
	for _, r := range repos {
		if r.Err != nil || r.Endpoint.API != s.Host {
			t.Errorf("%s: endpoint %v, err %v; want --host", r.Rel, r.Endpoint.API, r.Err)
		}
	}
	if len(skips) != 3 {
		t.Fatalf("skips = %+v", skips)
	}
	for _, sk := range skips {
		if sk.Reason != scan.ReasonOtherHost {
			t.Errorf("%s: reason %q, want other host", sk.Rel, sk.Reason)
		}
	}

	// The --host instance is recognised even outside the config and GITLAB_HOST.
	lan := config.Settings{Host: mustURL(t, "https://gitlab.lan")}
	repos, skips = scan.Select(inspectedOn("gitlab.lan"), lan)
	if len(repos) != 1 || len(skips) != 0 || repos[0].Endpoint.API.String() != "https://gitlab.lan" {
		t.Fatalf("gitlab.lan: %+v %+v", repos, skips)
	}
}
