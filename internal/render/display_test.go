package render_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/render"
)

func TestDisplayTTY(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	d := render.NewDisplay(&out, &errOut, render.ModeTTY, render.Style{Emoji: true, InFlight: true})
	s := fullSnapshot()
	s.FetchedAt = time.Now()
	if err := d.Draw(s, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "\x1b[?25l\r\x1b[2K🦊 group/sub/project") || strings.Contains(got, "\n") {
		t.Fatalf("first draw = %q", got)
	}

	out.Reset()
	_ = d.Draw(s, true)
	if out.Len() != 0 || errOut.String() != " ·" {
		t.Fatalf("in flight: stdout=%q stderr=%q", out.String(), errOut.String())
	}

	s.Stale = true
	s.FetchedAt = time.Now().Add(-15 * time.Second)
	_ = d.Draw(s, false)
	if got := out.String(); !strings.HasPrefix(got, "\r\x1b[2K") || strings.Contains(got, "?25l") || !strings.HasSuffix(got, " ⚠ stale 15s") {
		t.Fatalf("stale redraw = %q", got)
	}

	out.Reset()
	_ = d.Close()
	_ = d.Close()
	if out.String() != "\x1b[?25h\n" {
		t.Fatalf("close = %q", out.String())
	}
}

func TestDisplayTTYNoEmojiNoInFlight(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	d := render.NewDisplay(&out, &errOut, render.ModeTTY, render.Style{Color: true})
	s := fullSnapshot()
	s.Stale = true
	s.FetchedAt = time.Now().Add(-15 * time.Second)
	_ = d.Draw(s, false)
	_ = d.Draw(s, true)
	if errOut.Len() != 0 {
		t.Fatalf("marker written while stderr is not a terminal: %q", errOut.String())
	}
	if !strings.Contains(out.String(), " \x1b[33m! stale 15s\x1b[0m") {
		t.Fatalf("stale = %q", out.String())
	}
}

func TestDisplayTTYWidth(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	d := render.NewDisplay(&out, &out, render.ModeTTY, render.Style{})
	width := 0
	d.SetWidth(func() int { width++; return 0 })
	_ = d.Draw(fullSnapshot(), false)
	_ = d.Draw(fullSnapshot(), false)
	if width != 2 {
		t.Fatalf("width read %d times, want once per draw", width)
	}
}

func TestDisplayLineMode(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	d := render.NewDisplay(&out, &errOut, render.ModeLine, render.Style{})
	s := fullSnapshot()
	_ = d.Draw(s, false)
	_ = d.Draw(s, true)
	s.FetchedAt = s.FetchedAt.Add(time.Minute)
	_ = d.Draw(s, false) // same content, later
	if got := out.String(); strings.Count(got, "\n") != 1 || strings.ContainsRune(got, '\x1b') {
		t.Fatalf("duplicate or decorated lines: %q", got)
	}
	s.Fields[3].Count.N = 6
	_ = d.Draw(s, false)
	s.Stale = true
	_ = d.Draw(s, false)
	_ = d.Draw(s, false)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], "mr 6") || !strings.Contains(lines[2], " ! stale ") {
		t.Fatalf("lines = %q", lines)
	}
	_ = d.Close()
	if errOut.Len() != 0 || !strings.HasSuffix(out.String(), "\n") || strings.Contains(out.String(), "\n\n") {
		t.Fatalf("close: stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestDisplayJSONMode(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	d := render.NewDisplay(&out, &out, render.ModeJSON, render.Style{})
	s := fullSnapshot()
	_ = d.Draw(s, false)
	_ = d.Draw(s, false)
	s.Stale = true
	s.StaleSince = now
	_ = d.Draw(s, false)
	s.StaleSince = now.Add(time.Minute)
	_ = d.Draw(s, false)
	s.Stale = false
	_ = d.Draw(s, false)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], `"stale":false`) || !strings.Contains(lines[1], `"stale":true`) || !strings.Contains(lines[2], `"stale":false`) {
		t.Fatalf("records = %q", lines)
	}
}
