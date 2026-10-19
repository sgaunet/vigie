package main_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	main "github.com/sgaunet/vigie/cmd/vigie"
	"github.com/sgaunet/vigie/internal/cli"
	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/token"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	cases := map[error]int{
		cli.ErrUsage:              2,
		config.ErrInvalid:         2,
		config.ErrTokenInFile:     2,
		config.ErrInsecureHost:    2,
		config.ErrNoHost:          2,
		repo.ErrNotRepository:     2,
		repo.ErrRemoteMissing:     2,
		repo.ErrRemoteURL:         2,
		gitlab.ErrInsecure:        2,
		token.ErrMissing:          3,
		token.ErrCommand:          3,
		gitlab.ErrUnauthorized:    3,
		gitlab.ErrForbidden:       3,
		gitlab.ErrProjectNotFound: 4,
		gitlab.ErrNetwork:         5,
		context.Canceled:          1,
		errors.New("boom"):        1,
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
}
