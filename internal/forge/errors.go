package forge

import "errors"

// Sentinel errors both clients wrap. Exit codes: ErrInsecure 2, ErrUnauthorized and
// ErrForbidden 3, ErrNotFound 4, ErrNetwork 5.
var (
	ErrInsecure     = errors.New("refusing a plain http API URL")
	ErrUnauthorized = errors.New("token rejected")
	ErrForbidden    = errors.New("denied")
	ErrNotFound     = errors.New("not found")
	ErrNetwork      = errors.New("cannot reach")

	// ErrRateLimit marks an exhausted request allowance. It is always wrapped together
	// with ErrNetwork, so every caller treats it as throttling, never as an auth failure.
	ErrRateLimit = errors.New("request allowance exhausted")
)
