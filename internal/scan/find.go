package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Candidate is a directory holding a .git entry.
type Candidate struct {
	Path string // absolute OS path, given to repo.Detect
	Rel  string // relative to the root, /-separated; becomes the row's DIR
}

// errRootUnreadable stops the walk when the root itself cannot be read.
var errRootUnreadable = errors.New("root unreadable")

// Find walks root at most depth levels down (its children are level 1) and returns the
// directories holding a .git entry, in tree order. It never descends into a clone, a
// hidden directory or a bare repository, and never follows symbolic links. Bare
// repositories and unreadable directories are returned as skips.
func Find(ctx context.Context, root string, depth int) ([]Candidate, []Skip, error) {
	abs, err := filepath.Abs(root)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	var info fs.FileInfo
	if err == nil {
		info, err = os.Stat(abs)
	}
	if err != nil || !info.IsDir() {
		return nil, nil, fmt.Errorf("%w %s: check --dir", ErrRoot, root)
	}

	var (
		cands []Candidate
		skips []Skip
	)
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if p == abs {
			if err != nil {
				return errRootUnreadable
			}
			return nil
		}
		rel, relErr := filepath.Rel(abs, p)
		if relErr != nil {
			return fmt.Errorf("relative path of %s: %w", p, relErr)
		}
		rel = filepath.ToSlash(rel)
		switch {
		case err != nil: // the directory could not be read; the walk goes on
			skips = append(skips, Skip{Rel: rel, Reason: ReasonUnreadable, Err: err})
			return nil
		case !d.IsDir(): // files and symbolic links
			return nil
		case strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		}
		switch _, err := os.Lstat(filepath.Join(p, ".git")); {
		case err == nil:
			cands = append(cands, Candidate{Path: p, Rel: rel})
			return filepath.SkipDir
		case !errors.Is(err, fs.ErrNotExist): // e.g. no permission, also at the last level
			skips = append(skips, Skip{Rel: rel, Reason: ReasonUnreadable, Err: err})
			return filepath.SkipDir
		}
		if bare(p) {
			skips = append(skips, Skip{Rel: rel, Reason: ReasonBare})
			return filepath.SkipDir
		}
		if strings.Count(rel, "/")+1 >= depth {
			return filepath.SkipDir
		}
		return nil
	})
	switch {
	case ctx.Err() != nil:
		return nil, nil, fmt.Errorf("scan %s interrupted: %w", root, ctx.Err())
	case errors.Is(err, errRootUnreadable):
		return nil, nil, fmt.Errorf("%w %s: check --dir", ErrRoot, root)
	case err != nil:
		return nil, nil, fmt.Errorf("scan %s: %w", root, err)
	}
	return cands, skips, nil
}

// bare reports whether dir looks like a bare repository: a HEAD file plus objects/ and
// refs/ directories.
func bare(dir string) bool {
	head, err := os.Lstat(filepath.Join(dir, "HEAD"))
	if err != nil || !head.Mode().IsRegular() {
		return false
	}
	for _, sub := range []string{"objects", "refs"} {
		fi, err := os.Lstat(filepath.Join(dir, sub))
		if err != nil || !fi.IsDir() {
			return false
		}
	}
	return true
}
