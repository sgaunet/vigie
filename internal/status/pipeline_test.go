package status_test

import (
	"testing"

	"github.com/sgaunet/vigie/internal/status"
)

func TestParsePipelineStatus(t *testing.T) {
	t.Parallel()
	cases := map[string]status.PipelineStatus{
		"created":              status.PipelineCreated,
		"waiting_for_resource": status.PipelineWaitingForResource,
		"preparing":            status.PipelinePreparing,
		"pending":              status.PipelinePending,
		"running":              status.PipelineRunning,
		"success":              status.PipelineSuccess,
		"failed":               status.PipelineFailed,
		"canceled":             status.PipelineCanceled,
		"skipped":              status.PipelineSkipped,
		"manual":               status.PipelineManual,
		"scheduled":            status.PipelineScheduled,
		"":                     status.PipelineNone,
		"canceling":            status.PipelineUnknown,
		"SUCCESS":              status.PipelineUnknown,
	}
	for in, want := range cases {
		if got := status.ParsePipelineStatus(in); got != want {
			t.Errorf("ParsePipelineStatus(%q) = %q, want %q", in, got, want)
		}
	}
	if status.PipelineNone != "none" || status.PipelineUnknown != "unknown" {
		t.Errorf("none/unknown spellings changed: %q %q", status.PipelineNone, status.PipelineUnknown)
	}
}
