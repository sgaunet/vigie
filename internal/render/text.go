package render

import (
	"strconv"
	"strings"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

// Style controls the text rendering.
type Style struct {
	Color    bool // ANSI colors
	Emoji    bool // icons; ASCII labels otherwise
	Width    int  // terminal columns; 0 means no limit
	InFlight bool // watch mode may show the in-flight marker on stderr
}

const (
	separator   = " "
	unavailable = "–"
)

// Text renders the status line, without a trailing newline. With a Width, the project
// path is cut from the left until the line fits; other segments are never cut.
func Text(s status.Snapshot, st Style, now time.Time) string {
	fields := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		fields = append(fields, fieldSegment(f, st, now))
	}
	rest := ""
	if len(fields) > 0 {
		rest = separator + strings.Join(fields, separator)
	}
	return projectSegment(s.Project, st, st.Width-cellWidth(rest)) + rest
}

// projectSegment renders the project within budget cells (no limit when the style has no
// width).
func projectSegment(p status.Project, st Style, budget int) string {
	prefix, suffix := "", ""
	if st.Emoji {
		prefix = projectLook.icon + " "
	}
	if p.Archived {
		suffix = " " + archivedLook.label
	}
	path := p.Path
	if st.Width > 0 {
		path = truncateLeft(path, budget-cellWidth(prefix+suffix))
	}
	out := paint(prefix+path, projectLook.color, st.Color)
	if p.Archived {
		out += " " + paint(archivedLook.label, archivedLook.color, st.Color)
	}
	return out
}

func fieldSegment(f status.FieldResult, st Style, now time.Time) string {
	text, c := fieldValue(f, st, now)
	switch f.Name {
	case status.FieldBranches:
		return segment(branchesLook, text, c, st)
	case status.FieldPipelines:
		return segment(pipelinesLook, text, c, st)
	case status.FieldLast:
		return lastSegment(f, text, c, st)
	case status.FieldMRs:
		return segment(mrsLook, text, c, st)
	case status.FieldIssues:
		return segment(issuesLook, text, c, st)
	}
	return ""
}

// segment joins an icon (or label) and a text, then paints it.
func segment(l look, text string, c sgr, st Style) string {
	prefix := l.label
	if st.Emoji {
		prefix = l.icon
	}
	return paint(prefix+" "+text, c, st.Color)
}

// lastSegment prefixes the last-pipeline value: the value starts with the status icon, so
// only an unavailable value gets the field icon; ASCII always gets the "last:" label.
func lastSegment(f status.FieldResult, text string, c sgr, st Style) string {
	switch {
	case !st.Emoji:
		text = lastLook.label + text
	case f.Err != nil:
		text = lastLook.icon + " " + text
	}
	return paint(text, c, st.Color)
}

// fieldValue returns the value part of a field segment, without the field icon or label,
// and its color. The last pipeline keeps its status icon (or label): that is a value.
func fieldValue(f status.FieldResult, st Style, now time.Time) (string, sgr) {
	if f.Err != nil {
		return unavailable, dim
	}
	switch f.Name {
	case status.FieldBranches:
		return branchesValue(f.Branches)
	case status.FieldPipelines:
		return pipelinesValue(f.Pipelines)
	case status.FieldLast:
		return lastValue(f.Last, st, now)
	case status.FieldMRs, status.FieldIssues:
		return countValue(f.Count)
	}
	return "", plain
}

func branchesValue(b status.Branches) (string, sgr) {
	c := plain
	if b.Total == (status.Count{N: 1}) {
		c = dim
	}
	return formatCount(b.Active) + "/" + formatCount(b.Total), c
}

func pipelinesValue(p status.Pipelines) (string, sgr) {
	var c sgr
	switch {
	case positive(p.Running):
		c = blue
	case positive(p.Pending):
		c = yellow
	default:
		c = dim
	}
	return formatCount(p.Running) + "r/" + formatCount(p.Pending) + "p", c
}

func lastValue(l status.LastPipeline, st Style, now time.Time) (string, sgr) {
	lk := pipelineLook(l.Status)
	parts := []string{lk.label}
	if st.Emoji {
		parts[0] = lk.icon
	}
	if l.Ref != "" {
		parts = append(parts, l.Ref)
	}
	if l.Status != status.PipelineNone {
		parts = append(parts, age(now.Sub(l.UpdatedAt)))
	}
	return strings.Join(parts, " "), lk.color
}

func countValue(n status.Count) (string, sgr) {
	if positive(n) {
		return formatCount(n), bold
	}
	return formatCount(n), dim
}

func positive(c status.Count) bool { return c.Capped || c.N > 0 }

func formatCount(c status.Count) string {
	if c.Capped {
		return "10k+"
	}
	return strconv.Itoa(c.N)
}

// age formats a duration as 12s, 3m, 2h or 4d.
func age(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}
