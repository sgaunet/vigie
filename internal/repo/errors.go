package repo

import "errors"

// Sentinel errors, all mapped to exit code 2.
var (
	ErrNotRepository = errors.New("not a git repository")
	ErrRemoteMissing = errors.New("not found")
	ErrRemoteURL     = errors.New("cannot parse remote URL")
)
