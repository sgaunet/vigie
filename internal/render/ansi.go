package render

// sgr is an ANSI Select Graphic Rendition parameter list.
type sgr string

// Colors and attributes used on the line.
const (
	plain   sgr = ""
	bold    sgr = "1"
	dim     sgr = "2"
	red     sgr = "1;31" // failed pipelines are red and bold
	green   sgr = "32"
	yellow  sgr = "33"
	blue    sgr = "34"
	magenta sgr = "35"
	cyan    sgr = "36"
	gray    sgr = "90"
)

// Terminal control sequences of the watch display.
const (
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
	eraseLine  = "\r\x1b[2K"
)

// paint wraps s in the color c when color output is on.
func paint(s string, c sgr, on bool) string {
	if !on || c == plain {
		return s
	}
	return "\x1b[" + string(c) + "m" + s + "\x1b[0m"
}
