package main_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/github/githubtest"
	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
)

const ghToken = "ghp_e2eGitHubToken"

// ghCloneURL is the origin of the harness clones: an Enterprise Server entry, ghe.test,
// points at githubtest, since the binary cannot be pointed at api.github.com.
const ghCloneURL = "https://ghe.test/octo/app.git"

// ghE2E is a fake GitHub at ghe.test plus a clone of octo/app.
type ghE2E struct {
	srv  *githubtest.Server
	repo string
	env  []string
}

// ghConfig is the hosts entry of a githubtest server keyed name.
func ghConfig(name string, srv *githubtest.Server) string {
	return fmt.Sprintf("  %s:\n    api: %s\n    platform: github\n    insecure: true\n    token_env: E2E_GH_TOKEN\n", name, srv.URL())
}

func newGitHubE2E(t *testing.T) *ghE2E {
	t.Helper()
	return newGitHubE2EWith(t, githubtest.Fixture(time.Now()))
}

func newGitHubE2EWith(t *testing.T, r githubtest.Repo) *ghE2E {
	t.Helper()
	srv := githubtest.New(t, r)
	return &ghE2E{
		srv:  srv,
		repo: gitRepo(t, ghCloneURL, "main"),
		env:  []string{writeConfig(t, "hosts:\n"+ghConfig("ghe.test", srv)), "E2E_GH_TOKEN=" + ghToken},
	}
}

func (e *ghE2E) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return checkNoToken(t, ghToken)(runVigie(t, e.repo, e.env, args...))
}

const ghWantLine = "octo/app br 3/5 ci 1r/2p last:fail main 3m mr 5 is 12\n"

func TestE2EGitHubStatusLine(t *testing.T) {
	t.Parallel()
	e := newGitHubE2E(t)
	out, errOut, code := e.run(t)
	if code != 0 || out != ghWantLine || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for _, r := range out {
		if r > 0x7f && r != '–' {
			t.Fatalf("non-ASCII rune %q in piped output: %q", r, out)
		}
	}
	if got := e.srv.Tokens(); !slices.Equal(got, []string{"Bearer " + ghToken}) {
		t.Fatalf("tokens seen = %q", got)
	}
}

func TestE2EGitHubIssuesDisabled(t *testing.T) {
	t.Parallel()
	r := githubtest.Fixture(time.Now())
	r.HasIssues = false
	e := newGitHubE2EWith(t, r)
	out, errOut, code := e.run(t)
	if code != 0 || out != strings.Replace(ghWantLine, "is 12", "is –", 1) || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EGitHubRunsRefused(t *testing.T) {
	t.Parallel()
	e := newGitHubE2E(t)
	e.srv.Set(githubtest.EndpointRuns, githubtest.Behavior{Status: http.StatusForbidden})
	out, errOut, code := e.run(t)
	if code != 0 || out != "octo/app br 3/5 ci – last:– mr 5 is 12\n" || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EGitHubLookupErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		b    githubtest.Behavior
		code int
		want []string
	}{
		{"401", githubtest.Behavior{Status: http.StatusUnauthorized}, 3,
			[]string{"token rejected by ", "(401): check that the token is valid and not expired"}},
		{"404", githubtest.Behavior{Status: http.StatusNotFound}, 4,
			[]string{"repository octo/app not found on ", "check that the token can see it"}},
		{"SSO", githubtest.Behavior{Status: http.StatusForbidden, Header: map[string]string{"X-GitHub-SSO": "required; url=https://x"}}, 3,
			[]string{"authorize the token for the organization's SAML single sign-on"}},
	}
	for _, c := range cases {
		e := newGitHubE2E(t)
		e.srv.Set(githubtest.EndpointRepo, c.b)
		out, errOut, code := e.run(t)
		if code != c.code || out != "" || strings.Count(errOut, "\n") != 1 {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", c.name, code, out, errOut)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(errOut, w) {
				t.Errorf("%s: stderr %q lacks %q", c.name, errOut, w)
			}
		}
		if c.name == "401" && !strings.HasPrefix(errOut, c.want[0]) {
			t.Errorf("401: stderr = %q", errOut)
		}
	}
}

func TestE2EGitHubPathRule(t *testing.T) {
	t.Parallel()
	e := newGitHubE2E(t)
	git(t, e.repo, "remote", "set-url", "origin", "https://ghe.test/a/b/c.git")
	out, errOut, code := e.run(t)
	if code != 2 || out != "" || !strings.Contains(errOut, "project path a/b/c on ") || !strings.Contains(errOut, "GitHub repositories are owner/repo") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if n := len(e.srv.Requests()); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

func TestE2EGitHubOutsideClone(t *testing.T) {
	t.Parallel()
	e := newGitHubE2E(t)
	out, errOut, code := e.run(t, "-p", "octo/app", "--host", e.srv.URL(), "-C", t.TempDir())
	if code != 0 || out != ghWantLine {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	out, errOut, code = e.run(t, "-p", "a/b/c", "--host", e.srv.URL(), "-C", t.TempDir())
	if code != 2 || out != "" || !strings.Contains(errOut, "GitHub repositories are owner/repo") {
		t.Fatalf("a/b/c: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EGitHubRenamed(t *testing.T) {
	t.Parallel()
	r := githubtest.Fixture(time.Now())
	r.MovedFrom = "octo/old"
	e := newGitHubE2EWith(t, r)
	git(t, e.repo, "remote", "set-url", "origin", "https://ghe.test/octo/old.git")
	out, errOut, code := e.run(t, "-v")
	if code != 0 || !strings.HasPrefix(out, "octo/app ") || !strings.Contains(errOut, "repository moved to octo/app: update the remote URL") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

// ghMissingToken is the one-line error for a github.com clone without any GitHub token.
const ghMissingToken = `GH_TOKEN and GITHUB_TOKEN are not set: create a fine-grained token with read access to the repository and export it, or set token_command: ["gh", "auth", "token"] for this host in the config file` + "\n"

func TestE2EGitHubComNoToken(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"https://github.com/cli/cli.git", "ssh://git@ssh.github.com:443/cli/cli.git"} {
		dir := gitRepo(t, origin, "main")
		proxy, proxied := recordingProxy(t)
		out, errOut, code := checkNoToken(t, "glpat-e2e")(runVigie(t, dir, append(proxy, "GITLAB_TOKEN=glpat-e2e")))
		if code != 3 || out != "" || errOut != ghMissingToken {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", origin, code, out, errOut)
		}
		if n := proxied(); n != 0 {
			t.Errorf("%s: the proxy received %d requests", origin, n)
		}
	}
}

func TestE2EGitHubGist(t *testing.T) {
	t.Parallel()
	// A gist remote with a two-segment path reaches the host rule; the usual one-segment
	// form is already refused by remote parsing, with the same exit code.
	for origin, want := range map[string]string{
		"https://gist.github.com/octo/abc123.git": "gist.github.com is not a repository host: check the git remote\n",
		"https://gist.github.com/abc123.git":      "",
	} {
		dir := gitRepo(t, origin, "main")
		out, errOut, code := checkNoToken(t, "glpat-e2e")(runVigie(t, dir, []string{"GITLAB_TOKEN=glpat-e2e"}))
		if code != 2 || out != "" || (want != "" && errOut != want) {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", origin, code, out, errOut)
		}
	}
}

func TestE2ELookupDeadline(t *testing.T) {
	t.Parallel()
	e := newGitHubE2E(t)
	e.srv.Set(githubtest.EndpointRepo, githubtest.Behavior{Delay: 5 * time.Second})
	start := time.Now()
	out, errOut, code := e.run(t, "--timeout", "300ms")
	if code != 5 || out != "" || !strings.Contains(errOut, "raise --timeout") || time.Since(start) > 2*time.Second {
		t.Fatalf("GitHub: code=%d stdout=%q stderr=%q after %v", code, out, errOut, time.Since(start))
	}

	g := newE2E(t)
	g.srv.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Delay: 5 * time.Second})
	start = time.Now()
	out, errOut, code = g.run(t, "--timeout", "300ms")
	if code != 5 || out != "" || !strings.Contains(errOut, "raise --timeout") || time.Since(start) > 2*time.Second {
		t.Fatalf("GitLab: code=%d stdout=%q stderr=%q after %v", code, out, errOut, time.Since(start))
	}
}

func TestE2EGitHubJSON(t *testing.T) {
	t.Parallel()
	e := newGitHubE2E(t)
	out, errOut, code := e.run(t, "-o", "json")
	if code != 0 || errOut != "" || !strings.HasPrefix(out, `{"platform":"github",`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	rec := records(t, out)[0]
	checkSchema(t, rec)
	last, _ := rec["last"].(map[string]any)
	sha := githubtest.Fixture(time.Now()).Runs[0].HeadSHA
	if rec["project"] != "octo/app" || last["status"] != "failed" || last["web_url"] != e.srv.URL()+"/octo/app/commit/"+sha {
		t.Fatalf("record = %v", rec)
	}
	if _, ok := last["id"]; ok {
		t.Fatalf("last has an id on GitHub: %v", last)
	}
	if p, _ := rec["pipelines"].(map[string]any); fmt.Sprint(p) != "map[pending:2 running:1]" {
		t.Fatalf("pipelines = %v", rec["pipelines"])
	}
}
