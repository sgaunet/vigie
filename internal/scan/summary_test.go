package scan_test

import (
	"errors"
	"testing"

	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/scan"
)

func skips(reasons ...scan.Reason) []scan.Skip {
	out := make([]scan.Skip, 0, len(reasons))
	for _, r := range reasons {
		out = append(out, scan.Skip{Rel: "x", Reason: r})
	}
	return out
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	s := scan.Summarize(4, skips(scan.ReasonUnknownHost, scan.ReasonNoRemote), "origin")
	if s.Found != 6 || s.Listed != 4 || s.Skipped[scan.ReasonUnknownHost] != 1 || s.Skipped[scan.ReasonNoRemote] != 1 || s.Remote != "origin" {
		t.Fatalf("Summarize = %+v", s)
	}
	cases := []struct {
		s    scan.Summary
		want string
	}{
		{s, `6 repositories found: 4 listed, 2 skipped (1 no remote "origin", 1 unknown host); run with -v to list them`},
		{scan.Summarize(4, nil, "origin"), "4 repositories found: 4 listed"},
		{scan.Summarize(1, nil, "origin"), "1 repository found: 1 listed"},
	}
	for _, c := range cases {
		if got := c.s.Line(); got != c.want {
			t.Errorf("Line() = %q, want %q", got, c.want)
		}
	}
}

func TestSummarySkipsOrder(t *testing.T) {
	t.Parallel()
	s := scan.Summarize(0, skips(scan.ReasonOtherHost, scan.ReasonUnknownHost, scan.ReasonRemoteURL,
		scan.ReasonNoRemote, scan.ReasonUnreadable, scan.ReasonBare, scan.ReasonBare), "upstream")
	want := `7 skipped (2 bare, 1 unreadable, 1 no remote "upstream", 1 remote URL not understood, 1 unknown host, 1 other host)`
	if got := s.Skips(); got != want {
		t.Fatalf("Skips() = %q, want %q", got, want)
	}
	if got := scan.Summarize(2, nil, "origin").Skips(); got != "" {
		t.Fatalf("Skips() without skips = %q", got)
	}
}

func TestSummaryErr(t *testing.T) {
	t.Parallel()
	if err := scan.Summarize(1, skips(scan.ReasonBare), "origin").Err(4); err != nil {
		t.Fatalf("listed: %v", err)
	}
	err := scan.Summarize(0, nil, "origin").Err(4)
	want := "not a git repository: no clone found below it either (depth 4); run vigie in a clone or a folder of clones, or pass --project and --host"
	if !errors.Is(err, repo.ErrNotRepository) || err.Error() != want {
		t.Fatalf("nothing found: %v", err)
	}
	err = scan.Summarize(0, skips(scan.ReasonNoRemote, scan.ReasonUnknownHost, scan.ReasonUnknownHost), "origin").Err(4)
	want = `no GitLab repository found: 3 skipped (1 no remote "origin", 2 unknown host); add self-hosted hosts to the config file "hosts", or run with -v`
	if !errors.Is(err, scan.ErrNoGitLab) || errors.Is(err, repo.ErrNotRepository) || err.Error() != want {
		t.Fatalf("none listed: %v", err)
	}
}

func TestReasonText(t *testing.T) {
	t.Parallel()
	cases := map[scan.Reason]string{
		scan.ReasonBare:        "bare",
		scan.ReasonUnreadable:  "unreadable",
		scan.ReasonNoRemote:    `no remote "origin"`,
		scan.ReasonRemoteURL:   "remote URL not understood",
		scan.ReasonUnknownHost: "unknown host",
		scan.ReasonOtherHost:   "other host",
	}
	for r, want := range cases {
		if got := r.Text("origin"); got != want {
			t.Errorf("%v.Text() = %q, want %q", r, got, want)
		}
	}
}
