package token

import "errors"

// Sentinel errors, both mapped to exit code 3.
var (
	ErrMissing = errors.New("is not set")
	ErrCommand = errors.New("token command failed")
)
