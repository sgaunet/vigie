package token

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// maskedPrefixes are the token prefixes Mask keeps: GitLab personal access tokens, then
// GitHub fine-grained, classic, OAuth, user-to-server, server-to-server and refresh tokens.
var maskedPrefixes = []string{"glpat-", "github_pat_", "ghp_", "gho_", "ghu_", "ghs_", "ghr_"}

// Default token variables per kind of host, in precedence order (GitHub CLI convention).
var (
	GitLabVars     = []string{"GITLAB_TOKEN"}
	GitHubVars     = []string{"GH_TOKEN", "GITHUB_TOKEN"}
	EnterpriseVars = []string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}
)

// Token is a GitLab or GitHub access token. Every way of printing it (fmt verbs, JSON,
// slog) yields the mask; only Value returns the raw string, for the request header.
type Token struct {
	value string
}

// New wraps a raw token value.
func New(value string) Token { return Token{value: value} }

// Value returns the raw token. Call it only to authenticate a request.
func (t Token) Value() string { return t.value }

// String returns the mask.
func (t Token) String() string { return Mask(t.value) }

// GoString returns the mask.
func (t Token) GoString() string { return Mask(t.value) }

// Format writes the mask for every verb; %q quotes it.
func (t Token) Format(f fmt.State, verb rune) {
	s := Mask(t.value)
	if verb == 'q' {
		s = strconv.Quote(s)
	}
	_, _ = f.Write([]byte(s))
}

// MarshalJSON encodes the mask.
func (t Token) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(Mask(t.value))), nil }

// LogValue logs the mask.
func (t Token) LogValue() slog.Value { return slog.StringValue(Mask(t.value)) }

// Mask hides a token, keeping only a known prefix (glpat-, ghp_, github_pat_, …).
func Mask(s string) string {
	for _, p := range maskedPrefixes {
		if strings.HasPrefix(s, p) {
			return p + "****"
		}
	}
	return "****"
}

// Source tells where a host profile takes its token from: Env, else Command, else the first
// set variable of Vars. The zero value means GITLAB_TOKEN.
type Source struct {
	Env     string   // environment variable name
	Command []string // argv of a command printing the token, run without a shell
	Vars    []string // default variables in precedence order; nil means GitLabVars
	Host    string   // the host named in the GitHub Enterprise Server hint
}
