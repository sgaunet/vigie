package render_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/render"
	"github.com/sgaunet/vigie/internal/status"
)

// failedRow is a scan row whose project could not be fetched.
func failedRow(dir, path string, f status.Failure) status.Row {
	var fields []status.FieldResult
	for _, n := range status.DefaultFields() {
		fields = append(fields, status.FieldResult{Name: n})
	}
	return status.Row{
		Dir:      dir,
		Snapshot: status.Snapshot{Project: status.Project{Path: path, WebURL: "https://gitlab.com/" + path}, Branch: "main", Fields: fields, FetchedAt: now},
		Failure:  f,
		Err:      fmt.Errorf("project %s %s", path, f),
	}
}

// tableRows is the four-row example of contracts/cli.md "Text output".
func tableRows() []status.Row {
	api := fullSnapshot()
	api.Project = status.Project{Path: "backend/api", WebURL: "https://gitlab.com/backend/api"}

	cli := fullSnapshot()
	cli.Project = status.Project{Path: "tools/cli", WebURL: "https://gitlab.com/tools/cli"}
	cli.Branch = "fix-login"
	cli.Fields = []status.FieldResult{
		{Name: status.FieldBranches, Branches: status.Branches{Active: count(3), Total: count(3)}},
		{Name: status.FieldPipelines, Pipelines: status.Pipelines{Running: count(0), Pending: count(0)}},
		{Name: status.FieldLast, Last: status.LastPipeline{ID: 7, Status: status.PipelineFailed, Ref: "fix-login", UpdatedAt: now.Add(-time.Hour)}},
		{Name: status.FieldMRs, Count: count(0)},
		{Name: status.FieldIssues, Count: count(2)},
	}

	web := fullSnapshot()
	web.Project = status.Project{Path: "frontend/web", WebURL: "https://gitlab.com/frontend/web", Archived: true}
	web.Fields = []status.FieldResult{
		{Name: status.FieldBranches, Branches: status.Branches{Active: count(1), Total: count(1)}},
		{Name: status.FieldPipelines, Pipelines: status.Pipelines{Running: count(0), Pending: count(0)}},
		{Name: status.FieldLast, Last: status.LastPipeline{Status: status.PipelineNone, Ref: "main"}},
		{Name: status.FieldMRs, Count: count(0)},
		{Name: status.FieldIssues, Err: status.ErrUnavailable},
	}

	return []status.Row{
		{Dir: "api", Snapshot: api},
		failedRow("legacy", "group/legacy", status.FailureNotFound),
		{Dir: "tools/cli", Snapshot: cli},
		{Dir: "web", Snapshot: web},
	}
}

// checkLines asserts the layout rules every table follows.
func checkLines(t *testing.T, name, table string) {
	t.Helper()
	if !strings.HasSuffix(table, "\n") {
		t.Errorf("%s: last line does not end with a newline: %q", name, table)
	}
	for _, l := range strings.Split(strings.TrimSuffix(table, "\n"), "\n") {
		if plain := strings.ReplaceAll(l, "\x1b[0m", ""); strings.HasSuffix(plain, " ") {
			t.Errorf("%s: trailing spaces in %q", name, l)
		}
	}
}

func table(t *testing.T, name string, rows []status.Row, fields []status.FieldName, st render.Style) string {
	t.Helper()
	got := render.Table(rows, fields, st, now)
	checkLines(t, name, got)
	return got
}

func TestTableStyles(t *testing.T) {
	t.Parallel()
	golden(t, "table_emoji_color", table(t, "emoji color", tableRows(), status.DefaultFields(), emojiColor))
	golden(t, "table_emoji_nocolor", table(t, "emoji", tableRows(), status.DefaultFields(), render.Style{Emoji: true}))
	golden(t, "table_ascii", table(t, "ascii", tableRows(), status.DefaultFields(), asciiPlain))

	if got := table(t, "ascii", tableRows(), status.DefaultFields(), asciiPlain); strings.ContainsRune(got, '\x1b') {
		t.Errorf("escape code without color: %q", got)
	}
	head, _, _ := strings.Cut(table(t, "emoji color", tableRows(), status.DefaultFields(), emojiColor), "\n")
	if !strings.HasPrefix(head, "\x1b[1mDIR\x1b[0m") || !strings.Contains(head, "\x1b[1mISSUES\x1b[0m") {
		t.Errorf("header is not bold: %q", head)
	}
}

func TestTableFieldSubset(t *testing.T) {
	t.Parallel()
	fields := []status.FieldName{status.FieldLast, status.FieldMRs}
	rows := tableRows()
	for i := range rows {
		s := &rows[i].Snapshot
		s.Fields = []status.FieldResult{s.Fields[2], s.Fields[3]}
	}
	got := table(t, "subset", rows, fields, asciiPlain)
	golden(t, "table_subset", got)
	if head, _, _ := strings.Cut(got, "\n"); head != "DIR        PROJECT                LAST               MRS" {
		t.Errorf("header = %q", head)
	}
}

func TestTableFailures(t *testing.T) {
	t.Parallel()
	var rows []status.Row
	for _, f := range []status.Failure{status.FailureAuth, status.FailureNotFound, status.FailureNetwork, status.FailureError} {
		rows = append(rows, failedRow(strings.ReplaceAll(string(f), " ", "-"), "group/"+strings.ReplaceAll(string(f), " ", ""), f))
	}
	rows = append(rows, tableRows()[0])
	got := table(t, "failures emoji color", rows, status.DefaultFields(), emojiColor) +
		table(t, "failures ascii", rows, status.DefaultFields(), asciiPlain)
	golden(t, "table_failures", got)
	for _, want := range []string{"\x1b[33m⚠ auth\x1b[0m\n", "\x1b[33m⚠ not found\x1b[0m\n", "  ! network\n", "  ! error\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("marker %q missing", want)
		}
	}
}

func TestTableCapped(t *testing.T) {
	t.Parallel()
	s := fullSnapshot()
	c := status.Count{Capped: true}
	s.Fields[0].Branches = status.Branches{Active: c, Total: c}
	s.Fields[1].Pipelines = status.Pipelines{Running: c, Pending: count(0)}
	s.Fields[3].Count = c
	s.Fields[4].Count = c
	got := table(t, "capped", []status.Row{{Dir: "big", Snapshot: s}}, status.DefaultFields(), asciiPlain)
	golden(t, "table_capped", got)
	if !strings.Contains(got, "10k+/10k+  10k+r/0p") || !strings.HasSuffix(got, "10k+  10k+\n") {
		t.Errorf("capped = %q", got)
	}
}

func longRows() []status.Row {
	a := fullSnapshot()
	a.Project.Path = "acme/platform/backend/api"
	b := fullSnapshot()
	b.Project.Path = "tools/cli"
	return []status.Row{{Dir: "src/acme/platform/api", Snapshot: a}, {Dir: "src/tools/cli", Snapshot: b}}
}

// fieldCells returns everything after the PROJECT column of each line, cells only.
func fieldCells(table string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSuffix(table, "\n"), "\n") {
		var cells []string
		for _, c := range strings.Split(l, "  ") {
			if c = strings.TrimSpace(c); c != "" {
				cells = append(cells, c)
			}
		}
		out = append(out, strings.Join(cells[2:], "|"))
	}
	return out
}

func TestTableWidth(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	for _, st := range []render.Style{{}, {Emoji: true}} {
		full := table(t, "full", longRows(), status.DefaultFields(), st)
		fullCells := fieldCells(full)
		for _, width := range []int{0, 100, 94, 80, 70, 50} {
			st.Width = width
			got := table(t, fmt.Sprint("width ", width), longRows(), status.DefaultFields(), st)
			if !st.Emoji {
				fmt.Fprintf(&out, "width %d\n%s", width, got)
			}
			if fc := fieldCells(got); strings.Join(fc, "\n") != strings.Join(fullCells, "\n") {
				t.Errorf("width %d: a field column was changed:\n%s", width, got)
			}
			fits := width >= 69 || width == 0
			for _, l := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
				if w := render.CellWidth(l); fits && width > 0 && w > width {
					t.Errorf("width %d: line is %d cells: %q", width, w, l)
				}
			}
			if width == 0 || width >= render.CellWidth(strings.SplitN(full, "\n", 2)[0]) {
				if got != full {
					t.Errorf("width %d: cut although it fits:\n%s", width, got)
				}
			}
		}
	}
	golden(t, "table_width", out.String())
	cut := render.Table(longRows(), status.DefaultFields(), render.Style{Width: 80}, now)
	if !strings.Contains(cut, "src/acme/platform/api  …/api ") || !strings.Contains(cut, "src/tools/cli          tools/cli ") {
		t.Errorf("width 80 must cut PROJECT only:\n%s", cut)
	}
}

func TestTableArchivedTagSurvivesCut(t *testing.T) {
	t.Parallel()
	rows := longRows()
	rows[0].Snapshot.Project.Archived = true
	got := render.Table(rows, status.DefaultFields(), render.Style{Width: 85}, now)
	if !strings.Contains(got, "…/api archived  ") {
		t.Errorf("archived tag lost:\n%s", got)
	}
}
