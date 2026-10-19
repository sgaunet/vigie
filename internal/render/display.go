package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

// Mode selects how the watch display writes records.
type Mode int

// Display modes.
const (
	ModeTTY  Mode = iota // redraw one line in place
	ModeLine             // one text line per changed snapshot (stdout is not a terminal)
	ModeJSON             // one JSON record per changed snapshot
)

// Display draws successive snapshots in watch mode.
type Display struct {
	out, errOut io.Writer
	mode        Mode
	style       Style
	width       func() int

	drawn  bool
	closed bool
	lastFP string
}

// NewDisplay returns a display writing records to out and the in-flight marker to errOut.
func NewDisplay(out, errOut io.Writer, mode Mode, style Style) *Display {
	return &Display{out: out, errOut: errOut, mode: mode, style: style}
}

// SetWidth makes the TTY display read the terminal width before each draw.
func (d *Display) SetWidth(width func() int) { d.width = width }

// Draw shows s. With inFlight set, it only marks a refresh in progress (TTY mode, on
// errOut, when the style allows it); the next draw erases the marker.
func (d *Display) Draw(s status.Snapshot, inFlight bool) error {
	if inFlight {
		if d.mode == ModeTTY && d.style.InFlight && d.drawn {
			return write(d.errOut, paint(" ·", dim, d.style.Color))
		}
		return nil
	}
	switch d.mode {
	case ModeTTY:
		return d.drawTTY(s)
	case ModeJSON:
		if !d.changed(s) {
			return nil
		}
		return JSON(d.out, s)
	default:
		if !d.changed(s) {
			return nil
		}
		return write(d.out, Text(s, d.style, time.Now())+staleSuffix(s, d.style)+"\n")
	}
}

func (d *Display) drawTTY(s status.Snapshot) error {
	st := d.style
	if d.width != nil {
		st.Width = d.width()
	}
	suffix := staleSuffix(s, st)
	if st.Width > 0 {
		st.Width = max(1, st.Width-cellWidth(stripANSI(suffix)))
	}
	var b strings.Builder
	if !d.drawn {
		b.WriteString(hideCursor)
		d.drawn = true
	}
	b.WriteString(eraseLine)
	b.WriteString(Text(s, st, time.Now()))
	b.WriteString(suffix)
	return write(d.out, b.String())
}

// changed reports whether s differs from the last record written, and remembers it.
func (d *Display) changed(s status.Snapshot) bool {
	fp := s.Fingerprint()
	if d.drawn && fp == d.lastFP {
		return false
	}
	d.drawn, d.lastFP = true, fp
	return true
}

// Close restores the terminal: cursor shown and the line ended. It is safe to call twice.
func (d *Display) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	if d.mode == ModeTTY && d.drawn {
		return write(d.out, showCursor+"\n")
	}
	return nil
}

// staleSuffix is " ⚠ stale 15s" (" ! stale 15s" without emoji), with the age of the values.
func staleSuffix(s status.Snapshot, st Style) string {
	if !s.Stale {
		return ""
	}
	since := s.FetchedAt
	if since.IsZero() {
		since = s.StaleSince
	}
	icon := staleLook.label
	if st.Emoji {
		icon = staleLook.icon
	}
	return " " + paint(icon+" stale "+age(time.Since(since)), staleLook.color, st.Color)
}

func write(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("write display: %w", err)
	}
	return nil
}
