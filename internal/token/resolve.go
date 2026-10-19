package token

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// commandTimeout bounds a token_command run (research R9).
var commandTimeout = 5 * time.Second

// Resolve returns the token from src (an environment variable, or a command printing the
// token), else from GITLAB_TOKEN in env.
func Resolve(ctx context.Context, src Source, env map[string]string) (Token, error) {
	switch {
	case src.Env != "":
		return fromEnv(env, src.Env)
	case len(src.Command) > 0:
		return fromCommand(ctx, src.Command, env)
	}
	return fromEnv(env, "GITLAB_TOKEN")
}

func fromEnv(env map[string]string, name string) (Token, error) {
	v := strings.TrimSpace(env[name])
	if v == "" {
		return Token{}, fmt.Errorf("%s %w: create a token with the read_api scope and export it", name, ErrMissing)
	}
	return New(v), nil
}

// fromCommand runs argv without a shell and reads the token from its stdout. Neither its
// stdout nor its stderr ever appears in an error message.
func fromCommand(ctx context.Context, argv []string, env map[string]string) (Token, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = make([]string, 0, len(env))
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return Token{}, fmt.Errorf("%w: %s did not finish within %s, check token_command in the config file", ErrCommand, argv[0], commandTimeout)
	case err != nil:
		return Token{}, fmt.Errorf("%w: %s: %s, check token_command in the config file", ErrCommand, argv[0], exitReason(err))
	}
	v := strings.TrimSpace(out.String())
	if v == "" {
		return Token{}, fmt.Errorf("%w: %s printed nothing, check token_command in the config file", ErrCommand, argv[0])
	}
	return New(v), nil
}

// exitReason describes a failed run without any of the command's output.
func exitReason(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.String()
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "command not found"
	}
	return "cannot run it"
}
