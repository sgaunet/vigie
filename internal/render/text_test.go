package render_test

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/render"
	"github.com/sgaunet/vigie/internal/status"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var now = time.Date(2026, 10, 19, 9, 15, 0, 0, time.UTC)

// golden compares got with testdata/<name>.golden, rewriting it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden (run go test -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n got: %q\nwant: %q", name, got, want)
	}
}

func count(n int) status.Count { return status.Count{N: n} }

// fullSnapshot is the example line of the CLI contract.
func fullSnapshot() status.Snapshot {
	return status.Snapshot{
		Project: status.Project{Path: "group/sub/project", WebURL: "https://gitlab.com/group/sub/project", Platform: status.PlatformGitLab},
		Branch:  "main",
		Fields: []status.FieldResult{
			{Name: status.FieldBranches, Branches: status.Branches{Active: count(12), Total: count(42)}},
			{Name: status.FieldPipelines, Pipelines: status.Pipelines{Running: count(2), Pending: count(1)}},
			{Name: status.FieldLast, Last: status.LastPipeline{ID: 48213, Status: status.PipelineSuccess, Ref: "main", UpdatedAt: now.Add(-3 * time.Minute), WebURL: "https://gitlab.com/group/sub/project/-/pipelines/48213"}},
			{Name: status.FieldMRs, Count: count(5)},
			{Name: status.FieldIssues, Count: count(17)},
		},
		FetchedAt: now,
	}
}

var (
	emojiColor = render.Style{Color: true, Emoji: true}
	asciiPlain = render.Style{}
)

// both renders a snapshot in the two extreme styles, one per line.
func both(s status.Snapshot) string {
	return render.Text(s, emojiColor, now) + "\n" + render.Text(s, asciiPlain, now) + "\n"
}

func TestTextFull(t *testing.T) {
	t.Parallel()
	golden(t, "text_full_emoji_color", render.Text(fullSnapshot(), emojiColor, now))
	golden(t, "text_full_ascii", render.Text(fullSnapshot(), asciiPlain, now))
	golden(t, "text_full_emoji_nocolor", render.Text(fullSnapshot(), render.Style{Emoji: true}, now))
	golden(t, "text_full_ascii_color", render.Text(fullSnapshot(), render.Style{Color: true}, now))

	if got := render.Text(fullSnapshot(), asciiPlain, now); got != "group/sub/project br 12/42 ci 2r/1p last:ok main 3m mr 5 is 17" {
		t.Errorf("ascii line = %q", got)
	}
	if got := render.Text(fullSnapshot(), render.Style{Emoji: true}, now); got != "🦊 group/sub/project 🌿 12/42 ⚙️ 2r/1p ✅ main 3m 🔀 5 🐛 17" {
		t.Errorf("emoji line = %q", got)
	}
}

func TestTextPipelineStatuses(t *testing.T) {
	t.Parallel()
	for _, st := range []status.PipelineStatus{
		status.PipelineCreated, status.PipelineWaitingForResource, status.PipelinePreparing,
		status.PipelinePending, status.PipelineRunning, status.PipelineSuccess, status.PipelineFailed,
		status.PipelineCanceled, status.PipelineSkipped, status.PipelineManual, status.PipelineScheduled,
		status.PipelineNone, status.PipelineUnknown,
	} {
		s := fullSnapshot()
		s.Fields = []status.FieldResult{{Name: status.FieldLast, Last: status.LastPipeline{ID: 9, Status: st, Ref: "main", UpdatedAt: now.Add(-12 * time.Second)}}}
		if st == status.PipelineNone {
			s.Fields[0].Last = status.LastPipeline{Status: st, Ref: "main"}
		}
		golden(t, "text_last_"+string(st), both(s))
	}
}

func TestTextUnavailableAndCapped(t *testing.T) {
	t.Parallel()
	s := fullSnapshot()
	for i := range s.Fields {
		s.Fields[i] = status.FieldResult{Name: s.Fields[i].Name, Err: status.ErrUnavailable}
	}
	golden(t, "text_unavailable", both(s))
	if !strings.Contains(render.Text(s, asciiPlain, now), "br – ci – last:– mr – is –") {
		t.Errorf("unavailable line = %q", render.Text(s, asciiPlain, now))
	}

	s = fullSnapshot()
	capped := status.Count{Capped: true}
	s.Fields[0].Branches = status.Branches{Active: capped, Total: capped}
	s.Fields[1].Pipelines = status.Pipelines{Running: capped, Pending: count(0)}
	s.Fields[3].Count = capped
	s.Fields[4].Count = capped
	golden(t, "text_capped", both(s))
	if got := render.Text(s, asciiPlain, now); !strings.Contains(got, "br 10k+/10k+ ci 10k+r/0p") || !strings.Contains(got, "mr 10k+ is 10k+") {
		t.Errorf("capped line = %q", got)
	}
	s.Fields[4].Err = errors.New("cannot reach gitlab.com: timeout")
	if got := render.Text(s, asciiPlain, now); !strings.HasSuffix(got, "is –") {
		t.Errorf("transport error line = %q", got)
	}
}

func TestTextArchived(t *testing.T) {
	t.Parallel()
	s := fullSnapshot()
	s.Project.Archived = true
	s.Fields = nil
	golden(t, "text_archived", both(s))
	if got := render.Text(s, asciiPlain, now); got != "group/sub/project archived" {
		t.Errorf("archived = %q", got)
	}
}

func TestTextAges(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{12 * time.Second, "12s"}, {3 * time.Minute, "3m"}, {150 * time.Minute, "2h"},
		{4*24*time.Hour + time.Hour, "4d"}, {-time.Minute, "0s"},
	} {
		s := fullSnapshot()
		s.Fields = []status.FieldResult{{Name: status.FieldLast, Last: status.LastPipeline{ID: 1, Status: status.PipelineSuccess, Ref: "main", UpdatedAt: now.Add(-c.ago)}}}
		if got := render.Text(s, asciiPlain, now); !strings.HasSuffix(got, "main "+c.want) {
			t.Errorf("age %v: %q", c.ago, got)
		}
		if got := render.Text(s, render.Style{Emoji: true}, now); !strings.HasSuffix(got, "main "+c.want) {
			t.Errorf("age %v emoji: %q", c.ago, got)
		}
	}
}

func TestTextDimAndBold(t *testing.T) {
	t.Parallel()
	s := fullSnapshot()
	s.Fields[0].Branches = status.Branches{Active: count(1), Total: count(1)}
	s.Fields[1].Pipelines = status.Pipelines{Running: count(0), Pending: count(0)}
	s.Fields[3].Count = count(0)
	s.Fields[4].Count = count(1)
	golden(t, "text_dim_zero", both(s))
	s.Fields[1].Pipelines = status.Pipelines{Running: count(0), Pending: count(3)}
	golden(t, "text_pending_only", both(s))
}

func TestTextPlainHasNoEscapes(t *testing.T) {
	t.Parallel()
	got := render.Text(fullSnapshot(), asciiPlain, now)
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("escape code in plain output: %q", got)
	}
	for _, r := range got {
		if r > 0x7f && r != '–' {
			t.Fatalf("non-ASCII rune %q in plain output: %q", r, got)
		}
	}
}

// gitHubSnapshot is the fixture line of the GitHub fake server.
func gitHubSnapshot() status.Snapshot {
	return status.Snapshot{
		Project: status.Project{Path: "octo/app", WebURL: "https://github.com/octo/app", Platform: status.PlatformGitHub},
		Branch:  "main",
		Fields: []status.FieldResult{
			{Name: status.FieldBranches, Branches: status.Branches{Active: count(3), Total: count(5)}},
			{Name: status.FieldPipelines, Pipelines: status.Pipelines{Running: count(1), Pending: count(2)}},
			{Name: status.FieldLast, Last: status.LastPipeline{Status: status.PipelineFailed, Ref: "main", UpdatedAt: now.Add(-3 * time.Minute), WebURL: "https://github.com/octo/app/commit/c0ffee0000000000000000000000000000000000"}},
			{Name: status.FieldMRs, Count: count(5)},
			{Name: status.FieldIssues, Count: count(12)},
		},
		FetchedAt: now,
	}
}

func TestTextPlatformIcon(t *testing.T) {
	t.Parallel()
	golden(t, "text_github_emoji_nocolor", render.Text(gitHubSnapshot(), render.Style{Emoji: true}, now))
	golden(t, "text_github_ascii", render.Text(gitHubSnapshot(), asciiPlain, now))

	s := fullSnapshot()
	s.Project.Platform = ""
	if got := render.Text(s, render.Style{Emoji: true}, now); !strings.HasPrefix(got, "🗼 group/sub/project ") {
		t.Errorf("no platform: %q", got)
	}
	if render.CellWidth("🦊") != 2 || render.CellWidth("🐙") != 2 {
		t.Errorf("icon widths = %d, %d", render.CellWidth("🦊"), render.CellWidth("🐙"))
	}
}
