package watch

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

// Refresh periods (FR-005, FR-018).
const (
	BranchCacheTTL = 60 * time.Second
	ProjectRefresh = 5 * time.Minute
)

// Target is a resolved project, its reporting branch and its metrics source.
type Target struct {
	Project status.Project
	Branch  string
	Source  status.Source
}

// Config wires the loop. The function fields keep watch free of any HTTP or terminal code.
type Config struct {
	Interval   time.Duration
	Fields     []status.FieldName
	Timeout    time.Duration
	ActiveDays int

	Target  Target                                  // resolved at startup
	Resolve func(context.Context) (Target, error)  // re-run every ProjectRefresh
	Draw    func(s status.Snapshot, inFlight bool) error
	Resize  <-chan struct{} // redraw the last snapshot without fetching; may be nil

	Fatal      func(error) bool                    // errors that stop the loop (rejected token, denied access)
	RetryAfter func(error) (time.Duration, bool) // server delay hint carried by an error
}

// Run refreshes until ctx is cancelled (returns nil) or a fatal error occurs (returns it).
// The next refresh starts one delay after the previous one ended, so refreshes never
// overlap; a failed refresh keeps the last values and marks them stale.
func Run(ctx context.Context, cfg Config) error {
	l := loop{cfg: cfg, target: cfg.Target, resolvedAt: time.Now(), backoff: Backoff{Base: cfg.Interval}}
	l.cache.ttl = BranchCacheTTL
	for {
		if err := l.refresh(ctx); err != nil || ctx.Err() != nil {
			return err
		}
		if err := l.wait(ctx); err != nil || ctx.Err() != nil {
			return err
		}
	}
}

type loop struct {
	cfg        Config
	target     Target
	resolvedAt time.Time
	backoff    Backoff
	cache      branchCache
	last       status.Snapshot
	have       bool
}

func (l *loop) refresh(ctx context.Context) error {
	if time.Since(l.resolvedAt) >= ProjectRefresh {
		t, err := l.cfg.Resolve(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil && l.cfg.Fatal(err):
			return err
		case err == nil:
			if t.Project.Path != l.target.Project.Path {
				l.cache.reset()
			}
			l.target, l.resolvedAt = t, time.Now()
		}
		// A transient failure keeps the old target and retries at the next refresh.
	}
	if l.have {
		if err := l.cfg.Draw(l.last, true); err != nil {
			return err
		}
	}

	now := time.Now()
	l.cache.served = false
	fields := status.Collect(ctx, cachedSource{l.target.Source, &l.cache}, status.Request{
		Fields:      l.cfg.Fields,
		Branch:      l.target.Branch,
		ActiveSince: now.AddDate(0, 0, -l.cfg.ActiveDays),
		Timeout:     l.cfg.Timeout,
	})
	if ctx.Err() != nil {
		return nil
	}
	for _, f := range fields {
		if f.Err != nil && l.cfg.Fatal(f.Err) {
			return f.Err
		}
	}

	if err := failure(fields, l.cache.served); err != nil {
		l.backoff.Failure(l.retryAfter(fields))
		if !l.have {
			l.last = status.Snapshot{Project: l.target.Project, Branch: l.target.Branch, Fields: fields}
		}
		if !l.last.Stale {
			l.last.StaleSince = time.Now()
		}
		l.last.Stale, l.last.RefreshErr = true, err
	} else {
		l.backoff.Success()
		l.last = status.Snapshot{Project: l.target.Project, Branch: l.target.Branch, Fields: fields, FetchedAt: time.Now()}
	}
	l.have = true
	return l.cfg.Draw(l.last, false)
}

// wait sleeps until the next refresh, redrawing on resize.
func (l *loop) wait(ctx context.Context) error {
	timer := time.NewTimer(l.backoff.Next())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return nil
		case <-l.cfg.Resize:
			if err := l.cfg.Draw(l.last, false); err != nil {
				return err
			}
		}
	}
}

func (l *loop) retryAfter(fields []status.FieldResult) time.Duration {
	var d time.Duration
	for _, f := range fields {
		if f.Err == nil || l.cfg.RetryAfter == nil {
			continue
		}
		if ra, ok := l.cfg.RetryAfter(f.Err); ok {
			d = max(d, ra)
		}
	}
	return d
}

// failure returns the refresh error when every field fetched from the server failed with
// something other than "unavailable" (the deadline passed, or the server could not be
// reached), else nil. A branch count served from the cache proves nothing either way.
func failure(fields []status.FieldResult, cachedBranches bool) error {
	var first error
	for _, f := range fields {
		if f.Name == status.FieldBranches && cachedBranches {
			continue
		}
		if f.Err == nil || errors.Is(f.Err, status.ErrUnavailable) {
			return nil
		}
		if first == nil {
			first = f.Err
		}
	}
	return first
}

// branchCache reuses a successful branch count for ttl: listing branches is the only
// costly call (FR-018).
type branchCache struct {
	mu     sync.Mutex
	ttl    time.Duration
	at     time.Time
	val    status.Branches
	ok     bool
	served bool // the current refresh got its branch count from the cache
}

func (c *branchCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ok = false
}

type cachedSource struct {
	status.Source
	cache *branchCache
}

func (s cachedSource) Branches(ctx context.Context, activeSince time.Time) (status.Branches, error) {
	c := s.cache
	c.mu.Lock()
	if c.ok && time.Since(c.at) < c.ttl {
		v := c.val
		c.served = true
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()
	v, err := s.Source.Branches(ctx, activeSince)
	if err != nil {
		return v, err
	}
	c.mu.Lock()
	c.val, c.at, c.ok = v, time.Now(), true
	c.mu.Unlock()
	return v, nil
}
