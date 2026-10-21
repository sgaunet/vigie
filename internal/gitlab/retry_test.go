package gitlab_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
	"github.com/sgaunet/vigie/internal/token"
)

// The tests below are not parallel: they shorten the package backoff base.

func fastRetries(t *testing.T) {
	t.Helper()
	old := *gitlab.RetryBase
	*gitlab.RetryBase = time.Millisecond
	t.Cleanup(func() { *gitlab.RetryBase = old })
}

func retryClient(t *testing.T, s *gitlabtest.Server, retry bool, throttled *atomic.Int32) *gitlab.Client {
	t.Helper()
	u, err := url.Parse(s.URL())
	if err != nil {
		t.Fatal(err)
	}
	opts := forge.Options{Insecure: true, Retry: retry}
	if throttled != nil {
		opts.OnThrottle = func() { throttled.Add(1) }
	}
	c, err := gitlab.NewClient(u, token.New(tokenValue), opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRetryBaseDefault(t *testing.T) {
	if *gitlab.RetryBase != 250*time.Millisecond {
		t.Fatalf("backoff base = %v, want 250ms", *gitlab.RetryBase)
	}
}

func TestRetryThenSuccess(t *testing.T) {
	fastRetries(t)
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		s := newServer(t)
		s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: code, Times: 2})
		var throttled atomic.Int32
		p, err := retryClient(t, s, true, &throttled).Project(context.Background(), "group/app")
		if err != nil || p.ID != 42 {
			t.Fatalf("%d: Project = %+v, %v", code, p, err)
		}
		if n := len(s.Requests()); n != 3 || throttled.Load() != 2 {
			t.Fatalf("%d: %d requests, OnThrottle called %d times", code, n, throttled.Load())
		}
	}
}

func TestRetryHonoursRetryAfter(t *testing.T) {
	fastRetries(t)
	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusTooManyRequests, RetryAfter: "1", Times: 1})
	start := time.Now()
	if _, err := retryClient(t, s, true, nil).Project(context.Background(), "group/app"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < time.Second {
		t.Fatalf("retried after %v, want at least the 1s of Retry-After", d)
	}
}

func TestRetryNotPastDeadline(t *testing.T) {
	fastRetries(t)
	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusTooManyRequests, RetryAfter: "5"})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := retryClient(t, s, true, nil).Project(ctx, "group/app")
	if !errors.Is(err, forge.ErrNetwork) || time.Since(start) > 500*time.Millisecond || len(s.Requests()) != 1 {
		t.Fatalf("got %v after %v and %d requests", err, time.Since(start), len(s.Requests()))
	}
}

func TestRetryBounded(t *testing.T) {
	fastRetries(t)
	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusTooManyRequests, Times: 10})
	_, err := retryClient(t, s, true, nil).Project(context.Background(), "group/app")
	if !errors.Is(err, forge.ErrNetwork) || len(s.Requests()) != 5 {
		t.Fatalf("got %v after %d requests, want ErrNetwork after 5", err, len(s.Requests()))
	}
}

func TestRetryOnlyThrottling(t *testing.T) {
	fastRetries(t)
	for code, want := range map[int]error{http.StatusUnauthorized: forge.ErrUnauthorized, http.StatusNotFound: forge.ErrNotFound} {
		s := newServer(t)
		s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: code})
		var throttled atomic.Int32
		_, err := retryClient(t, s, true, &throttled).Project(context.Background(), "group/app")
		if !errors.Is(err, want) || len(s.Requests()) != 1 || throttled.Load() != 0 {
			t.Fatalf("%d: got %v after %d requests", code, err, len(s.Requests()))
		}
	}
	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusTooManyRequests, Times: 1})
	_, err := retryClient(t, s, false, nil).Project(context.Background(), "group/app")
	if !errors.Is(err, forge.ErrNetwork) || len(s.Requests()) != 1 {
		t.Fatalf("Retry off: got %v after %d requests", err, len(s.Requests()))
	}
}

func TestRetryWaitInterrupted(t *testing.T) {
	fastRetries(t)
	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusTooManyRequests, RetryAfter: "5"})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := retryClient(t, s, true, nil).Project(ctx, "group/app")
	if !errors.Is(err, context.Canceled) || errors.Is(err, forge.ErrNetwork) || time.Since(start) > time.Second {
		t.Fatalf("got %v after %v", err, time.Since(start))
	}
}
