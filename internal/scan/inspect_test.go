package scan_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/scan"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

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

// clone creates a repository at root/rel on branch main with one commit and, unless
// remoteURL is empty, the remote name pointing at remoteURL.
func clone(t *testing.T, root, rel, name, remoteURL string) scan.Candidate {
	t.Helper()
	dir := filepath.Join(root, rel)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	if remoteURL != "" {
		git(t, dir, "remote", "add", name, remoteURL)
	}
	return scan.Candidate{Path: dir, Rel: rel}
}

func inspect(cands []scan.Candidate, remote string) ([]scan.Inspected, []scan.Skip) {
	return scan.Inspect(context.Background(), cands, remote, slog.New(slog.DiscardHandler))
}

func TestInspect(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root := t.TempDir()
	app := clone(t, root, "app", "origin", "git@gitlab.test:group/app.git")
	detached := clone(t, root, "detached", "origin", "git@gitlab.test:group/d.git")
	git(t, detached.Path, "checkout", "-q", "--detach")
	noRemote := clone(t, root, "noremote", "origin", "")
	fileURL := clone(t, root, "file", "origin", "file:///x")
	broken := scan.Candidate{Path: filepath.Join(root, "broken"), Rel: "broken"}
	tree(t, root, "broken/.git")
	wt := scan.Candidate{Path: filepath.Join(root, "wt"), Rel: "wt"}
	git(t, app.Path, "worktree", "add", "-q", "-b", "feature", wt.Path)

	got, skips := inspect([]scan.Candidate{app, detached, noRemote, fileURL, broken, wt}, "origin")
	if len(got) != 3 || got[0].Rel != "app" || got[1].Rel != "detached" || got[2].Rel != "wt" {
		t.Fatalf("inspected = %+v", got)
	}
	co := got[0].Checkout
	if co.Branch != "main" || co.Remote != (repo.Remote{Host: "gitlab.test", Path: "group/app"}) {
		t.Fatalf("app = %+v", co)
	}
	if got[1].Checkout.Branch != "" || got[2].Checkout.Branch != "feature" || got[2].Checkout.Remote.Path != "group/app" {
		t.Fatalf("detached/worktree = %+v / %+v", got[1].Checkout, got[2].Checkout)
	}
	want := []struct {
		rel    string
		reason scan.Reason
	}{{"noremote", scan.ReasonNoRemote}, {"file", scan.ReasonRemoteURL}, {"broken", scan.ReasonUnreadable}}
	if len(skips) != len(want) {
		t.Fatalf("skips = %+v", skips)
	}
	for i, w := range want {
		if skips[i].Rel != w.rel || skips[i].Reason != w.reason || skips[i].Err == nil {
			t.Errorf("skip %d = %+v, want %s %s", i, skips[i], w.rel, w.reason)
		}
	}
	if !errors.Is(skips[2].Err, repo.ErrNotRepository) {
		t.Errorf("broken: %v does not wrap ErrNotRepository", skips[2].Err)
	}
}

func TestInspectRemoteName(t *testing.T) {
	t.Parallel()
	requireGit(t)
	c := clone(t, t.TempDir(), "up", "upstream", "git@gitlab.test:group/up.git")
	got, skips := inspect([]scan.Candidate{c}, "upstream")
	if len(got) != 1 || len(skips) != 0 || got[0].Checkout.Remote.Path != "group/up" {
		t.Fatalf("upstream: %+v %+v", got, skips)
	}
	_, skips = inspect([]scan.Candidate{c}, "origin")
	if len(skips) != 1 || skips[0].Reason != scan.ReasonNoRemote || !strings.Contains(skips[0].Reason.Text("origin"), `"origin"`) {
		t.Fatalf("origin: %+v", skips)
	}
}

func TestInspectKeepsOrder(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root := t.TempDir()
	var cands []scan.Candidate
	for i := range 12 {
		cands = append(cands, clone(t, root, fmt.Sprintf("r%02d", i), "origin", fmt.Sprintf("git@gitlab.test:group/r%02d.git", i)))
	}
	got, _ := inspect(cands, "origin")
	if len(got) != 12 {
		t.Fatalf("got %d", len(got))
	}
	for i, in := range got {
		if want := fmt.Sprintf("r%02d", i); in.Rel != want || in.Checkout.Remote.Path != "group/"+want {
			t.Fatalf("position %d: %+v", i, in)
		}
	}
}
