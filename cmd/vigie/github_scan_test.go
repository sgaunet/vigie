package main_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/github/githubtest"
	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
)

// mixedE2E is a folder with two GitLab clones on gitlab.test and two GitHub clones on
// ghe.test, an Enterprise Server entry pointing at githubtest.
type mixedE2E struct {
	gl   *gitlabtest.Server
	gh   *githubtest.Server
	root string
	cfg  string // VIGIE_CONFIG entry
}

func newMixedE2E(t *testing.T) *mixedE2E {
	t.Helper()
	now := time.Now()
	gl := gitlabtest.New(t, gitlabtest.Fixture(now))
	for i, path := range []string{"backend/api", "frontend/web"} {
		p := gitlabtest.Fixture(now)
		p.ID, p.Path = int64(i+1), path
		gl.AddProject(p)
	}
	gh := githubtest.New(t, githubtest.Fixture(now))
	lib := githubtest.Fixture(now)
	lib.ID, lib.FullName = 43, "octo/lib"
	gh.AddRepo(lib)
	e := &mixedE2E{
		gl: gl, gh: gh, root: t.TempDir(),
		cfg: writeConfig(t, fmt.Sprintf("hosts:\n  gitlab.test:\n    api: %s\n    insecure: true\n", gl.URL())+ghConfig("ghe.test", gh)),
	}
	cloneAt(t, e.root, "gl/api", "git@gitlab.test:backend/api.git", "main")
	cloneAt(t, e.root, "gl/web", "git@gitlab.test:frontend/web.git", "main")
	cloneAt(t, e.root, "gh/app", "https://ghe.test/octo/app.git", "main")
	cloneAt(t, e.root, "gh/lib", "git@ghe.test:octo/lib.git", "main")
	return e
}

// tokens are both platforms' tokens.
func (e *mixedE2E) tokens() []string {
	return []string{e.cfg, "GITLAB_TOKEN=" + e2eToken, "E2E_GH_TOKEN=" + ghToken}
}

func (e *mixedE2E) run(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	return checkNoToken(t, e2eToken, ghToken)(runVigie(t, e.root, env, args...))
}

func TestE2EScanMixed(t *testing.T) {
	t.Parallel()
	e := newMixedE2E(t)
	out, errOut, code := e.run(t, e.tokens())
	if code != 0 || errOut != "4 repositories found: 4 listed\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	goldenFile(t, "scan_mixed", out)
}

func TestE2EScanGitHubNoToken(t *testing.T) {
	t.Parallel()
	e := newMixedE2E(t)
	cloneAt(t, e.root, "hub/one", "https://github.com/cli/cli.git", "main")
	cloneAt(t, e.root, "hub/two", "git@github.com:cli/go-gh.git", "main")
	proxy, proxied := recordingProxy(t)
	env := append(e.tokens(), proxy...)
	out, errOut, code := e.run(t, env)
	if code != 0 || errOut != "6 repositories found: 4 listed, 2 skipped (2 no token); run with -v to list them\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	goldenFile(t, "scan_mixed", out)
	_, errOut, code = e.run(t, env, "-v")
	var skipped []string
	for _, l := range strings.Split(errOut, "\n") {
		if strings.Contains(l, "repository skipped") {
			skipped = append(skipped, l)
		}
	}
	if code != 0 || len(skipped) != 2 || !strings.Contains(skipped[0], "dir=hub/one") || !strings.Contains(skipped[1], "dir=hub/two") ||
		!strings.Contains(skipped[0], `reason="no token"`) {
		t.Fatalf("-v: code=%d skipped=%q", code, skipped)
	}
	if n := proxied(); n != 0 {
		t.Fatalf("the proxy received %d requests", n)
	}
}

func TestE2EScanOnlyGitHubComNoToken(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cloneAt(t, root, "one", "https://github.com/cli/cli.git", "main")
	cloneAt(t, root, "two", "git@github.com:cli/go-gh.git", "main")
	proxy, proxied := recordingProxy(t)
	out, errOut, code := checkNoToken(t, "glpat-e2e")(runVigie(t, root, append(proxy, "GITLAB_TOKEN=glpat-e2e")))
	want := "no GitLab or GitHub repository found: 2 skipped (2 no token); export GH_TOKEN for github.com clones, or run with -v\n"
	if code != 2 || out != "" || errOut != want || proxied() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q proxied=%d", code, out, errOut, proxied())
	}
}

func TestE2EScanGitHubRejected(t *testing.T) {
	t.Parallel()
	e := newMixedE2E(t)
	e.gh.SetAll(githubtest.Behavior{Status: http.StatusUnauthorized})
	out, errOut, code := e.run(t, e.tokens())
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for _, dir := range []string{"gh/app", "gh/lib"} {
		if !strings.HasSuffix(rowLine(t, out, dir), "! auth") {
			t.Errorf("%s: %q", dir, rowLine(t, out, dir))
		}
	}
	for _, dir := range []string{"gl/api", "gl/web"} {
		complete(t, rowLine(t, out, dir))
	}
	lines := strings.Split(strings.TrimSuffix(errOut, "\n"), "\n")
	if len(lines) != 2 || lines[0] != "4 repositories found: 4 listed" ||
		!strings.HasPrefix(lines[1], "token rejected by ") || !strings.HasSuffix(lines[1], " (2 repositories)") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestE2EScanGitHubFailingSource(t *testing.T) {
	t.Parallel()
	e := newMixedE2E(t)
	out, errOut, code := e.run(t, []string{e.cfg, "GITLAB_TOKEN=" + e2eToken}) // E2E_GH_TOKEN unset
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for _, dir := range []string{"gh/app", "gh/lib"} {
		if !strings.HasSuffix(rowLine(t, out, dir), "! auth") {
			t.Errorf("%s: %q", dir, rowLine(t, out, dir))
		}
	}
	lines := strings.Split(strings.TrimSuffix(errOut, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "E2E_GH_TOKEN is not set: ") || !strings.HasSuffix(lines[1], " (2 repositories)") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestE2EScanGitHubHostFilter(t *testing.T) {
	t.Parallel()
	e := newMixedE2E(t)
	out, errOut, code := e.run(t, e.tokens(), "--host", e.gh.URL())
	if got := strings.Join(dirs(t, out), " "); code != 0 || got != "gh/app gh/lib" {
		t.Fatalf("code=%d rows=%q stderr=%q", code, got, errOut)
	}
	if want := "4 repositories found: 2 listed, 2 skipped (2 other host); run with -v to list them\n"; errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
	if n := len(e.gl.Requests()); n != 0 {
		t.Fatalf("gitlab.test received %d requests", n)
	}
}
