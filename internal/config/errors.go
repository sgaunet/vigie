package config

import (
	"errors"
	"fmt"
)

// Sentinel errors, all mapped to exit code 2. Messages are written to read well when
// wrapped in the middle of a sentence ("config file x contains a token: ...").
var (
	ErrInvalid      = errors.New("invalid")
	ErrTokenInFile  = errors.New("contains a token")
	ErrInsecureHost = errors.New("refusing to send the token over")
	ErrNoHost       = errors.New("no GitLab host")
)

// invalidError is an ErrInvalid whose message reads as a sentence of its own, without
// ErrInvalid's text.
type invalidError struct{ msg string }

func (e *invalidError) Error() string { return e.msg }
func (e *invalidError) Unwrap() error { return ErrInvalid }

func invalidf(format string, a ...any) error {
	return &invalidError{msg: fmt.Sprintf(format, a...)}
}
