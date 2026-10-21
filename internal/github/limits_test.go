package github_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/github"
	"github.com/sgaunet/vigie/internal/github/githubtest"
	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/watch"
)

// The tests that set the gate clock are not parallel.

// fixedClock sets the gate clock to a whole second and returns it.
func fixedClock(t *testing.T) time.Time {
	t.Helper()
	at := time.Now().Truncate(time.Second)
	old := *github.Now
	*github.Now = func() time.Time { return at }
	t.Cleanup(func() { *github.Now = old })
	return at
}

func octoApp(s *githubtest.Server) forge.Project {
	return forge.Project{ID: 42, Path: "octo/app", WebURL: s.URL() + "/octo/app", DefaultBranch: "main"}
}

func TestPrimaryLimitREST(t *testing.T) {
	at := fixedClock(t)
	reset := at.Add(13 * time.Minute)
	s := newServer(t)
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{RateLimited: true, Reset: reset})
	c := newClient(t, s.URL())
	_, err := c.Project(context.Background(), "octo/app")
	if !errors.Is(err, forge.ErrRateLimit) || !errors.Is(err, forge.ErrNetwork) || errors.Is(err, forge.ErrUnauthorized) || errors.Is(err, forge.ErrForbidden) {
		t.Fatalf("got %v", err)
	}
	if d, ok := forge.RetryAfter(err); !ok || d != 13*time.Minute+time.Second {
		t.Fatalf("RetryAfter = %v %v", d, ok)
	}
	want := hostOf(s) + " request allowance exhausted until " + reset.Local().Format("15:04") + " (in 13m): wait for the reset; it is shared by all your GitHub tokens"
	if err.Error() != want {
		t.Fatalf("message:\n got %q\nwant %q", err, want)
	}

	n := len(s.Requests())
	_, again := c.Project(context.Background(), "octo/app")
	if d, _ := forge.RetryAfter(again); d != 13*time.Minute+time.Second || len(s.Requests()) != n {
		t.Fatalf("while blocked: %v, RetryAfter %v, %d new requests", again, d, len(s.Requests())-n)
	}
	if m, err := c.Source(octoApp(s)).OpenMRs(context.Background()); err != nil || m.N != 5 {
		t.Fatalf("GraphQL pool: %+v, %v", m, err)
	}
}

func TestPrimaryLimitGraphQL(t *testing.T) {
	at := fixedClock(t)
	s := newServer(t)
	s.Set(githubtest.EndpointPullRequests, githubtest.Behavior{RateLimited: true, Reset: at.Add(time.Hour)})
	c := newClient(t, s.URL())
	src := c.Source(octoApp(s))
	_, err := src.OpenMRs(context.Background())
	if !errors.Is(err, forge.ErrRateLimit) || errors.Is(err, status.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	if _, err := src.OpenIssues(context.Background()); !errors.Is(err, forge.ErrRateLimit) || len(requestsTo(s, githubtest.EndpointIssues)) != 0 {
		t.Fatalf("graphql pool not blocked: %v", err)
	}
	if _, err := c.Project(context.Background(), "octo/app"); err != nil {
		t.Fatalf("core pool blocked: %v", err)
	}
}

func TestSecondaryLimit(t *testing.T) {
	fixedClock(t)
	for _, c := range []struct {
		b    githubtest.Behavior
		want time.Duration
	}{
		{githubtest.Behavior{Status: http.StatusForbidden, Header: map[string]string{"Retry-After": "30"}}, 30 * time.Second},
		{githubtest.Behavior{Secondary: true}, time.Minute},
	} {
		s := newServer(t)
		s.Set(githubtest.EndpointRepo, c.b)
		cl := newClient(t, s.URL())
		_, err := cl.Project(context.Background(), "octo/app")
		if !errors.Is(err, forge.ErrNetwork) || errors.Is(err, forge.ErrRateLimit) || errors.Is(err, forge.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if d, ok := forge.RetryAfter(err); !ok || d != c.want {
			t.Fatalf("RetryAfter = %v %v, want %v", d, ok, c.want)
		}
		if _, err := cl.Source(octoApp(s)).OpenMRs(context.Background()); err == nil || len(requestsTo(s, githubtest.EndpointPullRequests)) != 0 {
			t.Fatalf("graphql pool not blocked by a secondary limit: %v", err)
		}
	}
}

func TestLimitReopens(t *testing.T) {
	at := fixedClock(t)
	s := newServer(t)
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{RateLimited: true, Reset: at.Add(time.Minute), Times: 1})
	c := newClient(t, s.URL())
	if _, err := c.Project(context.Background(), "octo/app"); !errors.Is(err, forge.ErrRateLimit) {
		t.Fatalf("got %v", err)
	}
	*github.Now = func() time.Time { return at.Add(time.Minute + time.Second) }
	if p, err := c.Project(context.Background(), "octo/app"); err != nil || p.ID != 42 || len(s.Requests()) != 2 {
		t.Fatalf("after the reset: %+v, %v, %d requests", p, err, len(s.Requests()))
	}
}

func TestNotLimits(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	c := newClient(t, s.URL())
	// A 429 without rate headers, as in US1, and a 403 from an instance with limits off.
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{Status: http.StatusTooManyRequests, Times: 1})
	if _, err := c.Project(context.Background(), "octo/app"); !errors.Is(err, forge.ErrNetwork) || errors.Is(err, forge.ErrRateLimit) {
		t.Fatalf("429: %v", err)
	}
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{Status: http.StatusForbidden, Times: 1})
	if _, err := c.Project(context.Background(), "octo/app"); !errors.Is(err, forge.ErrForbidden) {
		t.Fatalf("403: %v", err)
	}
	if _, err := c.Project(context.Background(), "octo/app"); err != nil || len(s.Requests()) != 3 {
		t.Fatalf("no gate after plain answers: %v, %d requests", err, len(s.Requests()))
	}

	s.SetAll(githubtest.Behavior{RateLimited: true, Reset: time.Now().Add(time.Hour)})
	src := newClient(t, s.URL()).Source(octoApp(s))
	_, e1 := src.OpenIssues(context.Background())
	_, e2 := src.Pipelines(context.Background())
	for _, err := range []error{e1, e2} {
		if !errors.Is(err, forge.ErrRateLimit) || errors.Is(err, status.ErrUnavailable) {
			t.Errorf("rate-limited field: %v", err)
		}
	}
}

func TestLimitRetries(t *testing.T) {
	fastRetries(t)
	s := newServer(t)
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{Status: http.StatusForbidden, Header: map[string]string{"Retry-After": "1"}, Times: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p, err := retryClient(t, s, true, nil).Project(ctx, "octo/app"); err != nil || p.ID != 42 || len(s.Requests()) != 2 {
		t.Fatalf("secondary limit: %+v, %v, %d requests", p, err, len(s.Requests()))
	}

	s = newServer(t)
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{RateLimited: true, Reset: time.Now().Add(time.Minute), Times: 1})
	if _, err := retryClient(t, s, true, nil).Project(ctx, "octo/app"); !errors.Is(err, forge.ErrRateLimit) || len(s.Requests()) != 1 {
		t.Fatalf("primary limit: %v, %d requests", err, len(s.Requests()))
	}
}

// timedTransport serves requests in process and records when each one was sent.
type timedTransport struct {
	handlerTransport
	mu    sync.Mutex
	times []time.Time
}

func (t *timedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.times = append(t.times, time.Now())
	t.mu.Unlock()
	return t.handlerTransport.RoundTrip(r)
}

func TestWatchWaitsForReset(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	synctest.Test(t, func(t *testing.T) {
		c := newClient(t, s.URL())
		tr := &timedTransport{handlerTransport: handlerTransport{s.Handler()}}
		github.WithRoundTripper(c, tr)
		p, err := c.Project(context.Background(), "octo/app")
		if err != nil {
			t.Fatal(err)
		}
		reset := time.Now().Add(10 * time.Minute)
		s.SetAll(githubtest.Behavior{RateLimited: true, Reset: reset})
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		var staleAt, freshAt time.Time
		err = runWatch(ctx, c, p, func(snap status.Snapshot) {
			switch {
			case snap.Stale && staleAt.IsZero():
				staleAt = time.Now()
				s.SetAll(githubtest.Behavior{}) // only the gate stands between vigie and GitHub now
			case !snap.Stale && !staleAt.IsZero() && freshAt.IsZero():
				freshAt = time.Now()
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if staleAt.IsZero() || freshAt.IsZero() {
			t.Fatalf("stale at %v, fresh at %v", staleAt, freshAt)
		}
		until := time.Unix(reset.Unix(), 0).Add(time.Second)
		tr.mu.Lock()
		defer tr.mu.Unlock()
		for _, at := range tr.times {
			if at.After(staleAt) && at.Before(until) {
				t.Fatalf("request at %v, before the reset %v", at, until)
			}
		}
		if freshAt.After(until.Add(5 * time.Second)) {
			t.Fatalf("fresh values at %v, more than one interval after the reset %v", freshAt, until)
		}
	})
}

// runWatch runs watch mode on p at the default interval until ctx ends, and hands every
// drawn snapshot to draw.
func runWatch(ctx context.Context, c *github.Client, p forge.Project, draw func(status.Snapshot)) error {
	target := watch.Target{Project: status.Project{Path: p.Path, WebURL: p.WebURL, Platform: status.PlatformGitHub}, Branch: "main", Source: c.Source(p)}
	return watch.Run(ctx, watch.Config{
		Interval: 5 * time.Second, Fields: status.DefaultFields(), Timeout: 10 * time.Second, ActiveDays: 90,
		Target:  target,
		Resolve: func(context.Context) (watch.Target, error) { return target, nil },
		Draw: func(s status.Snapshot, inFlight bool) error {
			if !inFlight {
				draw(s)
			}
			return nil
		},
		Fatal:      func(error) bool { return false },
		RetryAfter: forge.RetryAfter,
	})
}
