package main_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/github/githubtest"
	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
)

// Distinct token values, one per kind of host (SC-004).
const (
	glTok   = "gl-e2e"
	ghTok   = "gh-e2e"
	gheTok  = "ghe-e2e"
	ghebTok = "gheb-e2e"
)

// tokensE2E is gitlab.test plus two Enterprise Servers: ghe-a.test without a token source of
// its own, and ghe-b.test with token_env GHE_B_TOKEN.
type tokensE2E struct {
	gl   *gitlabtest.Server
	a, b *githubtest.Server
	cfg  string
	env  []string
}

func newTokensE2E(t *testing.T) *tokensE2E {
	t.Helper()
	gl := gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
	a := githubtest.New(t, githubtest.Fixture(time.Now()))
	b := githubtest.New(t, githubtest.Fixture(time.Now()))
	cfg := writeConfig(t, fmt.Sprintf(`hosts:
  gitlab.test:
    api: %s
    insecure: true
  ghe-a.test:
    api: %s
    platform: github
    insecure: true
  ghe-b.test:
    api: %s
    platform: github
    insecure: true
    token_env: GHE_B_TOKEN
`, gl.URL(), a.URL(), b.URL()))
	return &tokensE2E{gl: gl, a: a, b: b, cfg: cfg, env: []string{cfg,
		"GITLAB_TOKEN=" + glTok, "GH_TOKEN=" + ghTok, "GH_ENTERPRISE_TOKEN=" + gheTok, "GHE_B_TOKEN=" + ghebTok}}
}

var allTokens = []string{glTok, ghTok, gheTok, ghebTok}

func TestE2ETokenSeparation(t *testing.T) {
	t.Parallel()
	e := newTokensE2E(t)
	root := t.TempDir()
	clones := map[string]string{
		"gl":    "git@gitlab.test:group/app.git",
		"ghe-a": "https://ghe-a.test/octo/app.git",
		"ghe-b": "git@ghe-b.test:octo/app.git",
	}
	for dir, origin := range clones {
		clone := cloneAt(t, root, dir, origin, "main")
		out, errOut, code := checkNoToken(t, allTokens...)(runVigie(t, clone, e.env))
		if code != 0 {
			t.Fatalf("%s: code=%d stdout=%q stderr=%q", dir, code, out, errOut)
		}
	}
	out, errOut, code := checkNoToken(t, allTokens...)(runVigie(t, root, e.env))
	if code != 0 || errOut != "3 repositories found: 3 listed\n" {
		t.Fatalf("scan: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for _, dir := range []string{"ghe-a", "ghe-b"} {
		if !strings.Contains(rowLine(t, out, dir), " github ") {
			t.Errorf("%s: %q", dir, rowLine(t, out, dir))
		}
	}

	for _, r := range e.gl.Requests() {
		if r.Token != glTok {
			t.Fatalf("gitlab.test saw token %q", r.Token)
		}
	}
	if got := e.a.Tokens(); !slices.Equal(got, []string{"Bearer " + gheTok}) {
		t.Errorf("ghe-a.test saw %q", got)
	}
	if got := e.b.Tokens(); !slices.Equal(got, []string{"Bearer " + ghebTok}) {
		t.Errorf("ghe-b.test saw %q", got)
	}
}

func TestE2EEnterpriseTokenMissing(t *testing.T) {
	t.Parallel()
	e := newTokensE2E(t)
	clone := gitRepo(t, "https://ghe-a.test/octo/app.git", "main")
	out, errOut, code := checkNoToken(t, allTokens...)(runVigie(t, clone, []string{e.cfg, "GITLAB_TOKEN=" + glTok, "GH_TOKEN=" + ghTok}))
	want := "GH_ENTERPRISE_TOKEN and GITHUB_ENTERPRISE_TOKEN are not set: create a token with read access to the repository and export it, or set token_env or token_command for ghe-a.test in the config file\n"
	if code != 3 || out != "" || errOut != want {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if n := len(e.a.Requests()); n != 0 {
		t.Fatalf("ghe-a.test received %d requests", n)
	}
}

func TestE2EOldConfigIsGitLab(t *testing.T) {
	t.Parallel()
	e := newE2E(t) // the 001 config: no platform anywhere
	out, errOut, code := e.run(t)
	if code != 0 || out != "group/app br 2/3 ci 2r/1p last:ok main 3m mr 5 is 17\n" || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}
