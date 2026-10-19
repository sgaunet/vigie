package token_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/sgaunet/vigie/internal/token"
)

const raw = "glpat-s3cr3tV4lue"

func TestTokenNeverPrintsRawValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value, mask string
	}{
		{raw, "glpat-****"},
		{"other-s3cr3tV4lue", "****"},
	}
	for _, c := range cases {
		tok := token.New(c.value)
		holder := struct{ Tok token.Token }{tok}
		outputs := []string{
			fmt.Sprintf("%s", tok),
			fmt.Sprintf("%v", tok),
			fmt.Sprintf("%+v", tok),
			fmt.Sprintf("%#v", tok),
			fmt.Sprintf("%q", tok),
			fmt.Sprintf("%v %+v %#v", holder, holder, holder),
			tok.String(),
			tok.GoString(),
		}
		b, err := json.Marshal(holder)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		outputs = append(outputs, string(b))

		var text, js bytes.Buffer
		slog.New(slog.NewTextHandler(&text, nil)).Info("x", "token", tok, "nested", holder)
		slog.New(slog.NewJSONHandler(&js, nil)).Info("x", "token", tok)
		outputs = append(outputs, text.String(), js.String())

		for _, out := range outputs {
			if strings.Contains(out, "s3cr3t") {
				t.Errorf("raw value leaked in %q", out)
			}
			if !strings.Contains(out, c.mask) {
				t.Errorf("mask %q missing from %q", c.mask, out)
			}
		}
		if tok.Value() != c.value {
			t.Errorf("Value() = %q, want the raw value", tok.Value())
		}
	}
}

func TestMask(t *testing.T) {
	t.Parallel()
	if got := token.Mask("glpat-abc"); got != "glpat-****" {
		t.Errorf("Mask(glpat-abc) = %q", got)
	}
	if got := token.Mask("abc"); got != "****" {
		t.Errorf("Mask(abc) = %q", got)
	}
}

func TestResolveFromGitLabToken(t *testing.T) {
	t.Parallel()
	tok, err := token.Resolve(context.Background(), token.Source{}, map[string]string{"GITLAB_TOKEN": raw})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tok.Value() != raw {
		t.Fatalf("Value() = %q", tok.Value())
	}
}

func TestResolveMissing(t *testing.T) {
	t.Parallel()
	for _, env := range []map[string]string{{}, {"GITLAB_TOKEN": ""}} {
		_, err := token.Resolve(context.Background(), token.Source{}, env)
		if !errors.Is(err, token.ErrMissing) {
			t.Fatalf("env %v: got %v, want ErrMissing", env, err)
		}
		want := "GITLAB_TOKEN is not set: create a token with the read_api scope and export it"
		if err.Error() != want {
			t.Fatalf("message = %q, want %q", err.Error(), want)
		}
	}
}
