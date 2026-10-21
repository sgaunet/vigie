package scan_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/scan"
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
	in := inspectedOn("gitlab.com", "ssh.gitlab.example.com", "git.corp", "bitbucket.org", "gitlab.evil.com", "plain.test")
	repos, skips := scan.Select(in, selectSettings(t), nil)
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
	for i, rel := range []string{"bitbucket.org", "gitlab.evil.com"} {
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
	repos, skips := scan.Select(in, s, nil)
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
	repos, skips = scan.Select(inspectedOn("gitlab.lan"), lan, nil)
	if len(repos) != 1 || len(skips) != 0 || repos[0].Endpoint.API.String() != "https://gitlab.lan" {
		t.Fatalf("gitlab.lan: %+v %+v", repos, skips)
	}
}

// gitHubSelectSettings adds a github.com alias and an Enterprise Server to selectSettings.
func gitHubSelectSettings(t *testing.T) config.Settings {
	t.Helper()
	s := selectSettings(t)
	s.Hosts["github-work"] = config.HostProfile{API: mustURL(t, "https://github.com"), Token: token.Source{Env: "WORK_GH_TOKEN"}}
	s.Hosts["github.example.com"] = config.HostProfile{Platform: status.PlatformGitHub}
	return s
}

// one selects a single clone on host with path.
func one(t *testing.T, s config.Settings, env map[string]string, host, path string) ([]scan.Repo, []scan.Skip) {
	t.Helper()
	in := []scan.Inspected{{Rel: host, Checkout: repo.Checkout{Branch: "main", Remote: repo.Remote{Host: host, Path: path}}}}
	return scan.Select(in, s, env)
}

func TestSelectGitHub(t *testing.T) {
	t.Parallel()
	s := gitHubSelectSettings(t)
	gh := map[string]string{"GH_TOKEN": "ghp_x"}
	cases := []struct {
		name   string
		s      config.Settings
		env    map[string]string
		host   string
		path   string
		reason scan.Reason // "" = listed
	}{
		{"github.com with GH_TOKEN", s, gh, "github.com", "cli/cli", ""},
		{"github.com without a GitHub token", s, map[string]string{"GITLAB_TOKEN": "glpat-x"}, "github.com", "cli/cli", scan.ReasonNoToken},
		{"alias with token_env unset", s, nil, "github-work", "cli/cli", ""},
		{"Enterprise Server with GH_TOKEN only", s, gh, "github.example.com", "o/r", scan.ReasonNoToken},
		{"Enterprise Server with its token", s, map[string]string{"GH_ENTERPRISE_TOKEN": "x"}, "github.example.com", "o/r", ""},
		{"GitHub path not owner/repo", s, gh, "github.com", "a/b/c", scan.ReasonRemoteURL},
		{"gist", s, gh, "gist.github.com", "o/abc", scan.ReasonUnknownHost},
		{"GitLab without GITLAB_TOKEN", s, nil, "gitlab.com", "group/app", ""},
	}
	for _, c := range cases {
		repos, skips := one(t, c.s, c.env, c.host, c.path)
		switch {
		case c.reason == "" && (len(repos) != 1 || len(skips) != 0):
			t.Errorf("%s: repos %+v, skips %+v; want listed", c.name, repos, skips)
		case c.reason != "" && (len(repos) != 0 || len(skips) != 1 || skips[0].Reason != c.reason):
			t.Errorf("%s: repos %+v, skips %+v; want %s", c.name, repos, skips, c.reason)
		}
	}
	repos, _ := one(t, s, gh, "ssh.github.com", "cli/cli")
	if len(repos) != 1 || repos[0].Endpoint.Platform != status.PlatformGitHub {
		t.Fatalf("ssh.github.com: %+v", repos)
	}
}

func TestSelectGitHubHostFilter(t *testing.T) {
	t.Parallel()
	s := gitHubSelectSettings(t)
	s.Host = mustURL(t, "https://github.com")
	in := inspectedOn("ssh.github.com", "github-work", "gitlab.com", "ssh.gitlab.example.com")
	repos, skips := scan.Select(in, s, map[string]string{"GH_TOKEN": "ghp_x"})
	if len(repos) != 2 || repos[0].Rel != "ssh.github.com" || repos[1].Rel != "github-work" {
		t.Fatalf("repos = %+v", repos)
	}
	for _, sk := range skips {
		if sk.Reason != scan.ReasonOtherHost {
			t.Errorf("%s: reason %q, want other host", sk.Rel, sk.Reason)
		}
	}
	// FR-025a has no --host exception.
	repos, skips = scan.Select(inspectedOn("ssh.github.com"), s, nil)
	if len(repos) != 0 || len(skips) != 1 || skips[0].Reason != scan.ReasonNoToken {
		t.Fatalf("no token with --host: %+v %+v", repos, skips)
	}
}
