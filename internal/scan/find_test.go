package scan_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/sgaunet/vigie/internal/scan"
)

// tree creates the directories rels below root; a trailing "/.git" makes a fake clone.
func tree(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o750); err != nil {
			t.Fatal(err)
		}
	}
}

func writeFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func find(t *testing.T, root string, depth int) ([]scan.Candidate, []scan.Skip) {
	t.Helper()
	cands, skips, err := scan.Find(context.Background(), root, depth)
	if err != nil {
		t.Fatalf("Find(%s): %v", root, err)
	}
	return cands, skips
}

func rels(cands []scan.Candidate) []string {
	out := []string{}
	for _, c := range cands {
		out = append(out, c.Rel)
	}
	return out
}

func TestFindDepth(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree(t, root, "d1/.git", "a/d2/.git", "a/b/c/d4/.git", "a/b/c/d/d5/.git")
	cands, skips := find(t, root, 4)
	if got := rels(cands); !slices.Equal(got, []string{"a/b/c/d4", "a/d2", "d1"}) || len(skips) != 0 {
		t.Fatalf("depth 4: %v, skips %v", got, skips)
	}
	cands, _ = find(t, root, 1)
	if got := rels(cands); !slices.Equal(got, []string{"d1"}) {
		t.Fatalf("depth 1: %v", got)
	}
}

func TestFindEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree(t, root, "a/.git", "a/sub/.git", ".cache/x/.git", "plain/dir")
	writeFile(t, filepath.Join(root, "wt", ".git")) // worktree or submodule: .git is a file
	writeFile(t, filepath.Join(root, "file"))
	cands, skips := find(t, root, 4)
	if got := rels(cands); !slices.Equal(got, []string{"a", "wt"}) || len(skips) != 0 {
		t.Fatalf("got %v, skips %v", got, skips)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if !filepath.IsAbs(c.Path) || c.Path != filepath.Join(resolved, c.Rel) {
			t.Errorf("Path = %q for %q", c.Path, c.Rel)
		}
	}
}

func TestFindHiddenRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), ".hidden")
	tree(t, root, "x/.git")
	cands, _ := find(t, root, 4)
	if got := rels(cands); !slices.Equal(got, []string{"x"}) {
		t.Fatalf("got %v", got)
	}
}

func TestFindTreeOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree(t, root, "b/.git", "a-b/.git", "a/x/.git", "team/tools/cli/.git")
	cands, _ := find(t, root, 4)
	if got := rels(cands); !slices.Equal(got, []string{"a/x", "a-b", "b", "team/tools/cli"}) {
		t.Fatalf("got %v", got)
	}
}

func TestFindBare(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree(t, root, "b.git/objects", "b.git/refs/x/.git", "c/.git")
	writeFile(t, filepath.Join(root, "b.git", "HEAD"))
	cands, skips := find(t, root, 4)
	if got := rels(cands); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("got %v", got)
	}
	if len(skips) != 1 || skips[0].Rel != "b.git" || skips[0].Reason != scan.ReasonBare {
		t.Fatalf("skips = %+v", skips)
	}
}

func TestFindSymlinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree(t, root, "clone/.git")
	if err := os.Symlink(filepath.Join(root, "clone"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks: %v", err)
	}
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	cands, skips := find(t, root, 4)
	if got := rels(cands); !slices.Equal(got, []string{"clone"}) || len(skips) != 0 {
		t.Fatalf("got %v, skips %v", got, skips)
	}

	// A root reached through a symbolic link is scanned.
	via := filepath.Join(t.TempDir(), "via")
	if err := os.Symlink(root, via); err != nil {
		t.Fatal(err)
	}
	cands, _ = find(t, via, 4)
	if got := rels(cands); !slices.Equal(got, []string{"clone"}) {
		t.Fatalf("through a link: %v", got)
	}
}

func TestFindUnreadable(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permissions do not apply")
	}
	root := t.TempDir()
	tree(t, root, "a/.git", "locked/x/.git", "z/.git")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
	cands, skips := find(t, root, 4)
	if got := rels(cands); !slices.Equal(got, []string{"a", "z"}) {
		t.Fatalf("got %v", got)
	}
	if len(skips) != 1 || skips[0].Rel != "locked" || skips[0].Reason != scan.ReasonUnreadable || skips[0].Err == nil {
		t.Fatalf("skips = %+v", skips)
	}
	// At the last level the directory is probed, not read: it is still reported.
	_, skips = find(t, root, 1)
	if len(skips) != 1 || skips[0].Rel != "locked" || skips[0].Reason != scan.ReasonUnreadable {
		t.Fatalf("depth 1: skips = %+v", skips)
	}
}

func TestFindBadRoot(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file)
	for _, root := range []string{filepath.Join(t.TempDir(), "missing"), file} {
		_, _, err := scan.Find(context.Background(), root, 4)
		if !errors.Is(err, scan.ErrRoot) || err.Error() != "cannot scan "+root+": check --dir" {
			t.Errorf("%s: got %v", root, err)
		}
	}
}

func TestFindCanceled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree(t, root, "a/.git", "b/.git")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := scan.Find(ctx, root, 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

// BenchmarkFind10k walks 10,000 directories holding no repository (SC-002: under 2 s).
func BenchmarkFind10k(b *testing.B) {
	root := b.TempDir()
	for i := range 100 {
		for j := range 99 {
			if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("d%02d", i), fmt.Sprintf("e%02d", j)), 0o750); err != nil {
				b.Fatal(err)
			}
		}
	}
	for b.Loop() {
		cands, _, err := scan.Find(context.Background(), root, 4)
		if err != nil || len(cands) != 0 {
			b.Fatalf("Find: %d candidates, %v", len(cands), err)
		}
	}
}
