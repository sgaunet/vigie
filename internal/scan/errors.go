package scan

import (
	"errors"
	"strconv"
)

// Sentinel errors, both mapped to exit code 2.
var (
	ErrRoot          = errors.New("cannot scan")
	ErrNothingListed = errors.New("no GitLab or GitHub repository found")
)

// AllFailedError is the error of a scan where every listed row failed. It deliberately
// has no Unwrap: no single row error stands for the run, so the exit code is the one the
// row errors share, mapped in cmd/vigie.
type AllFailedError struct {
	Errs    []error // one per row, in row order
	Skipped Summary
}

func (e *AllFailedError) Error() string {
	msg := "1 listed repository failed"
	if n := len(e.Errs); n != 1 {
		msg = "all " + strconv.Itoa(n) + " listed repositories failed"
	}
	if skipped := e.Skipped.Skips(); skipped != "" {
		msg += "; " + skipped + "; run with -v to list them"
	}
	return msg
}
