package gitlab

import "errors"

// Sentinel errors. Exit codes: ErrInsecure 2, ErrUnauthorized and ErrForbidden 3,
// ErrProjectNotFound 4, ErrNetwork 5.
var (
	ErrInsecure        = errors.New("refusing a plain http API URL")
	ErrUnauthorized    = errors.New("token rejected")
	ErrForbidden       = errors.New("denied")
	ErrProjectNotFound = errors.New("not found")
	ErrNetwork         = errors.New("cannot reach")
)
