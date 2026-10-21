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

// Resolve returns the token from src: its environment variable, else its command printing
// the token, else the first non-empty variable of src.Vars in env.
func Resolve(ctx context.Context, src Source, env map[string]string) (Token, error) {
	switch {
	case src.Env != "":
		v := strings.TrimSpace(env[src.Env])
		if v == "" {
			return Token{}, &missingError{msg: src.Env + " is not set: " + src.createHint()}
		}
		return New(v), nil
	case len(src.Command) > 0:
		return fromCommand(ctx, src.Command, env)
	}
	vars := src.vars()
	for _, name := range vars {
		if v := strings.TrimSpace(env[name]); v != "" {
			return New(v), nil
		}
	}
	verb := "is"
	if len(vars) > 1 {
		verb = "are"
	}
	return Token{}, &missingError{msg: fmt.Sprintf("%s %s not set: %s", strings.Join(vars, " and "), verb, src.varsHint())}
}

// Configured reports whether src names a token source at all: a token_env (even unset), a
// token_command (never run here), or a set default variable. A scan skips a GitHub host
// whose source is not configured, without any request.
func (src Source) Configured(env map[string]string) bool {
	if src.Env != "" || len(src.Command) > 0 {
		return true
	}
	for _, name := range src.vars() {
		if strings.TrimSpace(env[name]) != "" {
			return true
		}
	}
	return false
}

// missingError is a token source with nothing in it. It wraps ErrMissing, whose text it
// does not repeat, so a plural can read "are not set".
type missingError struct{ msg string }

func (e *missingError) Error() string { return e.msg }
func (e *missingError) Unwrap() error { return ErrMissing }

// vars returns the default variables, GITLAB_TOKEN when none are set.
func (src Source) vars() []string {
	if len(src.Vars) == 0 {
		return GitLabVars
	}
	return src.Vars
}

// convention names the kind of host the default variables serve, by their first variable.
func (src Source) convention() string { return src.vars()[0] }

// createHint tells how to create a token for this kind of host.
func (src Source) createHint() string {
	switch src.convention() {
	case GitHubVars[0]:
		return "create a fine-grained token with read access to the repository and export it"
	case EnterpriseVars[0]:
		return "create a token with read access to the repository and export it"
	}
	return "create a token with the read_api scope and export it"
}

// varsHint is the hint of a missing default variable: how to create a token, and the
// config file alternatives on GitHub.
func (src Source) varsHint() string {
	switch src.convention() {
	case GitHubVars[0]:
		return src.createHint() + `, or set token_command: ["gh", "auth", "token"] for this host in the config file`
	case EnterpriseVars[0]:
		return src.createHint() + ", or set token_env or token_command for " + src.Host + " in the config file"
	}
	return src.createHint()
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
