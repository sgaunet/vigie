package main_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	main "github.com/sgaunet/vigie/cmd/vigie"
	"github.com/sgaunet/vigie/internal/cli"
	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/scan"
	"github.com/sgaunet/vigie/internal/token"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	rateLimit := fmt.Errorf("%w x: %w", forge.ErrRateLimit, forge.ErrNetwork)
	cases := map[error]int{
		cli.ErrUsage:           2,
		config.ErrInvalid:      2,
		config.ErrTokenInFile:  2,
		config.ErrInsecureHost: 2,
		config.ErrNoHost:       2,
		repo.ErrNotRepository:  2,
		repo.ErrRemoteMissing:  2,
		repo.ErrRemoteURL:      2,
		forge.ErrInsecure:      2,
		token.ErrMissing:       3,
		token.ErrCommand:       3,
		forge.ErrUnauthorized:  3,
		forge.ErrForbidden:     3,
		forge.ErrNotFound:      4,
		forge.ErrNetwork:       5,
		rateLimit:              5,
		scan.ErrRoot:           2,
		scan.ErrNothingListed:  2,
		context.Canceled:       1,
		errors.New("boom"):     1,
	}
	if got := main.ExitCode(nil); got != 0 {
		t.Errorf("ExitCode(nil) = %d", got)
	}
	for sentinel, want := range cases {
		err := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", sentinel))
		if got := main.ExitCode(err); got != want {
			t.Errorf("ExitCode(%v) = %d, want %d", err, got, want)
		}
	}
	noGit := fmt.Errorf("%w: git is %w", repo.ErrNotRepository, repo.ErrNoGit)
	if got := main.ExitCode(noGit); got != 2 {
		t.Errorf("ExitCode(no git) = %d, want 2", got)
	}
}

func TestExitCodeAllFailed(t *testing.T) {
	t.Parallel()
	unauthorized := fmt.Errorf("%w by gitlab.test (401)", forge.ErrUnauthorized)
	cases := []struct {
		errs []error
		want int
	}{
		{[]error{unauthorized, token.ErrMissing}, 3},
		{[]error{forge.ErrNotFound, forge.ErrNotFound}, 4},
		{[]error{forge.ErrNetwork}, 5},
		{[]error{config.ErrInsecureHost, forge.ErrInsecure}, 2},
		{[]error{forge.ErrNotFound, forge.ErrUnauthorized}, 1},
		{[]error{errors.New("boom")}, 1},
		{nil, 1},
	}
	for _, c := range cases {
		err := &scan.AllFailedError{Errs: c.errs}
		for _, e := range []error{err, fmt.Errorf("outer: %w", err)} {
			if got := main.ExitCode(e); got != c.want {
				t.Errorf("ExitCode(%v of %v) = %d, want %d", e, c.errs, got, c.want)
			}
		}
	}
}
