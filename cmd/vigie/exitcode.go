package main

import (
	"errors"

	"github.com/sgaunet/vigie/internal/cli"
	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/scan"
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
	{scan.ErrRoot, exitSetup},
	{scan.ErrNoGitLab, exitSetup},
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
	// Every scan row failed: the code all row errors share, else 1.
	var all *scan.AllFailedError
	if errors.As(err, &all) {
		code := exitFailure
		for i, e := range all.Errs {
			switch c := exitCode(e); {
			case i == 0:
				code = c
			case c != code:
				return exitFailure
			}
		}
		return code
	}
	for _, e := range exitCodes {
		if errors.Is(err, e.err) {
			return e.code
		}
	}
	return exitFailure
}
