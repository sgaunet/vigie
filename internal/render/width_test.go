package render_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sgaunet/vigie/internal/render"
	"github.com/sgaunet/vigie/internal/status"
)

func TestCellWidth(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"abc": 3, "…": 1, "–": 1, "·": 1, "∅": 1, "⚠": 1,
		"🗼": 2, "🦊": 2, "🐙": 2, "🌿": 2, "⚙️": 2, "✅": 2, "❌": 2, "🔄": 2, "⏳": 2, "✋": 2, "🕒": 2,
		"⏭️": 2, "❓": 2, "❔": 2, "🔀": 2, "🐛": 2,
		"🗼 group/app": 12, "\x1b[1mbold\x1b[0m": 4,
	}
	for s, want := range cases {
		if got := render.CellWidth(s); got != want {
			t.Errorf("CellWidth(%q) = %d, want %d", s, got, want)
		}
	}
}

func longSnapshot() status.Snapshot {
	s := fullSnapshot()
	s.Project.Path = "acme/platform/infrastructure/team/project"
	return s
}

// rest returns the line after the project segment (the first space after the icon).
func rest(line string) string {
	start := 0
	if i := strings.Index(line, "🦊 "); i >= 0 {
		start = i + len("🦊 ")
	}
	return line[start+strings.Index(line[start:], " "):]
}

func TestTruncation(t *testing.T) {
	t.Parallel()
	for _, st := range []render.Style{{}, {Emoji: true}, {Emoji: true, Color: true}} {
		full := render.Text(longSnapshot(), st, now)
		var out strings.Builder
		for _, width := range []int{0, 120, 100, 90, 80, 70, 60, 40} {
			st.Width = width
			got := render.Text(longSnapshot(), st, now)
			fmt.Fprintf(&out, "%3d %s\n", width, got)
			if rest(got) != rest(full) {
				t.Errorf("width %d: a segment other than the project was changed: %q", width, got)
			}
			w := render.CellWidth(got)
			minimal := render.CellWidth(rest(full)) + render.CellWidth("…")
			if st.Emoji {
				minimal += render.CellWidth("🦊 ")
			}
			if width > 0 && width >= minimal && w > width {
				t.Errorf("width %d: line is %d cells: %q", width, w, got)
			}
			if width == 0 || width >= render.CellWidth(full) {
				if got != full {
					t.Errorf("width %d: cut although it fits: %q", width, got)
				}
			}
		}
		name := "width_ascii"
		if st.Emoji {
			name = "width_emoji"
		}
		if st.Color {
			name += "_color"
		}
		golden(t, name, out.String())
	}
}

func TestTruncationKeepsArchivedTag(t *testing.T) {
	t.Parallel()
	s := longSnapshot()
	s.Project.Archived = true
	got := render.Text(s, render.Style{Width: 80}, now)
	if !strings.HasPrefix(got, "…/") || !strings.Contains(got, "project archived br ") {
		t.Fatalf("got %q", got)
	}
}

func TestFieldSubsetOrder(t *testing.T) {
	t.Parallel()
	s := fullSnapshot()
	s.Fields = []status.FieldResult{s.Fields[1], s.Fields[2], s.Fields[3]}
	if got := render.Text(s, render.Style{}, now); got != "group/sub/project ci 2r/1p last:ok main 3m mr 5" {
		t.Fatalf("got %q", got)
	}
}
