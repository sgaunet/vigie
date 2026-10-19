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
	switch f.Name {
	case status.FieldBranches:
		return branchesSegment(f, st)
	case status.FieldPipelines:
		return pipelinesSegment(f, st)
	case status.FieldLast:
		return lastSegment(f, st, now)
	case status.FieldMRs:
		return countSegment(f, st, mrsLook)
	case status.FieldIssues:
		return countSegment(f, st, issuesLook)
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

func branchesSegment(f status.FieldResult, st Style) string {
	if f.Err != nil {
		return segment(branchesLook, unavailable, dim, st)
	}
	b := f.Branches
	c := plain
	if b.Total == (status.Count{N: 1}) {
		c = dim
	}
	return segment(branchesLook, formatCount(b.Active)+"/"+formatCount(b.Total), c, st)
}

func pipelinesSegment(f status.FieldResult, st Style) string {
	if f.Err != nil {
		return segment(pipelinesLook, unavailable, dim, st)
	}
	p := f.Pipelines
	var c sgr
	switch {
	case positive(p.Running):
		c = blue
	case positive(p.Pending):
		c = yellow
	default:
		c = dim
	}
	return segment(pipelinesLook, formatCount(p.Running)+"r/"+formatCount(p.Pending)+"p", c, st)
}

func lastSegment(f status.FieldResult, st Style, now time.Time) string {
	if f.Err != nil {
		if st.Emoji {
			return paint(lastLook.icon+" "+unavailable, dim, st.Color)
		}
		return paint(lastLook.label+unavailable, dim, st.Color)
	}
	l := f.Last
	lk := pipelineLook(l.Status)
	parts := []string{lk.icon}
	if !st.Emoji {
		parts[0] = "last:" + lk.label
	}
	if l.Ref != "" {
		parts = append(parts, l.Ref)
	}
	if l.Status != status.PipelineNone {
		parts = append(parts, age(now.Sub(l.UpdatedAt)))
	}
	return paint(strings.Join(parts, " "), lk.color, st.Color)
}

func countSegment(f status.FieldResult, st Style, l look) string {
	if f.Err != nil {
		return segment(l, unavailable, dim, st)
	}
	c := dim
	if positive(f.Count) {
		c = bold
	}
	return segment(l, formatCount(f.Count), c, st)
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
