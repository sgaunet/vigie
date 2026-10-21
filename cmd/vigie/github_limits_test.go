package main_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/github/githubtest"
	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
	"github.com/sgaunet/vigie/internal/scan"
)

var allowanceLine = regexp.MustCompile(`^\S+ request allowance exhausted until \d\d:\d\d \(in 1[23]m\): wait for the reset; it is shared by all your GitHub tokens\n$`)

func TestE2EGitHubAllowanceExhausted(t *testing.T) {
	t.Parallel()
	e := newGitHubE2E(t)
	e.srv.SetAll(githubtest.Behavior{RateLimited: true, Reset: time.Now().Add(13 * time.Minute)})
	out, errOut, code := e.run(t)
	if code != 5 || out != "" || !allowanceLine.MatchString(errOut) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EGitHubSecondaryLimit(t *testing.T) {
	t.Parallel()
	e := newGitHubE2E(t)
	e.srv.Set(githubtest.EndpointRepo, githubtest.Behavior{Secondary: true, Header: map[string]string{"Retry-After": "1"}})
	out, errOut, code := e.run(t)
	if code != 5 || out != "" || strings.Count(errOut, "\n") != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EScanGitHubAllowanceLatch(t *testing.T) {
	t.Parallel()
	gl := gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
	gh := githubtest.New(t, githubtest.Fixture(time.Now()))
	gh.SetAll(githubtest.Behavior{RateLimited: true, Reset: time.Now().Add(13 * time.Minute)})
	root := t.TempDir()
	for _, d := range []string{"gh/a", "gh/b", "gh/c"} {
		cloneAt(t, root, d, ghCloneURL, "main")
	}
	cloneAt(t, root, "gl", "git@gitlab.test:group/app.git", "main")
	env := []string{
		writeConfig(t, fmt.Sprintf("hosts:\n  gitlab.test:\n    api: %s\n    insecure: true\n", gl.URL())+ghConfig("ghe.test", gh)),
		"GITLAB_TOKEN=" + e2eToken, "E2E_GH_TOKEN=" + ghToken,
	}
	latched := regexp.MustCompile(`request allowance exhausted until .* \(3 repositories\)$`)
	for i, args := range [][]string{nil, {"-q"}} {
		before := len(gh.Requests())
		out, errOut, code := checkNoToken(t, e2eToken, ghToken)(runVigie(t, root, env, args...))
		if code != 0 {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
		for _, d := range []string{"gh/a", "gh/b", "gh/c"} {
			if !strings.HasSuffix(rowLine(t, out, d), "! network") {
				t.Errorf("%v %s: %q", args, d, rowLine(t, out, d))
			}
		}
		complete(t, rowLine(t, out, "gl"))
		var matching int
		for _, l := range strings.Split(strings.TrimSuffix(errOut, "\n"), "\n") {
			if latched.MatchString(l) {
				matching++
			}
		}
		if matching != 1 {
			t.Fatalf("%v: stderr = %q", args, errOut)
		}
		if n := len(gh.Requests()) - before; n > scan.ReposPerHost {
			t.Fatalf("run %d: githubtest received %d requests", i, n)
		}
	}
}
