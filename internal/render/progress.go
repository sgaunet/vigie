package render

import (
	"io"
	"strconv"
)

// ShowProgress reports whether the scan counter may be drawn: only when stdout and stderr
// are both terminals (no progress when stdout is not a TTY), and neither --quiet nor
// --verbose is set (debug lines would tear the line redrawn in place).
func ShowProgress(stdoutTTY, stderrTTY, quiet, verbose bool) bool {
	return stdoutTTY && stderrTTY && !quiet && !verbose
}

// Progress draws " · done/total" on one line redrawn in place.
type Progress struct {
	w     io.Writer
	color bool
	drawn bool
}

// NewProgress returns a counter writing to w, dim when color is on.
func NewProgress(w io.Writer, color bool) *Progress {
	return &Progress{w: w, color: color}
}

// Update redraws the counter.
func (p *Progress) Update(done, total int) error {
	p.drawn = true
	return write(p.w, eraseLine+paint(" · "+strconv.Itoa(done)+"/"+strconv.Itoa(total), dim, p.color))
}

// Clear erases the counter, if one was drawn.
func (p *Progress) Clear() error {
	if !p.drawn {
		return nil
	}
	p.drawn = false
	return write(p.w, eraseLine)
}
