package main_test

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
)

// serverHost returns the host name (no port) of the fake server, the key of its profile.
func serverHost(t *testing.T, srv *gitlabtest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL())
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname()
}

const wantLine = "group/app br 2/3 ci 2r/1p last:ok main 3m mr 5 is 17\n"

func TestE2EProjectAndHostOutsideRepository(t *testing.T) {
	t.Parallel()
	srv := gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
	cfg := writeConfig(t, fmt.Sprintf("hosts:\n  %s:\n    insecure: true\n", serverHost(t, srv)))
	out, errOut, code := checkNoToken(t)(runVigie(t, t.TempDir(), []string{cfg, "GITLAB_TOKEN=" + e2eToken},
		"-p", "group/app", "--host", srv.URL()))
	if code != 0 || out != wantLine {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EGitLabHostFallback(t *testing.T) {
	t.Parallel()
	srv := gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
	cfg := writeConfig(t, fmt.Sprintf("hosts:\n  %s:\n    insecure: true\n", serverHost(t, srv)))
	env := []string{cfg, "GITLAB_TOKEN=" + e2eToken, "GITLAB_HOST=" + srv.URL()}
	out, errOut, code := checkNoToken(t)(runVigie(t, t.TempDir(), env, "-p", "group/app"))
	if code != 0 || out != wantLine {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2ESSHHostMappedToAPI(t *testing.T) {
	t.Parallel()
	srv := gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
	cfg := writeConfig(t, fmt.Sprintf("hosts:\n  ssh.gitlab.test:\n    api: %s\n    insecure: true\n", srv.URL()))
	dir := gitRepo(t, "ssh://git@ssh.gitlab.test:2222/group/app.git", "main")
	out, errOut, code := checkNoToken(t)(runVigie(t, dir, []string{cfg, "GITLAB_TOKEN=" + e2eToken}))
	if code != 0 || out != wantLine {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2ETokenCommand(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	srv := gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
	cfg := writeConfig(t, fmt.Sprintf(
		"hosts:\n  gitlab.test:\n    api: %s\n    insecure: true\n    token_command: [\"sh\", \"-c\", \"echo glpat-fromcmd\"]\n", srv.URL()))
	dir := gitRepo(t, "git@gitlab.test:group/app.git", "main")
	out, errOut, code := runVigie(t, dir, []string{cfg, "GITLAB_TOKEN=" + e2eToken})
	if code != 0 || out != wantLine {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for _, r := range srv.Requests() {
		if r.Token != "glpat-fromcmd" {
			t.Fatalf("request %s used token %q, want the command's", r.Path, r.Token)
		}
	}
	if strings.Contains(out+errOut, "glpat-fromcmd") {
		t.Fatal("command token leaked")
	}
}

func TestE2ETokenInConfigFile(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "hosts:\n  gitlab.test:\n    token: glpat-literal\n")
	dir := gitRepo(t, "git@gitlab.test:group/app.git", "main")
	out, errOut, code := runVigie(t, dir, []string{cfg, "GITLAB_TOKEN=" + e2eToken})
	if code != 2 || out != "" || !strings.Contains(errOut, "contains a token: use token_env or token_command instead") ||
		strings.Contains(errOut, "glpat-literal") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EPlainHTTPRefused(t *testing.T) {
	t.Parallel()
	srv := gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
	cfg := writeConfig(t, fmt.Sprintf("hosts:\n  gitlab.test:\n    api: %s\n", srv.URL()))
	dir := gitRepo(t, "git@gitlab.test:group/app.git", "main")
	out, errOut, code := checkNoToken(t)(runVigie(t, dir, []string{cfg, "GITLAB_TOKEN=" + e2eToken}))
	if code != 2 || out != "" || !strings.HasPrefix(errOut, "refusing to send the token over http://") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("server received %d requests", n)
	}
}
