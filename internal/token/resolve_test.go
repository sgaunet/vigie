package token_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/token"
)

// TestHelperProcess is not a test: it is the token command run by the tests below.
func TestHelperProcess(_ *testing.T) {
	if os.Getenv("VIGIE_TOKEN_HELPER") != "1" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "print":
		_, _ = os.Stdout.WriteString("  glpat-fromcmd \n")
		os.Exit(0)
	case "empty":
		os.Exit(0)
	case "fail":
		_, _ = os.Stdout.WriteString("glpat-leakedOnStdout")
		_, _ = os.Stderr.WriteString("s3cr3t-on-stderr")
		os.Exit(1)
	case "hang":
		time.Sleep(time.Minute)
	}
	os.Exit(2)
}

func helper(mode string) token.Source {
	return token.Source{Command: []string{os.Args[0], "-test.run=^TestHelperProcess$", "--", mode}}
}

var helperEnv = map[string]string{"VIGIE_TOKEN_HELPER": "1", "GITLAB_TOKEN": "glpat-fromenv"}

func TestResolveFromNamedEnv(t *testing.T) {
	t.Parallel()
	env := map[string]string{"MY_TOKEN": "glpat-mine", "GITLAB_TOKEN": "glpat-fromenv"}
	tok, err := token.Resolve(context.Background(), token.Source{Env: "MY_TOKEN"}, env)
	if err != nil || tok.Value() != "glpat-mine" {
		t.Fatalf("got %q, %v", tok.Value(), err)
	}
	_, err = token.Resolve(context.Background(), token.Source{Env: "MY_TOKEN"}, map[string]string{"GITLAB_TOKEN": "x"})
	if !errors.Is(err, token.ErrMissing) || !strings.HasPrefix(err.Error(), "MY_TOKEN is not set") {
		t.Fatalf("missing named variable: %v", err)
	}
}

func TestResolveFromCommand(t *testing.T) {
	t.Parallel()
	tok, err := token.Resolve(context.Background(), helper("print"), helperEnv)
	if err != nil || tok.Value() != "glpat-fromcmd" {
		t.Fatalf("got %q, %v", tok.Value(), err)
	}
}

func TestResolveCommandFailure(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"fail", "empty"} {
		_, err := token.Resolve(context.Background(), helper(mode), helperEnv)
		if !errors.Is(err, token.ErrCommand) {
			t.Fatalf("%s: got %v, want ErrCommand", mode, err)
		}
		if msg := err.Error(); strings.Contains(msg, "glpat-leaked") || strings.Contains(msg, "s3cr3t") || strings.Contains(msg, "fromenv") {
			t.Fatalf("%s: message leaks command output: %q", mode, msg)
		}
	}
	_, err := token.Resolve(context.Background(), token.Source{Command: []string{"/nonexistent/vigie-token"}}, helperEnv)
	if !errors.Is(err, token.ErrCommand) {
		t.Fatalf("missing command: %v", err)
	}
}

// TestResolveCommandTimeout is not parallel: it shortens the package timeout.
func TestResolveCommandTimeout(t *testing.T) {
	if *token.CommandTimeout != 5*time.Second {
		t.Fatalf("default timeout = %v, want 5s", *token.CommandTimeout)
	}
	old := *token.CommandTimeout
	*token.CommandTimeout = 300 * time.Millisecond
	defer func() { *token.CommandTimeout = old }()
	start := time.Now()
	_, err := token.Resolve(context.Background(), helper("hang"), helperEnv)
	if !errors.Is(err, token.ErrCommand) || time.Since(start) > 3*time.Second {
		t.Fatalf("got %v after %v", err, time.Since(start))
	}
}
