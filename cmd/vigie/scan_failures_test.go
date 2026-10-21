package main_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
)

// rowLine returns the stdout line of the row whose DIR is dir.
func rowLine(t *testing.T, out, dir string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, dir+" ") {
			return l
		}
	}
	t.Fatalf("no row %q in %q", dir, out)
	return ""
}

// complete asserts a row shows every fixture value, after DIR, PLATFORM and PROJECT.
func complete(t *testing.T, line string) {
	t.Helper()
	cells := tableRows(t, "DIR \n"+line+"\n")[0]
	if strings.Join(cells[3:], "|") != "2/3|2r/1p|ok main 3m|5|17" {
		t.Errorf("incomplete row %q", line)
	}
}

func rejected(srv *gitlabtest.Server, n string) string {
	return "token rejected by " + strings.TrimPrefix(srv.URL(), "http://") + " (401): check that the token is valid and not expired (" + n + ")"
}

func TestE2EScanNotFoundRow(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	cloneAt(t, e.root, "legacy", "git@gitlab.test:group/legacy.git", "main")
	out, errOut, code := e.run(t, nil)
	if code != 0 || !strings.HasSuffix(rowLine(t, out, "legacy"), "  group/legacy  ! not found") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for _, dir := range []string{"api", "team/tools/cli", "team/web"} {
		complete(t, rowLine(t, out, dir))
	}
	if want := "5 repositories found: 4 listed, 1 skipped (1 unknown host); run with -v to list them\n"; errOut != want {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestE2EScanRejectedHost(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	e.other.SetAll(gitlabtest.Behavior{Status: http.StatusUnauthorized})
	out, errOut, code := e.run(t, nil)
	if code != 0 || !strings.HasSuffix(rowLine(t, out, "team/tools/cli"), "  tools/cli     ! auth") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	complete(t, rowLine(t, out, "api"))
	complete(t, rowLine(t, out, "team/web"))
	want := "4 repositories found: 3 listed, 1 skipped (1 unknown host); run with -v to list them\n" + rejected(e.other, "1 repository") + "\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
	_, errOut, code = e.run(t, nil, "-q")
	if code != 0 || errOut != rejected(e.other, "1 repository")+"\n" {
		t.Fatalf("-q: code=%d stderr=%q", code, errOut)
	}
}

func TestE2EScanUnavailableCell(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	e.gl.Set(gitlabtest.EndpointIssues, gitlabtest.Behavior{Status: http.StatusForbidden})
	out, errOut, code := e.run(t, nil)
	if code != 0 || !strings.HasSuffix(rowLine(t, out, "api"), "  5    –") || !strings.HasSuffix(rowLine(t, out, "team/web"), "  5    –") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	complete(t, rowLine(t, out, "team/tools/cli"))
}

func TestE2EScanAllRejected(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	e.gl.SetAll(gitlabtest.Behavior{Status: http.StatusUnauthorized})
	e.other.SetAll(gitlabtest.Behavior{Status: http.StatusUnauthorized})
	out, errOut, code := e.run(t, nil)
	want := rejected(e.gl, "2 repositories") + "\n" + rejected(e.other, "1 repository") + "\n" +
		"all 3 listed repositories failed; 1 skipped (1 unknown host); run with -v to list them\n"
	if code != 3 || out != "" || errOut != want {
		t.Fatalf("code=%d stdout=%q stderr=%q\nwant stderr %q", code, out, errOut, want)
	}
}

func TestE2EScanAllFailedMixed(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	cloneAt(t, e.root, "a", "git@other.test:tools/cli.git", "main")
	cloneAt(t, e.root, "b", "git@gitlab.test:group/missing.git", "main")
	e.other.SetAll(gitlabtest.Behavior{Status: http.StatusUnauthorized})
	out, errOut, code := e.run(t, nil)
	want := rejected(e.other, "1 repository") + "\n" +
		"b: project group/missing not found on " + e.gl.URL() + ": check the hosts mapping in the config file\n" +
		"all 2 listed repositories failed\n"
	if code != 1 || out != "" || errOut != want {
		t.Fatalf("code=%d stdout=%q stderr=%q\nwant stderr %q", code, out, errOut, want)
	}
}

func TestE2EScanThrottled(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	e.gl.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusTooManyRequests, RetryAfter: "0", Times: 2})
	out, errOut, code := e.run(t, nil)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	goldenFile(t, "scan_table", out)
}
