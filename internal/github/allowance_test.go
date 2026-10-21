package github_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/github"
	"github.com/sgaunet/vigie/internal/github/githubtest"
	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/watch"
)

// handlerTransport serves every request with h, in process.
type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	resp := rec.Result()
	resp.Request = r
	return resp, nil
}

// bubbleClient returns a revalidating client of s that never leaves the synctest bubble.
func bubbleClient(t *testing.T, s *githubtest.Server) *github.Client {
	t.Helper()
	c := newClientOpts(t, s.URL(), forge.Options{Revalidate: true})
	github.WithRoundTripper(c, handlerTransport{s.Handler()})
	return c
}

// watchFor runs watch mode on octo/app for d at the default interval, all five fields, and
// returns what it drew. It must run inside a synctest bubble.
func watchFor(t *testing.T, c *github.Client, d time.Duration) []status.Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	target := func(ctx context.Context) (watch.Target, error) {
		p, err := c.Project(ctx, "octo/app")
		return watch.Target{Project: status.Project{Path: p.Path, WebURL: p.WebURL, Platform: status.PlatformGitHub}, Branch: "main", Source: c.Source(p)}, err
	}
	first, err := target(ctx)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	var drawn []status.Snapshot
	err = watch.Run(ctx, watch.Config{
		Interval: 5 * time.Second, Fields: status.DefaultFields(), Timeout: 10 * time.Second, ActiveDays: 90,
		Target:  first,
		Resolve: target,
		Draw: func(s status.Snapshot, inFlight bool) error {
			if !inFlight {
				drawn = append(drawn, s)
			}
			return nil
		},
		Fatal:      func(error) bool { return false },
		RetryAfter: forge.RetryAfter,
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	return drawn
}

func checkBudget(t *testing.T, s *githubtest.Server) {
	t.Helper()
	core, graphql := s.Billed(githubtest.PoolCore), s.Billed(githubtest.PoolGraphQL)
	t.Logf("one hour billed %d REST requests and %d GraphQL points", core, graphql)
	if core > 2500 || graphql > 2500 {
		t.Fatalf("billed %d REST requests and %d GraphQL points, want at most 2500 each (SC-005)", core, graphql)
	}
}

func TestWatchAllowance(t *testing.T) {
	t.Parallel()
	s := githubtest.New(t, withBranches(time.Now(), 100))
	synctest.Test(t, func(t *testing.T) {
		watchFor(t, bubbleClient(t, s), time.Hour)
	})
	checkBudget(t, s)
}

func TestWatchAllowanceChangingRuns(t *testing.T) {
	t.Parallel()
	r := withBranches(time.Now(), 100)
	s := githubtest.New(t, r)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { // the main branch's run changes state every 2 minutes
			defer close(done)
			tick := time.NewTicker(2 * time.Minute)
			defer tick.Stop()
			for i := 0; ; i++ {
				select {
				case <-ctx.Done():
					return
				case now := <-tick.C:
					r.Runs = slices.Clone(r.Runs) // the server holds the previous slice
					r.Runs[0].Status, r.Runs[0].Conclusion = "in_progress", ""
					if i%2 == 1 {
						r.Runs[0].Status, r.Runs[0].Conclusion = "completed", "failure"
					}
					r.Runs[0].UpdatedAt = now
					s.SetRepo(r)
				}
			}
		}()
		watchFor(t, bubbleClient(t, s), time.Hour)
		cancel()
		<-done
	})
	checkBudget(t, s)
}
