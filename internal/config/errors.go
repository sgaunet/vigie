package config

import "errors"

// Sentinel errors, all mapped to exit code 2. Messages are written to read well when
// wrapped in the middle of a sentence ("config file x contains a token: ...").
var (
	ErrInvalid      = errors.New("invalid")
	ErrTokenInFile  = errors.New("contains a token")
	ErrInsecureHost = errors.New("refusing to send the token over")
	ErrNoHost       = errors.New("no GitLab host")
)
