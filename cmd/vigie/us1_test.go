package main_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
)

const e2eToken = "glpat-e2eS3cr3tT0ken"

// git runs a git command in dir, isolated from the user's configuration.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitRepo creates a repository on branch with one commit and the given origin.
func gitRepo(t *testing.T, origin, branch string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", branch)
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	git(t, dir, "remote", "add", "origin", origin)
	return dir
}

// writeConfig writes a config file and returns the VIGIE_CONFIG entry pointing at it.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return "VIGIE_CONFIG=" + p
}

// e2e is a fake GitLab at gitlab.test plus a clone of group/app.
type e2e struct {
	srv  *gitlabtest.Server
	repo string
	env  []string
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	srv := gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
	cfg := writeConfig(t, fmt.Sprintf("hosts:\n  gitlab.test:\n    api: %s\n    insecure: true\n", srv.URL()))
	return &e2e{
		srv:  srv,
		repo: gitRepo(t, "git@gitlab.test:group/app.git", "main"),
		env:  []string{cfg, "GITLAB_TOKEN=" + e2eToken},
	}
}

func (e *e2e) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return checkNoToken(t)(runVigie(t, e.repo, e.env, args...))
}

// checkNoToken asserts the token appears in neither stream (SC-003) and that a failed
// run printed nothing on stdout.
func checkNoToken(t *testing.T) func(string, string, int) (string, string, int) {
	t.Helper()
	return func(out, errOut string, code int) (string, string, int) {
		t.Helper()
		if strings.Contains(out, e2eToken) || strings.Contains(errOut, e2eToken) {
			t.Fatalf("token leaked: stdout=%q stderr=%q", out, errOut)
		}
		if code != 0 && out != "" {
			t.Fatalf("exit %d with stdout %q", code, out)
		}
		return out, errOut, code
	}
}

func TestE2EStatusLine(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, errOut, code := e.run(t)
	want := "group/app br 2/3 ci 2r/1p last:ok main 3m mr 5 is 17\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for _, r := range e.srv.Requests() {
		if r.Token != e2eToken {
			t.Fatalf("request %s sent token %q", r.Path, r.Token)
		}
	}
}

func TestE2EBranchAndDetachedHead(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	git(t, e.repo, "checkout", "-q", "-b", "feature")
	out, _, code := e.run(t)
	if code != 0 || !strings.Contains(out, "last:fail feature 1h") {
		t.Fatalf("feature branch: code=%d stdout=%q", code, out)
	}
	git(t, e.repo, "checkout", "-q", "--detach")
	out, _, code = e.run(t)
	if code != 0 || !strings.Contains(out, "last:ok main 3m") {
		t.Fatalf("detached: code=%d stdout=%q", code, out)
	}
}

func TestE2EUnavailableField(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.srv.Set(gitlabtest.EndpointIssues, gitlabtest.Behavior{Status: http.StatusForbidden})
	out, errOut, code := e.run(t)
	if code != 0 || !strings.HasSuffix(out, "mr 5 is –\n") || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2ENotARepository(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, errOut, code := checkNoToken(t)(runVigie(t, t.TempDir(), e.env))
	if code != 2 || out != "" || !strings.HasPrefix(errOut, "not a git repository: ") || strings.Count(errOut, "\n") != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EMissingToken(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, errOut, code := runVigie(t, e.repo, []string{e.env[0], "GITLAB_TOKEN="})
	want := "GITLAB_TOKEN is not set: create a token with the read_api scope and export it\n"
	if code != 3 || out != "" || errOut != want {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if n := len(e.srv.Requests()); n != 0 {
		t.Fatalf("%d requests sent without a token", n)
	}
}

func TestE2ETokenRejected(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.srv.SetAll(gitlabtest.Behavior{Status: http.StatusUnauthorized})
	_, errOut, code := e.run(t)
	if code != 3 || !strings.HasPrefix(errOut, "token rejected by 127.0.0.1:") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestE2EProjectNotFound(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	git(t, e.repo, "remote", "set-url", "origin", "git@gitlab.test:group/missing.git")
	_, errOut, code := e.run(t)
	if code != 4 || !strings.HasPrefix(errOut, "project group/missing not found on ") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestE2ENetworkError(t *testing.T) {
	t.Parallel()
	dir := gitRepo(t, "git@down.test:group/app.git", "main")
	cfg := writeConfig(t, "hosts:\n  down.test:\n    api: http://127.0.0.1:1\n    insecure: true\n")
	out, errOut, code := checkNoToken(t)(runVigie(t, dir, []string{cfg, "GITLAB_TOKEN=" + e2eToken}))
	if code != 5 || out != "" || !strings.HasPrefix(errOut, "cannot reach 127.0.0.1:1: ") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}
