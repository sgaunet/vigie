package token_test

import (
	"context"
	"errors"
	"os"
	"slices"
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

func TestVarConventions(t *testing.T) {
	t.Parallel()
	if !slices.Equal(token.GitLabVars, []string{"GITLAB_TOKEN"}) ||
		!slices.Equal(token.GitHubVars, []string{"GH_TOKEN", "GITHUB_TOKEN"}) ||
		!slices.Equal(token.EnterpriseVars, []string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}) {
		t.Fatalf("conventions = %v %v %v", token.GitLabVars, token.GitHubVars, token.EnterpriseVars)
	}
}

func TestResolveVarsPrecedence(t *testing.T) {
	t.Parallel()
	gh := token.Source{Vars: token.GitHubVars}
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"both set", map[string]string{"GH_TOKEN": "ghp_first", "GITHUB_TOKEN": "ghp_second"}, "ghp_first"},
		{"only GITHUB_TOKEN", map[string]string{"GITHUB_TOKEN": "ghp_second"}, "ghp_second"},
		{"GH_TOKEN of spaces", map[string]string{"GH_TOKEN": "   ", "GITHUB_TOKEN": "ghp_second"}, "ghp_second"},
	}
	for _, c := range cases {
		tok, err := token.Resolve(context.Background(), gh, c.env)
		if err != nil || tok.Value() != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, tok.Value(), err, c.want)
		}
	}
	for _, env := range []map[string]string{{"GITLAB_TOKEN": "glpat-x"}, {"GH_TOKEN": "  "}} {
		if _, err := token.Resolve(context.Background(), gh, env); !errors.Is(err, token.ErrMissing) {
			t.Errorf("env %v: got %v, want ErrMissing", env, err)
		}
	}
}

func TestResolveMissingHints(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src  token.Source
		want string
	}{
		{token.Source{Vars: token.GitHubVars},
			`GH_TOKEN and GITHUB_TOKEN are not set: create a fine-grained token with read access to the repository and export it, or set token_command: ["gh", "auth", "token"] for this host in the config file`},
		{token.Source{Vars: token.EnterpriseVars, Host: "github.example.com"},
			"GH_ENTERPRISE_TOKEN and GITHUB_ENTERPRISE_TOKEN are not set: create a token with read access to the repository and export it, or set token_env or token_command for github.example.com in the config file"},
		{token.Source{}, "GITLAB_TOKEN is not set: create a token with the read_api scope and export it"},
		{token.Source{Vars: token.GitLabVars}, "GITLAB_TOKEN is not set: create a token with the read_api scope and export it"},
		{token.Source{Env: "WORK_GH", Vars: token.GitHubVars}, "WORK_GH is not set: create a fine-grained token with read access to the repository and export it"},
	}
	for _, c := range cases {
		_, err := token.Resolve(context.Background(), c.src, map[string]string{"GITLAB_TOKEN": "", "OTHER": "x"})
		if !errors.Is(err, token.ErrMissing) || err.Error() != c.want {
			t.Errorf("%+v:\n got %v\nwant %s", c.src, err, c.want)
		}
	}
}

func TestResolveOrder(t *testing.T) {
	t.Parallel()
	env := map[string]string{"VIGIE_TOKEN_HELPER": "1", "MINE": "ghp_mine", "GH_TOKEN": "ghp_default"}
	src := helper("print")
	src.Vars = token.GitHubVars
	tok, err := token.Resolve(context.Background(), src, env)
	if err != nil || tok.Value() != "glpat-fromcmd" {
		t.Fatalf("command over vars: %q, %v", tok.Value(), err)
	}
	src.Env = "MINE"
	tok, err = token.Resolve(context.Background(), src, env)
	if err != nil || tok.Value() != "ghp_mine" {
		t.Fatalf("env over command: %q, %v", tok.Value(), err)
	}
}

func TestConfigured(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  token.Source
		env  map[string]string
		want bool
	}{
		{"named variable unset", token.Source{Env: "X"}, map[string]string{}, true},
		{"command, never run", token.Source{Command: []string{"gh", "auth", "token"}}, map[string]string{}, true},
		{"GITHUB_TOKEN set", token.Source{Vars: token.GitHubVars}, map[string]string{"GITHUB_TOKEN": "ghp_x"}, true},
		{"other kinds only", token.Source{Vars: token.GitHubVars}, map[string]string{"GITLAB_TOKEN": "glpat-x", "GH_ENTERPRISE_TOKEN": "ghe"}, false},
		{"github.com token for an Enterprise Server", token.Source{Vars: token.EnterpriseVars}, map[string]string{"GH_TOKEN": "ghp_x"}, false},
		{"spaces only", token.Source{Vars: token.GitHubVars}, map[string]string{"GH_TOKEN": "  "}, false},
	}
	for _, c := range cases {
		if got := c.src.Configured(c.env); got != c.want {
			t.Errorf("%s: Configured = %v, want %v", c.name, got, c.want)
		}
	}
}
