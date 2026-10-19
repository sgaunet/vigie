package status_test

import (
	"errors"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

func snapshot() status.Snapshot {
	t0 := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	return status.Snapshot{
		Project: status.Project{Path: "g/p", WebURL: "https://gitlab.com/g/p"},
		Branch:  "main",
		Fields: []status.FieldResult{
			{Name: status.FieldBranches, Branches: status.Branches{Active: status.Count{N: 1}, Total: status.Count{N: 2}}},
			{Name: status.FieldPipelines, Pipelines: status.Pipelines{Running: status.Count{N: 1}}},
			{Name: status.FieldLast, Last: status.LastPipeline{ID: 3, Status: status.PipelineRunning, Ref: "main", UpdatedAt: t0}},
			{Name: status.FieldMRs, Count: status.Count{N: 4}},
			{Name: status.FieldIssues, Err: status.ErrUnavailable},
		},
		FetchedAt: t0,
	}
}

func TestFingerprintIgnoresTimes(t *testing.T) {
	t.Parallel()
	a, b := snapshot(), snapshot()
	b.FetchedAt = b.FetchedAt.Add(time.Hour)
	b.StaleSince = time.Now()
	b.RefreshErr = errors.New("another failure message")
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("fingerprint changed with times only")
	}
}

func TestFingerprintDetectsChanges(t *testing.T) {
	t.Parallel()
	changes := map[string]func(*status.Snapshot){
		"branches":   func(s *status.Snapshot) { s.Fields[0].Branches.Total.N = 3 },
		"capped":     func(s *status.Snapshot) { s.Fields[3].Count = status.Count{Capped: true} },
		"pipelines":  func(s *status.Snapshot) { s.Fields[1].Pipelines.Pending.N = 1 },
		"last":       func(s *status.Snapshot) { s.Fields[2].Last.Status = status.PipelineSuccess },
		"last id":    func(s *status.Snapshot) { s.Fields[2].Last.ID = 4 },
		"mrs":        func(s *status.Snapshot) { s.Fields[3].Count.N = 5 },
		"field err":  func(s *status.Snapshot) { s.Fields[3].Err = errors.New("x") },
		"err text":   func(s *status.Snapshot) { s.Fields[4].Err = errors.New("other") },
		"project":    func(s *status.Snapshot) { s.Project.Path = "g/q" },
		"archived":   func(s *status.Snapshot) { s.Project.Archived = true },
		"branch":     func(s *status.Snapshot) { s.Branch = "dev" },
		"stale":      func(s *status.Snapshot) { s.Stale = true },
		"field list": func(s *status.Snapshot) { s.Fields = s.Fields[1:] },
	}
	base := snapshot().Fingerprint()
	for name, change := range changes {
		s := snapshot()
		change(&s)
		if s.Fingerprint() == base {
			t.Errorf("%s: fingerprint unchanged", name)
		}
	}
}
