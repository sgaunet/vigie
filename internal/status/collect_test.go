package status_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

// fakeSource answers each metric after its delay, or with its error.
type fakeSource struct {
	delay map[status.FieldName]time.Duration
	err   map[status.FieldName]error

	mu     sync.Mutex
	called map[status.FieldName]int
	ref    string
	since  time.Time
}

func (f *fakeSource) wait(ctx context.Context, n status.FieldName) error {
	f.mu.Lock()
	if f.called == nil {
		f.called = map[status.FieldName]int{}
	}
	f.called[n]++
	f.mu.Unlock()
	select {
	case <-time.After(f.delay[n]):
	case <-ctx.Done():
		return ctx.Err()
	}
	return f.err[n]
}

func (f *fakeSource) Branches(ctx context.Context, since time.Time) (status.Branches, error) {
	f.mu.Lock()
	f.since = since
	f.mu.Unlock()
	if err := f.wait(ctx, status.FieldBranches); err != nil {
		return status.Branches{}, err
	}
	return status.Branches{Active: status.Count{N: 1}, Total: status.Count{N: 2}}, nil
}

func (f *fakeSource) Pipelines(ctx context.Context) (status.Pipelines, error) {
	if err := f.wait(ctx, status.FieldPipelines); err != nil {
		return status.Pipelines{}, err
	}
	return status.Pipelines{Running: status.Count{N: 3}}, nil
}

func (f *fakeSource) LastPipeline(ctx context.Context, ref string) (status.LastPipeline, error) {
	f.mu.Lock()
	f.ref = ref
	f.mu.Unlock()
	if err := f.wait(ctx, status.FieldLast); err != nil {
		return status.LastPipeline{}, err
	}
	return status.LastPipeline{ID: 7, Status: status.PipelineRunning, Ref: ref}, nil
}

func (f *fakeSource) OpenMRs(ctx context.Context) (status.Count, error) {
	if err := f.wait(ctx, status.FieldMRs); err != nil {
		return status.Count{}, err
	}
	return status.Count{N: 4}, nil
}

func (f *fakeSource) OpenIssues(ctx context.Context) (status.Count, error) {
	if err := f.wait(ctx, status.FieldIssues); err != nil {
		return status.Count{}, err
	}
	return status.Count{N: 5}, nil
}

func TestCollectRequestedFieldsInOrder(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &fakeSource{}
		since := time.Now().AddDate(0, 0, -90)
		req := status.Request{
			Fields:      []status.FieldName{status.FieldIssues, status.FieldLast, status.FieldBranches},
			Branch:      "dev",
			ActiveSince: since,
			Timeout:     10 * time.Second,
		}
		res := status.Collect(context.Background(), src, req)
		if len(res) != 3 || res[0].Name != status.FieldIssues || res[1].Name != status.FieldLast || res[2].Name != status.FieldBranches {
			t.Fatalf("order = %+v", res)
		}
		if res[0].Count.N != 5 || res[1].Last.ID != 7 || res[2].Branches.Total.N != 2 {
			t.Fatalf("values = %+v", res)
		}
		if src.called[status.FieldMRs] != 0 || src.called[status.FieldPipelines] != 0 {
			t.Fatalf("unrequested fields called: %v", src.called)
		}
		if src.ref != "dev" || !src.since.Equal(since) {
			t.Fatalf("ref=%q since=%v", src.ref, src.since)
		}
	})
}

func TestCollectOneErrorKeepsOthers(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		boom := errors.New("boom")
		src := &fakeSource{err: map[status.FieldName]error{status.FieldMRs: boom}}
		res := status.Collect(context.Background(), src, status.Request{Fields: status.DefaultFields(), Timeout: time.Second})
		for _, r := range res {
			if r.Name == status.FieldMRs {
				if !errors.Is(r.Err, boom) {
					t.Errorf("mrs error = %v", r.Err)
				}
			} else if r.Err != nil {
				t.Errorf("%s: unexpected error %v", r.Name, r.Err)
			}
		}
	})
}

func TestCollectSharedDeadlineAndConcurrency(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &fakeSource{delay: map[status.FieldName]time.Duration{
			status.FieldBranches:  20 * time.Second,
			status.FieldPipelines: 3 * time.Second,
			status.FieldLast:      4 * time.Second,
			status.FieldMRs:       2 * time.Second,
			status.FieldIssues:    1 * time.Second,
		}}
		start := time.Now()
		res := status.Collect(context.Background(), src, status.Request{Fields: status.DefaultFields(), Timeout: 10 * time.Second})
		if elapsed := time.Since(start); elapsed != 10*time.Second {
			t.Fatalf("elapsed = %v, want 10s", elapsed)
		}
		for _, r := range res {
			if r.Name == status.FieldBranches {
				if !errors.Is(r.Err, context.DeadlineExceeded) {
					t.Errorf("branches error = %v", r.Err)
				}
			} else if r.Err != nil {
				t.Errorf("%s: %v", r.Name, r.Err)
			}
		}

		src.delay[status.FieldBranches] = 5 * time.Second
		start = time.Now()
		status.Collect(context.Background(), src, status.Request{Fields: status.DefaultFields(), Timeout: 10 * time.Second})
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("elapsed = %v, want the slowest field (5s)", elapsed)
		}
	})
}
