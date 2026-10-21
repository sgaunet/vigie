package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// gitTimeout bounds each git subprocess.
const gitTimeout = 5 * time.Second

// Checkout is the git working copy vigie runs in.
type Checkout struct {
	Root   string // top-level directory
	Branch string // current branch; "" when HEAD is detached
	Remote Remote // the selected remote, parsed; the raw URL is never kept
}

// Detect finds the repository containing dir and parses the URL of the named remote,
// with the user's git configuration (including insteadOf rewrites) applied.
func Detect(ctx context.Context, dir, remote string, log *slog.Logger) (Checkout, error) {
	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Checkout{}, notRepository(err)
	}
	// Exit status 1 with -q means a detached HEAD.
	branch, _ := git(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")

	raw, err := git(ctx, dir, "remote", "get-url", remote)
	if err != nil {
		return Checkout{}, missingRemote(ctx, dir, remote)
	}
	r, err := ParseRemote(raw)
	if err != nil {
		return Checkout{}, err
	}
	log.Debug("git checkout detected", "root", root, "branch", branch, "remote", remote, "host", r.Host, "path", r.Path)
	return Checkout{Root: root, Branch: branch, Remote: r}, nil
}

func notRepository(err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: git is %w, install it or pass --project and --host", ErrNotRepository, ErrNoGit)
	}
	return fmt.Errorf("%w: run vigie inside a clone, or pass --project and --host", ErrNotRepository)
}

func missingRemote(ctx context.Context, dir, remote string) error {
	out, _ := git(ctx, dir, "remote")
	names := strings.Fields(out)
	if len(names) == 0 {
		return fmt.Errorf("remote %q %w: the repository has no remote, add one or pass --project and --host", remote, ErrRemoteMissing)
	}
	return fmt.Errorf("remote %q %w: existing remotes are %s", remote, ErrRemoteMissing, strings.Join(names, ", "))
}

// git runs one git command in dir and returns its trimmed stdout. Stderr is discarded:
// it may echo a remote URL with credentials.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(out.String()), nil
}
