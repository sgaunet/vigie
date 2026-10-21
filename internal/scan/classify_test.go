package scan_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/forge"
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
		{forge.ErrUnauthorized, status.FailureAuth},
		{forge.ErrForbidden, status.FailureAuth},
		{forge.ErrNotFound, status.FailureNotFound},
		{forge.ErrNetwork, status.FailureNetwork},
		{fmt.Errorf("%w x: %w", forge.ErrRateLimit, forge.ErrNetwork), status.FailureNetwork},
		{config.ErrInsecureHost, status.FailureError},
		{forge.ErrInsecure, status.FailureError},
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
