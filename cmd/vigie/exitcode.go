package main

import (
	"errors"

	"github.com/sgaunet/vigie/internal/cli"
	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/token"
)

// Exit codes, listed in --help.
const (
	exitOK       = 0
	exitFailure  = 1
	exitSetup    = 2
	exitAuth     = 3
	exitNotFound = 4
	exitNetwork  = 5
)

var exitCodes = []struct {
	err  error
	code int
}{
	{cli.ErrUsage, exitSetup},
	{config.ErrInvalid, exitSetup},
	{config.ErrTokenInFile, exitSetup},
	{config.ErrInsecureHost, exitSetup},
	{config.ErrNoHost, exitSetup},
	{repo.ErrNotRepository, exitSetup},
	{repo.ErrRemoteMissing, exitSetup},
	{repo.ErrRemoteURL, exitSetup},
	{gitlab.ErrInsecure, exitSetup},
	{token.ErrMissing, exitAuth},
	{token.ErrCommand, exitAuth},
	{gitlab.ErrUnauthorized, exitAuth},
	{gitlab.ErrForbidden, exitAuth},
	{gitlab.ErrProjectNotFound, exitNotFound},
	{gitlab.ErrNetwork, exitNetwork},
}

// exitCode maps an error returned by run to the process exit code.
func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	for _, e := range exitCodes {
		if errors.Is(err, e.err) {
			return e.code
		}
	}
	return exitFailure
}
