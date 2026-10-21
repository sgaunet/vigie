package scan

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/sgaunet/vigie/internal/repo"
)

// inspectWorkers bounds the candidates read by git at once.
const inspectWorkers = 8

// Inspected is a candidate git could read.
type Inspected struct {
	Rel      string
	Checkout repo.Checkout
}

// Inspect reads each candidate's branch and remote with repo.Detect, the code path of
// single mode, 8 at a time. Both results keep the input order.
func Inspect(ctx context.Context, cands []Candidate, remote string, log *slog.Logger) ([]Inspected, []Skip) {
	type result struct {
		co  repo.Checkout
		err error
	}
	results := make([]result, len(cands))
	sem := make(chan struct{}, inspectWorkers)
	var wg sync.WaitGroup
	for i, c := range cands {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i].co, results[i].err = repo.Detect(ctx, c.Path, remote, log)
		})
	}
	wg.Wait()

	var (
		inspected []Inspected
		skips     []Skip
	)
	for i, r := range results {
		rel := cands[i].Rel
		switch {
		case r.err == nil:
			inspected = append(inspected, Inspected{Rel: rel, Checkout: r.co})
		case errors.Is(r.err, repo.ErrRemoteMissing):
			skips = append(skips, Skip{Rel: rel, Reason: ReasonNoRemote, Err: r.err})
		case errors.Is(r.err, repo.ErrRemoteURL):
			skips = append(skips, Skip{Rel: rel, Reason: ReasonRemoteURL, Err: r.err})
		default: // git refused the directory: dubious ownership, broken worktree link
			skips = append(skips, Skip{Rel: rel, Reason: ReasonUnreadable, Err: r.err})
		}
	}
	return inspected, skips
}
