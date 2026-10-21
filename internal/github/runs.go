package github

import (
	"cmp"
	"slices"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

// run is one workflow run of the REST runs listing.
type run struct {
	ID         int64     `json:"id"`
	WorkflowID int64     `json:"workflow_id"`
	HeadSHA    string    `json:"head_sha"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"` // null until completed, decoded as ""
	UpdatedAt  time.Time `json:"updated_at"`
}

// runState maps a run's status and conclusion to a vigie state (research G6, FR-014).
func runState(st, conclusion string) status.PipelineStatus {
	switch st {
	case "in_progress":
		return status.PipelineRunning
	case "queued", "requested", "pending":
		return status.PipelinePending
	case "waiting":
		return status.PipelineManual
	case "completed":
		switch conclusion {
		case "success", "neutral":
			return status.PipelineSuccess
		case "failure", "timed_out", "startup_failure":
			return status.PipelineFailed
		case "cancelled", "stale":
			return status.PipelineCanceled
		case "skipped":
			return status.PipelineSkipped
		case "action_required":
			return status.PipelineManual
		}
	}
	return status.PipelineUnknown
}

// precedence orders the combined states: the first one any counted run has wins.
var precedence = []status.PipelineStatus{
	status.PipelineRunning, status.PipelinePending, status.PipelineManual, status.PipelineFailed,
	status.PipelineCanceled, status.PipelineUnknown, status.PipelineSuccess, status.PipelineSkipped,
}

// newest returns the run with the highest ID: the latest created, since a re-run keeps its
// ID. runs must not be empty.
func newest(runs []run) run {
	return slices.MaxFunc(runs, func(a, b run) int { return cmp.Compare(a.ID, b.ID) })
}

// combine gives the last field from runs: the runs of the newest run's commit, the latest
// of each workflow, the first state of precedence any of them has, and the time of their
// latest update. A combined result has no single run ID; commitURL links the commit.
func combine(runs []run, ref, commitURL string) status.LastPipeline {
	if len(runs) == 0 {
		return status.LastPipeline{Status: status.PipelineNone, Ref: ref}
	}
	sha := newest(runs).HeadSHA
	latest := map[int64]run{} // per workflow
	for _, r := range runs {
		if r.HeadSHA != sha {
			continue
		}
		if l, ok := latest[r.WorkflowID]; !ok || r.ID > l.ID {
			latest[r.WorkflowID] = r
		}
	}
	out := status.LastPipeline{Ref: ref, WebURL: commitURL}
	best := len(precedence)
	for _, r := range latest {
		if i := slices.Index(precedence, runState(r.Status, r.Conclusion)); i < best {
			best = i
		}
		if r.UpdatedAt.After(out.UpdatedAt) {
			out.UpdatedAt = r.UpdatedAt
		}
	}
	out.Status = precedence[best]
	return out
}
