package repo

import "errors"

// Sentinel errors, all mapped to exit code 2. ErrNoGit always comes wrapped together with
// ErrNotRepository, so callers can tell a missing git from a directory outside any clone.
var (
	ErrNotRepository = errors.New("not a git repository")
	ErrNoGit         = errors.New("not installed")
	ErrRemoteMissing = errors.New("not found")
	ErrRemoteURL     = errors.New("cannot parse remote URL")
)
