package cli

import "errors"

// ErrUsage marks a command line that cannot be parsed (exit code 2).
var ErrUsage = errors.New("invalid usage")
