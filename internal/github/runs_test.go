package github_test

import (
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/github"
	"github.com/sgaunet/vigie/internal/status"
)

func TestRunState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status, conclusion string
		want               status.PipelineStatus
	}{
		{"in_progress", "", status.PipelineRunning},
		{"queued", "", status.PipelinePending},
		{"requested", "", status.PipelinePending},
		{"pending", "", status.PipelinePending},
		{"waiting", "", status.PipelineManual},
		{"completed", "success", status.PipelineSuccess},
		{"completed", "neutral", status.PipelineSuccess},
		{"completed", "failure", status.PipelineFailed},
		{"completed", "timed_out", status.PipelineFailed},
		{"completed", "startup_failure", status.PipelineFailed},
		{"completed", "cancelled", status.PipelineCanceled},
		{"completed", "stale", status.PipelineCanceled},
		{"completed", "skipped", status.PipelineSkipped},
		{"completed", "action_required", status.PipelineManual},
		{"completed", "", status.PipelineUnknown},
		{"completed", "weird", status.PipelineUnknown},
		{"weird", "", status.PipelineUnknown},
	}
	for _, c := range cases {
		if got := github.RunState(c.status, c.conclusion); got != c.want {
			t.Errorf("RunState(%q, %q) = %q, want %q", c.status, c.conclusion, got, c.want)
		}
	}
}

var t0 = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

// done is a completed run of workflow wf on commit sha, updated min minutes after t0.
func done(id, wf int64, sha, conclusion string, minutes int) github.Run {
	return github.Run{ID: id, WorkflowID: wf, HeadSHA: sha, Status: "completed", Conclusion: conclusion, UpdatedAt: t0.Add(time.Duration(minutes) * time.Minute)}
}

func TestCombine(t *testing.T) {
	t.Parallel()
	const url = "https://x/octo/app/commit/new"
	runs := []github.Run{
		done(12, 2, "new", "failure", 3),
		done(11, 1, "new", "success", 5),
		done(10, 3, "old", "failure", 9), // an older commit never counts
	}
	got := github.Combine(runs, "main", url)
	want := status.LastPipeline{Status: status.PipelineFailed, Ref: "main", UpdatedAt: t0.Add(5 * time.Minute), WebURL: url}
	if got != want {
		t.Fatalf("Combine = %+v, want %+v", got, want)
	}

	// A later run of the same workflow replaces the earlier one.
	rerun := append([]github.Run{done(13, 2, "new", "success", 7)}, runs...)
	if got := github.Combine(rerun, "main", url); got.Status != status.PipelineSuccess || !got.UpdatedAt.Equal(t0.Add(7*time.Minute)) {
		t.Fatalf("re-run: %+v", got)
	}

	if got := github.Combine(nil, "main", url); got.Status != status.PipelineNone || got.Ref != "main" {
		t.Fatalf("no run: %+v", got)
	}
}

func TestCombinePrecedence(t *testing.T) {
	t.Parallel()
	order := []github.Run{
		{Status: "in_progress"},
		{Status: "queued"},
		{Status: "waiting"},
		{Status: "completed", Conclusion: "failure"},
		{Status: "completed", Conclusion: "cancelled"},
		{Status: "completed", Conclusion: "weird"},
		{Status: "completed", Conclusion: "success"},
		{Status: "completed", Conclusion: "skipped"},
	}
	want := []status.PipelineStatus{status.PipelineRunning, status.PipelinePending, status.PipelineManual, status.PipelineFailed,
		status.PipelineCanceled, status.PipelineUnknown, status.PipelineSuccess, status.PipelineSkipped}
	for i := range len(order) - 1 {
		a, b := order[i], order[i+1]
		a.ID, a.WorkflowID, a.HeadSHA = 2, 1, "s"
		b.ID, b.WorkflowID, b.HeadSHA = 1, 2, "s"
		for _, runs := range [][]github.Run{{a, b}, {b, a}} {
			if got := github.Combine(runs, "main", ""); got.Status != want[i] || got.ID != 0 {
				t.Errorf("%s with %s: %+v, want %s", want[i], want[i+1], got, want[i])
			}
		}
	}
}
