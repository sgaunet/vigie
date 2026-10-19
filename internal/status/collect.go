package status

import (
	"context"
	"sync"
	"time"
)

// Source fetches the metrics of one project. gitlab.ProjectClient implements it; tests use
// fakes.
type Source interface {
	Branches(ctx context.Context, activeSince time.Time) (Branches, error)
	Pipelines(ctx context.Context) (Pipelines, error)
	LastPipeline(ctx context.Context, ref string) (LastPipeline, error)
	OpenMRs(ctx context.Context) (Count, error)
	OpenIssues(ctx context.Context) (Count, error)
}

// Request describes one refresh.
type Request struct {
	Fields      []FieldName
	Branch      string        // ref of the last-pipeline field
	ActiveSince time.Time     // branches committed since then are active
	Timeout     time.Duration // budget of the whole refresh
}

// Collect fetches every requested field concurrently under one deadline. A failing field
// never cancels the others; results keep the requested order.
func Collect(ctx context.Context, src Source, req Request) []FieldResult {
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	results := make([]FieldResult, len(req.Fields))
	var wg sync.WaitGroup
	for i, name := range req.Fields {
		wg.Go(func() { results[i] = collectOne(ctx, src, req, name) })
	}
	wg.Wait()
	return results
}

func collectOne(ctx context.Context, src Source, req Request, name FieldName) FieldResult {
	r := FieldResult{Name: name}
	switch name {
	case FieldBranches:
		r.Branches, r.Err = src.Branches(ctx, req.ActiveSince)
	case FieldPipelines:
		r.Pipelines, r.Err = src.Pipelines(ctx)
	case FieldLast:
		r.Last, r.Err = src.LastPipeline(ctx, req.Branch)
	case FieldMRs:
		r.Count, r.Err = src.OpenMRs(ctx)
	case FieldIssues:
		r.Count, r.Err = src.OpenIssues(ctx)
	}
	return r
}
