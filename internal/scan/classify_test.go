package scan_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/scan"
	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/token"
)

func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want status.Failure
	}{
		{token.ErrMissing, status.FailureAuth},
		{token.ErrCommand, status.FailureAuth},
		{gitlab.ErrUnauthorized, status.FailureAuth},
		{gitlab.ErrForbidden, status.FailureAuth},
		{gitlab.ErrProjectNotFound, status.FailureNotFound},
		{gitlab.ErrNetwork, status.FailureNetwork},
		{config.ErrInsecureHost, status.FailureError},
		{gitlab.ErrInsecure, status.FailureError},
		{errors.New("boom"), status.FailureError},
	}
	for _, c := range cases {
		wrapped := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", c.err))
		for _, err := range []error{c.err, wrapped} {
			if got := scan.Classify(err); got != c.want {
				t.Errorf("Classify(%v) = %q, want %q", err, got, c.want)
			}
		}
	}
	if got := scan.Classify(nil); got != "" {
		t.Errorf("Classify(nil) = %q", got)
	}
}
