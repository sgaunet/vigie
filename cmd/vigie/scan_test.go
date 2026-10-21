package main_test

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// cloneAt creates a repository at root/rel on branch with one commit and the given origin.
func cloneAt(t *testing.T, root, rel, origin, branch string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", branch)
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	git(t, dir, "remote", "add", "origin", origin)
	return dir
}

// scanE2E is a folder of clones and two fake GitLab hosts, gitlab.test and other.test.
// Every project is a copy of the fixture, so every row shows the fixture values.
type scanE2E struct {
	gl, other *gitlabtest.Server
	root      string
	env       []string
}

func newScanE2E(t *testing.T) *scanE2E {
	t.Helper()
	now := time.Now()
	gl := gitlabtest.New(t, gitlabtest.Fixture(now))
	other := gitlabtest.New(t, gitlabtest.Fixture(now))
	for i, p := range []struct {
		srv  *gitlabtest.Server
		path string
	}{{gl, "backend/api"}, {gl, "frontend/web"}, {other, "tools/cli"}} {
		proj := gitlabtest.Fixture(now)
		proj.ID, proj.Path = int64(i+1), p.path
		p.srv.AddProject(proj)
	}
	cfg := writeConfig(t, fmt.Sprintf("hosts:\n  gitlab.test:\n    api: %s\n    insecure: true\n  other.test:\n    api: %s\n    insecure: true\n",
		gl.URL(), other.URL()))
	return &scanE2E{gl: gl, other: other, root: t.TempDir(), env: []string{cfg, "GITLAB_TOKEN=" + e2eToken}}
}

// mainFolder fills the root with three listed clones on two hosts, a clone nested in one of
// them, a hidden clone, a GitHub clone and a plain directory.
func (e *scanE2E) mainFolder(t *testing.T) {
	t.Helper()
	cloneAt(t, e.root, "api", "git@gitlab.test:backend/api.git", "main")
	cloneAt(t, e.root, "team/tools/cli", "git@other.test:tools/cli.git", "main")
	cloneAt(t, e.root, "team/web", "git@gitlab.test:frontend/web.git", "main")
	cloneAt(t, e.root, "team/web/vendor/lib", "git@gitlab.test:vendor/lib.git", "main")
	cloneAt(t, e.root, ".hidden/x", "git@gitlab.test:hidden/x.git", "main")
	cloneAt(t, e.root, "github", "https://github.com/git/git.git", "main")
	if err := os.MkdirAll(filepath.Join(e.root, "plain", "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
}

func (e *scanE2E) run(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	return checkNoToken(t)(runVigie(t, e.root, append(append([]string(nil), e.env...), env...), args...))
}

func (e *scanE2E) requests() int { return len(e.gl.Requests()) + len(e.other.Requests()) }

// recordingProxy counts every request it receives, CONNECT included, and returns the
// proxy variables pointing at it. Loopback hosts (the fake GitLab servers) bypass it.
func recordingProxy(t *testing.T) ([]string, func() int64) {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		http.Error(w, "recording proxy", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	return []string{"HTTPS_PROXY=" + srv.URL, "HTTP_PROXY=" + srv.URL}, n.Load
}

func goldenFile(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n got: %q\nwant: %q", name, got, want)
	}
}

func TestE2EScanTable(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	proxy, proxied := recordingProxy(t)
	out, errOut, code := e.run(t, proxy)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	goldenFile(t, "scan_table", out)
	if want := "4 repositories found: 3 listed, 1 skipped (1 unknown host); run with -v to list them\n"; errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
	if n := proxied(); n != 0 {
		t.Fatalf("the proxy received %d requests", n)
	}
	for _, srv := range []*gitlabtest.Server{e.gl, e.other} {
		for _, r := range srv.Requests() {
			if r.Token != e2eToken {
				t.Fatalf("request %s sent token %q", r.Path, r.Token)
			}
			if strings.Contains(r.Path, "vendor") || strings.Contains(r.Path, "hidden") {
				t.Fatalf("request for a clone that must not be listed: %s", r.Path)
			}
		}
	}

	again, _, _ := e.run(t, nil)
	if again != out {
		t.Fatalf("second run differs (SC-006):\n%q\n%q", out, again)
	}
	out, errOut, code = e.run(t, nil, "-q")
	if code != 0 || out != again || errOut != "" {
		t.Fatalf("-q: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	_, errOut, code = e.run(t, nil, "-v")
	var skipped []string
	for _, l := range strings.Split(errOut, "\n") {
		if strings.Contains(l, "level=DEBUG") && strings.Contains(l, "repository skipped") {
			skipped = append(skipped, l)
		}
	}
	if code != 0 || len(skipped) != 1 || !strings.Contains(skipped[0], "dir=github") || !strings.Contains(skipped[0], `reason="unknown host"`) {
		t.Fatalf("-v: code=%d skipped=%q", code, skipped)
	}
}

func TestE2EScanUnknownHostsOnly(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	cloneAt(t, e.root, "a", "https://github.com/git/git.git", "main")
	cloneAt(t, e.root, "b", "git@unknown.test:group/b.git", "main")
	proxy, proxied := recordingProxy(t)
	out, errOut, code := e.run(t, proxy)
	want := `no GitLab repository found: 2 skipped (2 unknown host); add self-hosted hosts to the config file "hosts", or run with -v` + "\n"
	if code != 2 || out != "" || errOut != want {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if n := proxied(); n != 0 || e.requests() != 0 {
		t.Fatalf("requests: proxy %d, servers %d", n, e.requests())
	}
}

func TestE2EScanEmptyFolder(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	out, errOut, code := e.run(t, nil)
	want := "not a git repository: no clone found below it either (depth 4); run vigie in a clone or a folder of clones, or pass --project and --host\n"
	if code != 2 || out != "" || errOut != want {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

// tableRows returns the data lines of a table, split into cells on runs of spaces.
func tableRows(t *testing.T, out string) [][]string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) < 1 || !strings.HasPrefix(lines[0], "DIR ") {
		t.Fatalf("no header: %q", out)
	}
	var rows [][]string
	for _, l := range lines[1:] {
		var cells []string
		for _, c := range strings.Split(l, "  ") {
			if c = strings.TrimSpace(c); c != "" {
				cells = append(cells, c)
			}
		}
		rows = append(rows, cells)
	}
	return rows
}

func TestE2EScanSameProjectTwice(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	cloneAt(t, e.root, "a", "git@gitlab.test:backend/api.git", "main")
	cloneAt(t, e.root, "b", "git@gitlab.test:backend/api.git", "main")
	out, errOut, code := e.run(t, nil)
	rows := tableRows(t, out)
	if code != 0 || len(rows) != 2 || rows[0][0] != "a" || rows[1][0] != "b" || rows[0][1] != "backend/api" || rows[1][1] != "backend/api" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EScanWorktree(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	a := cloneAt(t, e.root, "a", "git@gitlab.test:backend/api.git", "main")
	git(t, a, "worktree", "add", "-q", "-b", "feature", filepath.Join(e.root, "b"))
	out, errOut, code := e.run(t, nil)
	rows := tableRows(t, out)
	if code != 0 || len(rows) != 2 || rows[1][0] != "b" || rows[0][4] != "ok main 3m" || rows[1][4] != "fail feature 1h" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EScanInterrupted(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM")
	}
	e := newScanE2E(t)
	e.mainFolder(t)
	e.gl.SetAll(gitlabtest.Behavior{Delay: 5 * time.Second})
	e.other.SetAll(gitlabtest.Behavior{Delay: 5 * time.Second})
	cmd := exec.Command(binary)
	cmd.Dir = e.root
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}, e.env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 || stdout.Len() != 0 || time.Since(start) > 3*time.Second {
		t.Fatalf("err=%v after %v stdout=%q stderr=%q", err, time.Since(start), stdout.String(), stderr.String())
	}
}

func TestE2EScanWatchRefused(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	out, errOut, code := e.run(t, nil, "-w")
	if code != 2 || out != "" || !strings.Contains(errOut, "--watch needs a single repository: run it inside a clone or pass --project") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if n := e.requests(); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

func TestE2EScanNotInsideAClone(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	cloneAt(t, e.repo, "sub/nested", "git@gitlab.test:group/nested.git", "main")
	out, errOut, code := e.run(t)
	if code != 0 || out != wantLine {
		t.Fatalf("inside a clone: code=%d stdout=%q stderr=%q", code, out, errOut)
	}

	s := newScanE2E(t)
	s.mainFolder(t)
	out, errOut, code = s.run(t, nil, "-p", "group/app", "--host", s.gl.URL())
	if code != 0 || out != wantLine {
		t.Fatalf("--project: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if n := len(s.other.Requests()); n != 0 {
		t.Fatalf("--project scanned: %d requests to other.test", n)
	}
}
