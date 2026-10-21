package main_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// dirs returns the DIR cell of every row.
func dirs(t *testing.T, out string) []string {
	t.Helper()
	var d []string
	for _, r := range tableRows(t, out) {
		d = append(d, r[0])
	}
	return d
}

func TestE2EScanDepth(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	for _, rel := range []string{"d1", "a/b/d3", "a/b/c/d/e/d6"} {
		cloneAt(t, e.root, rel, "git@gitlab.test:backend/api.git", "main")
	}
	out, errOut, code := e.run(t, nil, "--depth", "3")
	if got := strings.Join(dirs(t, out), " "); code != 0 || got != "a/b/d3 d1" {
		t.Fatalf("--depth 3: code=%d rows=%q stderr=%q", code, got, errOut)
	}
	out, _, code = e.run(t, nil, "--depth=6")
	if got := strings.Join(dirs(t, out), " "); code != 0 || got != "a/b/c/d/e/d6 a/b/d3 d1" {
		t.Fatalf("--depth 6: code=%d rows=%q", code, got)
	}
}

func TestE2EScanDepthInvalid(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	clone := cloneAt(t, e.root, "api", "git@gitlab.test:backend/api.git", "main")
	for _, dir := range []string{e.root, clone} {
		out, errOut, code := checkNoToken(t)(runVigie(t, dir, e.env, "--depth", "0"))
		if code != 2 || out != "" || errOut != "invalid --depth 0: minimum is 1\n" {
			t.Fatalf("%s: code=%d stdout=%q stderr=%q", dir, code, out, errOut)
		}
	}
	out, errOut, code := e.run(t, nil, "--depth", "x")
	if code != 2 || out != "" || !strings.HasSuffix(errOut, "Run 'vigie --help' for usage.\n") {
		t.Fatalf("--depth x: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if n := e.requests(); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

func TestE2EScanDir(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	out, errOut, code := checkNoToken(t)(runVigie(t, t.TempDir(), e.env, "-C", filepath.Join(e.root, "team")))
	if got := strings.Join(dirs(t, out), " "); code != 0 || got != "tools/cli web" {
		t.Fatalf("code=%d rows=%q stderr=%q", code, got, errOut)
	}
	missing := filepath.Join(t.TempDir(), "no", "such", "dir")
	out, errOut, code = e.run(t, nil, "-C", missing)
	if code != 2 || out != "" || errOut != "cannot scan "+missing+": check --dir\n" {
		t.Fatalf("missing dir: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EScanHostFilter(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	out, errOut, code := e.run(t, nil, "--host", e.gl.URL())
	if got := strings.Join(dirs(t, out), " "); code != 0 || got != "api team/web" {
		t.Fatalf("code=%d rows=%q stderr=%q", code, got, errOut)
	}
	if want := "4 repositories found: 2 listed, 2 skipped (2 other host); run with -v to list them\n"; errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
	if n := len(e.other.Requests()); n != 0 {
		t.Fatalf("other.test received %d requests", n)
	}

	out, errOut, code = e.run(t, nil, "--host", "https://gitlab.example.com")
	want := `no GitLab or GitHub repository found: 4 skipped (4 other host); add self-hosted hosts to the config file "hosts", or run with -v` + "\n"
	if code != 2 || out != "" || errOut != want {
		t.Fatalf("no clone on the host: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EScanUnreadableDirectory(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permissions do not apply")
	}
	e := newScanE2E(t)
	e.mainFolder(t)
	locked := filepath.Join(e.root, "locked")
	if err := os.Mkdir(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
	out, errOut, code := e.run(t, nil)
	if code != 0 || len(dirs(t, out)) != 3 ||
		errOut != "5 repositories found: 3 listed, 2 skipped (1 unreadable, 1 unknown host); run with -v to list them\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	_, errOut, _ = e.run(t, nil, "-v")
	if !strings.Contains(errOut, `msg="repository skipped" dir=locked reason=unreadable`) {
		t.Fatalf("-v does not name the directory: %q", errOut)
	}
}

func TestE2EScanDepthIgnoredInsideClone(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, errOut, code := e.run(t, "--depth", "2", "-v")
	if code != 0 || out != wantLine || !strings.Contains(errOut, `msg="--depth ignored: not scanning"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}
