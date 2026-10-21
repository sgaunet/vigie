package repo_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgaunet/vigie/internal/repo"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// git runs a git command in dir with a neutral identity and no user config.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo creates a repository with one commit on branch main and the given origin.
func newRepo(t *testing.T, origin string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	if origin != "" {
		git(t, dir, "remote", "add", "origin", origin)
	}
	return dir
}

func detect(t *testing.T, dir, remote string, log *slog.Logger) (repo.Checkout, error) {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return repo.Detect(context.Background(), dir, remote, log)
}

func TestDetect(t *testing.T) {
	t.Parallel()
	requireGit(t)
	dir := newRepo(t, "git@gitlab.com:group/sub/app.git")
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	c, err := detect(t, sub, "origin", nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if c.Root != dir || c.Branch != "main" || c.Remote != (repo.Remote{Host: "gitlab.com", Path: "group/sub/app"}) {
		t.Fatalf("Detect = %+v", c)
	}
}

func TestDetectStripsCredentials(t *testing.T) {
	t.Parallel()
	requireGit(t)
	dir := newRepo(t, "https://user:s3cr3t@gitlab.test/g/p.git")
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c, err := detect(t, dir, "origin", log)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if c.Remote != (repo.Remote{Host: "gitlab.test", Path: "g/p"}) {
		t.Fatalf("Remote = %+v", c.Remote)
	}
	if strings.Contains(logs.String(), "s3cr3t") || strings.Contains(c.Root+c.Branch, "s3cr3t") {
		t.Fatalf("credentials leaked: %q", logs.String())
	}
	if logs.Len() == 0 {
		t.Fatal("expected debug logs")
	}
}

func TestDetectDetachedHead(t *testing.T) {
	t.Parallel()
	requireGit(t)
	dir := newRepo(t, "git@gitlab.com:g/p.git")
	git(t, dir, "checkout", "-q", "--detach")
	c, err := detect(t, dir, "origin", nil)
	if err != nil || c.Branch != "" {
		t.Fatalf("detached: %+v, %v", c, err)
	}
}

func TestDetectInsteadOf(t *testing.T) {
	t.Parallel()
	requireGit(t)
	dir := newRepo(t, "gl:group/app.git")
	git(t, dir, "config", "url.git@gitlab.example.com:.insteadOf", "gl:")
	c, err := detect(t, dir, "origin", nil)
	if err != nil || c.Remote != (repo.Remote{Host: "gitlab.example.com", Path: "group/app"}) {
		t.Fatalf("insteadOf: %+v, %v", c, err)
	}
}

func TestDetectMissingRemote(t *testing.T) {
	t.Parallel()
	requireGit(t)
	dir := newRepo(t, "git@gitlab.com:g/p.git")
	git(t, dir, "remote", "add", "fork", "git@gitlab.com:me/p.git")
	_, err := detect(t, dir, "upstream", nil)
	if !errors.Is(err, repo.ErrRemoteMissing) {
		t.Fatalf("got %v, want ErrRemoteMissing", err)
	}
	if err.Error() != `remote "upstream" not found: existing remotes are fork, origin` {
		t.Fatalf("message = %q", err)
	}
	_, err = detect(t, newRepo(t, ""), "origin", nil)
	if !errors.Is(err, repo.ErrRemoteMissing) || !strings.Contains(err.Error(), "no remote") {
		t.Fatalf("no remotes: %v", err)
	}
}

func TestDetectNotRepository(t *testing.T) {
	t.Parallel()
	requireGit(t)
	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		_, err := detect(t, dir, "origin", nil)
		if !errors.Is(err, repo.ErrNotRepository) {
			t.Fatalf("%s: got %v, want ErrNotRepository", dir, err)
		}
		if !strings.HasPrefix(err.Error(), "not a git repository: ") {
			t.Fatalf("message = %q", err)
		}
	}
}

// TestDetectNoGit is not parallel: it empties PATH.
func TestDetectNoGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := detect(t, t.TempDir(), "origin", nil)
	if !errors.Is(err, repo.ErrNoGit) || !errors.Is(err, repo.ErrNotRepository) {
		t.Fatalf("got %v, want ErrNoGit and ErrNotRepository", err)
	}
	if want := "not a git repository: git is not installed, install it or pass --project and --host"; err.Error() != want {
		t.Fatalf("message = %q, want %q", err, want)
	}
}

func TestDetectNotRepositoryIsNotNoGit(t *testing.T) {
	t.Parallel()
	requireGit(t)
	_, err := detect(t, t.TempDir(), "origin", nil)
	if !errors.Is(err, repo.ErrNotRepository) || errors.Is(err, repo.ErrNoGit) {
		t.Fatalf("got %v, want ErrNotRepository only", err)
	}
}

func TestDetectWorktree(t *testing.T) {
	t.Parallel()
	requireGit(t)
	dir := newRepo(t, "git@gitlab.com:g/p.git")
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, dir, "worktree", "add", "-q", "-b", "feature", wt)
	c, err := detect(t, wt, "origin", nil)
	if err != nil || c.Branch != "feature" || c.Remote.Path != "g/p" {
		t.Fatalf("worktree: %+v, %v", c, err)
	}
}
