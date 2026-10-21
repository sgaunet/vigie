package render_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sgaunet/vigie/internal/render"
)

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestProgress(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := render.NewProgress(&buf, false)
	if err := p.Clear(); err != nil || buf.Len() != 0 {
		t.Fatalf("clear before any draw wrote %q (%v)", buf.String(), err)
	}
	if err := p.Update(12, 40); err != nil || buf.String() != "\r\x1b[2K · 12/40" {
		t.Fatalf("update = %q, %v", buf.String(), err)
	}
	buf.Reset()
	if err := p.Clear(); err != nil || buf.String() != "\r\x1b[2K" {
		t.Fatalf("clear = %q, %v", buf.String(), err)
	}
	buf.Reset()
	if err := p.Clear(); err != nil || buf.Len() != 0 {
		t.Fatalf("second clear wrote %q", buf.String())
	}

	buf.Reset()
	if err := render.NewProgress(&buf, true).Update(1, 2); err != nil || buf.String() != "\r\x1b[2K\x1b[2m · 1/2\x1b[0m" {
		t.Fatalf("color update = %q, %v", buf.String(), err)
	}

	boom := errors.New("boom")
	if err := render.NewProgress(failingWriter{boom}, false).Update(1, 2); !errors.Is(err, boom) {
		t.Fatalf("failing writer: got %v", err)
	}
}

func TestShowProgress(t *testing.T) {
	t.Parallel()
	for i := range 16 {
		stdout, stderr, quiet, verbose := i&1 != 0, i&2 != 0, i&4 != 0, i&8 != 0
		want := stdout && stderr && !quiet && !verbose
		if got := render.ShowProgress(stdout, stderr, quiet, verbose); got != want {
			t.Errorf("ShowProgress(%v, %v, %v, %v) = %v", stdout, stderr, quiet, verbose, got)
		}
	}
}
