package render

import (
	"strings"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

const (
	columnGap    = "  "
	headDir      = "DIR"
	headPlatform = "PLATFORM"
	headProject  = "PROJECT"
)

// Column indexes of the fixed columns; the field columns follow.
const (
	colDir = iota
	colPlatform
	colProject
	colFields
)

// cell is the text of one table cell, without color, and its color.
type cell struct {
	text  string
	color sgr
}

// tableLine is the header or one row. A failed row has a single marker in place of its
// field cells.
type tableLine struct {
	dir, platform, project string
	archived               bool
	fields                 []cell
	marker                 *cell
}

// projectText is the PROJECT cell as displayed, archived tag included.
func (l tableLine) projectText() string {
	if l.archived {
		return l.project + " " + archivedLook.label
	}
	return l.project
}

// Table renders scan rows under a header, each line ending with "\n": DIR, PLATFORM,
// PROJECT, then one column per field in fields order. Columns are two spaces apart and
// padded to their widest cell; the last cell of a line is never padded. With a Width,
// PROJECT then DIR are cut from the left until the table fits; the other columns never are.
func Table(rows []status.Row, fields []status.FieldName, st Style, now time.Time) string {
	head := tableLine{dir: headDir, platform: headPlatform, project: headProject}
	for _, f := range fields {
		head.fields = append(head.fields, cell{text: strings.ToUpper(string(f)), color: bold})
	}
	lines := []tableLine{head}
	for _, r := range rows {
		lines = append(lines, tableRow(r, st, now))
	}
	if st.Width > 0 {
		fit(lines, st.Width)
	}
	widths := columnWidths(lines)
	var b strings.Builder
	for i, l := range lines {
		dirColor, projectColor := plain, projectLook.color
		if i == 0 {
			dirColor = bold
		}
		project := paint(l.project, projectColor, st.Color)
		if l.archived {
			project += " " + paint(archivedLook.label, archivedLook.color, st.Color)
		}
		parts := []string{paint(l.dir, dirColor, st.Color), paint(l.platform, dirColor, st.Color), project}
		plainParts := []string{l.dir, l.platform, l.projectText()}
		rest := l.fields
		if l.marker != nil {
			rest = []cell{*l.marker}
		}
		for _, c := range rest {
			parts = append(parts, paint(c.text, c.color, st.Color))
			plainParts = append(plainParts, c.text)
		}
		for j, p := range parts {
			if j > 0 {
				b.WriteString(columnGap)
			}
			b.WriteString(p)
			if j < len(parts)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-cellWidth(plainParts[j])))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func tableRow(r status.Row, st Style, now time.Time) tableLine {
	p := r.Snapshot.Project
	l := tableLine{dir: r.Dir, platform: string(p.Platform), project: p.Path, archived: p.Archived}
	if !r.OK() {
		icon := staleLook.label
		if st.Emoji {
			icon = staleLook.icon
		}
		l.marker = &cell{text: icon + " " + string(r.Failure), color: staleLook.color}
		return l
	}
	for _, f := range r.Snapshot.Fields {
		text, c := fieldValue(f, st, now)
		l.fields = append(l.fields, cell{text: text, color: c})
	}
	return l
}

// columnWidths returns the widest cell of each column, in terminal cells. Markers span the
// field columns and take no part in their widths.
func columnWidths(lines []tableLine) []int {
	widths := make([]int, colFields+len(lines[0].fields))
	for _, l := range lines {
		widths[colDir] = max(widths[colDir], cellWidth(l.dir))
		widths[colPlatform] = max(widths[colPlatform], cellWidth(l.platform))
		widths[colProject] = max(widths[colProject], cellWidth(l.projectText()))
		for i, c := range l.fields {
			widths[colFields+i] = max(widths[colFields+i], cellWidth(c.text))
		}
	}
	return widths
}

// tableWidth is the width of the widest line.
func tableWidth(lines []tableLine) int {
	widths := columnWidths(lines)
	w := 0
	for _, l := range lines {
		last := cellWidth(l.projectText())
		n := colFields
		switch {
		case l.marker != nil:
			last, n = cellWidth(l.marker.text), colFields+1
		case len(l.fields) > 0:
			last, n = cellWidth(l.fields[len(l.fields)-1].text), colFields+len(l.fields)
		}
		lw := last + len(columnGap)*(n-1)
		for i := range n - 1 {
			lw += widths[i]
		}
		w = max(w, lw)
	}
	return w
}

// fit cuts the PROJECT column, then the DIR column, from the left until the table fits
// in width. A column never gets narrower than its header; PLATFORM is never cut.
func fit(lines []tableLine, width int) {
	if over := tableWidth(lines) - width; over > 0 {
		limit := max(cellWidth(headProject), columnWidths(lines)[colProject]-over)
		for i := range lines[1:] {
			l := &lines[1+i]
			l.project = truncateLeft(l.project, limit-(cellWidth(l.projectText())-cellWidth(l.project)))
		}
	}
	if over := tableWidth(lines) - width; over > 0 {
		limit := max(cellWidth(headDir), columnWidths(lines)[colDir]-over)
		for i := range lines[1:] {
			lines[1+i].dir = truncateLeft(lines[1+i].dir, limit)
		}
	}
}
