package token

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

const patPrefix = "glpat-"

// Token is a GitLab access token. Every way of printing it (fmt verbs, JSON, slog) yields
// the mask; only Value returns the raw string, for the PRIVATE-TOKEN header.
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

// Mask hides a token, keeping only the glpat- prefix when present.
func Mask(s string) string {
	if strings.HasPrefix(s, patPrefix) {
		return patPrefix + "****"
	}
	return "****"
}

// Source tells where a host profile takes its token from. The zero value means GITLAB_TOKEN.
type Source struct {
	Env     string   // environment variable name
	Command []string // argv of a command printing the token, run without a shell
}
