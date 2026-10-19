package watch_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/watch"
)

var (
	errFatal   = errors.New("token rejected")
	errNetwork = errors.New("cannot reach gitlab")
)

// throttled is a failure carrying a Retry-After hint.
type throttled struct{ after time.Duration }

func (throttled) Error() string { return "server answered 429" }

// source is a scripted status.Source. fail decides, per refresh number (1-based), the
// error every field returns. Inside a synctest bubble all calls of one refresh happen at
// the same instant, which identifies the refresh.
type source struct {
	mu       sync.Mutex
	delay    time.Duration
	fail     func(refresh int) error
	mrs      int
	refresh  int
	instant  time.Time
	branches int
	starts   []time.Time
	refs     []string
}

// err must be called with s.mu held.
func (s *source) err() error {
	if now := time.Now(); s.refresh == 0 || !now.Equal(s.instant) {
		s.refresh++
		s.instant = now
		s.starts = append(s.starts, now)
	}
	if s.fail == nil {
		return nil
	}
	return s.fail(s.refresh)
}

func (s *source) Branches(_ context.Context, _ time.Time) (status.Branches, error) {
	s.mu.Lock()
	s.branches++
	err := s.err()
	s.mu.Unlock()
	if err != nil {
		return status.Branches{}, err
	}
	return status.Branches{Total: status.Count{N: 1}}, nil
}

func (s *source) Pipelines(_ context.Context) (status.Pipelines, error) {
	s.mu.Lock()
	err := s.err()
	s.mu.Unlock()
	return status.Pipelines{}, err
}

func (s *source) LastPipeline(_ context.Context, ref string) (status.LastPipeline, error) {
	s.mu.Lock()
	s.refs = append(s.refs, ref)
	err := s.err()
	s.mu.Unlock()
	return status.LastPipeline{Status: status.PipelineNone, Ref: ref}, err
}

// OpenMRs paces the refresh: it sleeps delay before answering.
func (s *source) OpenMRs(ctx context.Context) (status.Count, error) {
	s.mu.Lock()
	err := s.err()
	n := s.mrs
	d := s.delay
	s.mu.Unlock()
	select {
	case <-time.After(d):
	case <-ctx.Done():
		return status.Count{}, ctx.Err()
	}
	return status.Count{N: n}, err
}

func (s *source) OpenIssues(_ context.Context) (status.Count, error) {
	s.mu.Lock()
	err := s.err()
	s.mu.Unlock()
	return status.Count{}, err
}

type draw struct {
	at       time.Time
	snap     status.Snapshot
	inFlight bool
}

type recorder struct {
	mu    sync.Mutex
	draws []draw
}

func (r *recorder) Draw(s status.Snapshot, inFlight bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.draws = append(r.draws, draw{time.Now(), s, inFlight})
	return nil
}

// final returns the non-in-flight draws.
func (r *recorder) final() []draw {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []draw
	for _, d := range r.draws {
		if !d.inFlight {
			out = append(out, d)
		}
	}
	return out
}

func config(src *source, rec *recorder) watch.Config {
	target := watch.Target{Project: status.Project{Path: "g/p"}, Branch: "main", Source: src}
	return watch.Config{
		Interval:   5 * time.Second,
		Fields:     status.DefaultFields(),
		Timeout:    10 * time.Second,
		ActiveDays: 90,
		Target:     target,
		Resolve:    func(context.Context) (watch.Target, error) { return target, nil },
		Draw:       rec.Draw,
		Fatal:      func(err error) bool { return errors.Is(err, errFatal) },
		RetryAfter: func(err error) (time.Duration, bool) {
			var t throttled
			if errors.As(err, &t) {
				return t.after, true
			}
			return 0, false
		},
	}
}

// runFor runs the loop for d, then cancels it and returns its error.
func runFor(t *testing.T, cfg watch.Config, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(d, cancel)
	return watch.Run(ctx, cfg)
}

func TestRefreshesNeverOverlap(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &source{delay: 3 * time.Second}
		rec := &recorder{}
		if err := runFor(t, config(src, rec), 30*time.Second); err != nil {
			t.Fatal(err)
		}
		if len(src.starts) < 3 {
			t.Fatalf("starts = %v", src.starts)
		}
		for i := 1; i < len(src.starts); i++ {
			if gap := src.starts[i].Sub(src.starts[i-1]); gap != 8*time.Second {
				t.Fatalf("gap %d = %v, want 3s refresh + 5s interval", i, gap)
			}
		}
		// Each refresh is preceded by an in-flight draw, except the first.
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if rec.draws[0].inFlight || !rec.draws[1].inFlight {
			t.Fatalf("draw sequence = %+v", rec.draws[:2])
		}
	})
}

func TestStaleStreak(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		src := &source{mrs: 7, fail: func(n int) error {
			if n == 2 || n == 3 {
				return errNetwork
			}
			return nil
		}}
		rec := &recorder{}
		// Refreshes at 0s (ok), 5s (fail), 15s (fail, after 10s backoff), 35s (ok).
		if err := runFor(t, config(src, rec), 36*time.Second); err != nil {
			t.Fatal(err)
		}
		d := rec.final()
		if len(d) != 4 {
			t.Fatalf("draws = %d, want 4", len(d))
		}
		if d[0].snap.Stale || d[0].snap.Fields[3].Count.N != 7 {
			t.Fatalf("first = %+v", d[0].snap)
		}
		for i := 1; i <= 2; i++ {
			s := d[i].snap
			if !s.Stale || !s.StaleSince.Equal(start.Add(5*time.Second)) || !s.FetchedAt.Equal(start) ||
				s.Fields[3].Count.N != 7 || s.Fields[3].Err != nil || !errors.Is(s.RefreshErr, errNetwork) {
				t.Fatalf("stale draw %d = %+v", i, s)
			}
		}
		if d[2].at.Sub(d[1].at) != 10*time.Second {
			t.Fatalf("backoff after first failure = %v", d[2].at.Sub(d[1].at))
		}
		if s := d[3].snap; s.Stale || !s.StaleSince.IsZero() || s.RefreshErr != nil || !s.FetchedAt.Equal(start.Add(35*time.Second)) {
			t.Fatalf("recovered = %+v", s)
		}
	})
}

func TestFieldUnavailableIsNotAFailure(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &source{fail: func(int) error { return status.ErrUnavailable }}
		rec := &recorder{}
		_ = runFor(t, config(src, rec), 11*time.Second)
		for _, d := range rec.final() {
			if d.snap.Stale {
				t.Fatal("unavailable fields marked the snapshot stale")
			}
		}
		if n := len(rec.final()); n != 3 {
			t.Fatalf("draws = %d, want 3 (no backoff)", n)
		}
	})
}

func TestRetryAfterAndServerBackoff(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &source{fail: func(n int) error {
			if n == 1 {
				return throttled{30 * time.Second}
			}
			return nil
		}}
		_ = runFor(t, config(src, &recorder{}), 31*time.Second)
		if len(src.starts) != 2 || src.starts[1].Sub(src.starts[0]) != 30*time.Second {
			t.Fatalf("starts = %v", src.starts)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		src := &source{fail: func(n int) error {
			if n <= 3 {
				return errNetwork // as 5xx answers
			}
			return nil
		}}
		_ = runFor(t, config(src, &recorder{}), 76*time.Second)
		want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 5 * time.Second}
		if len(src.starts) < 5 {
			t.Fatalf("starts = %v", src.starts)
		}
		for i, w := range want {
			if gap := src.starts[i+1].Sub(src.starts[i]); gap != w {
				t.Fatalf("gap %d = %v, want %v", i, gap, w)
			}
		}
	})
}

func TestBranchCache(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &source{}
		_ = runFor(t, config(src, &recorder{}), 2*time.Minute+time.Second)
		// Refreshes every 5s for 121s: 25 refreshes, branch fetches at 0s, 60s, 120s.
		if src.refresh != 25 || src.branches != 3 {
			t.Fatalf("refreshes=%d branch fetches=%d", src.refresh, src.branches)
		}
	})
}

func TestProjectRefresh(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &source{}
		rec := &recorder{}
		cfg := config(src, rec)
		resolves := 0
		cfg.Resolve = func(context.Context) (watch.Target, error) {
			resolves++
			return watch.Target{Project: status.Project{Path: "g/p"}, Branch: "develop", Source: src}, nil
		}
		_ = runFor(t, cfg, 5*time.Minute+time.Second)
		if resolves != 1 {
			t.Fatalf("resolves = %d, want 1 after 5 minutes", resolves)
		}
		if last := src.refs[len(src.refs)-1]; last != "develop" || src.refs[0] != "main" {
			t.Fatalf("refs = %v", src.refs)
		}
		if b := rec.final()[len(rec.final())-1].snap.Branch; b != "develop" {
			t.Fatalf("snapshot branch = %q", b)
		}
	})
}

func TestFatalErrorsStopTheLoop(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg := config(&source{}, &recorder{})
		cfg.Resolve = func(context.Context) (watch.Target, error) { return watch.Target{}, errFatal }
		start := time.Now()
		err := runFor(t, cfg, time.Hour)
		if !errors.Is(err, errFatal) || time.Since(start) != 5*time.Minute {
			t.Fatalf("project: err=%v after %v", err, time.Since(start))
		}
	})
	synctest.Test(t, func(t *testing.T) {
		src := &source{fail: func(n int) error {
			if n == 3 {
				return errFatal
			}
			return nil
		}}
		start := time.Now()
		err := runFor(t, config(src, &recorder{}), time.Hour)
		if !errors.Is(err, errFatal) || time.Since(start) != 10*time.Second {
			t.Fatalf("field: err=%v after %v", err, time.Since(start))
		}
	})
}

func TestCancelReturnsNil(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		if err := runFor(t, config(&source{delay: time.Second}, &recorder{}), 12*time.Second); err != nil {
			t.Fatalf("err = %v", err)
		}
		if el := time.Since(start); el != 12*time.Second {
			t.Fatalf("returned after %v, want at once", el)
		}
	})
}

func TestResizeRedrawsWithoutFetch(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &source{}
		rec := &recorder{}
		cfg := config(src, rec)
		resize := make(chan struct{})
		cfg.Resize = resize
		time.AfterFunc(2*time.Second, func() { resize <- struct{}{} })
		_ = runFor(t, cfg, 3*time.Second)
		d := rec.final()
		if src.refresh != 1 || len(d) != 2 || d[1].at.Sub(d[0].at) != 2*time.Second {
			t.Fatalf("refreshes=%d draws=%d", src.refresh, len(d))
		}
	})
}
