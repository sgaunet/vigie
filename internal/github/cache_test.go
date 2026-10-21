package github_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/github/githubtest"
)

func TestRevalidation(t *testing.T) {
	t.Parallel()
	r := githubtest.Fixture(time.Now())
	s := githubtest.New(t, r)
	c := newClientOpts(t, s.URL(), forge.Options{Revalidate: true})
	first, err := c.Project(context.Background(), "octo/app")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Project(context.Background(), "octo/app")
	reqs := s.Requests()
	if err != nil || second != first || len(reqs) != 2 || reqs[1].IfNoneMatch == "" || s.Billed(githubtest.PoolCore) != 1 {
		t.Fatalf("revalidated lookup = %+v, %v; requests %+v, billed %d", second, err, reqs, s.Billed(githubtest.PoolCore))
	}

	r.Archived = true
	s.SetRepo(r)
	if p, err := c.Project(context.Background(), "octo/app"); err != nil || !p.Archived || s.Billed(githubtest.PoolCore) != 2 {
		t.Fatalf("changed repository = %+v, %v, billed %d", p, err, s.Billed(githubtest.PoolCore))
	}

	// GraphQL has no conditional requests.
	src := c.Source(first)
	for range 2 {
		if _, err := src.OpenMRs(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, req := range requestsTo(s, githubtest.EndpointPullRequests) {
		if req.IfNoneMatch != "" {
			t.Fatalf("GraphQL request with If-None-Match: %+v", req)
		}
	}
}

func TestNoRevalidation(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	c := newClient(t, s.URL())
	for range 2 {
		if _, err := c.Project(context.Background(), "octo/app"); err != nil {
			t.Fatal(err)
		}
	}
	for _, req := range s.Requests() {
		if req.IfNoneMatch != "" {
			t.Fatalf("If-None-Match sent without Revalidate: %+v", req)
		}
	}
	if n := s.Billed(githubtest.PoolCore); n != 2 {
		t.Fatalf("billed %d, want 2", n)
	}
}

func TestNotModifiedWithoutEntry(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{Status: http.StatusNotModified})
	_, err := newClientOpts(t, s.URL(), forge.Options{Revalidate: true}).Project(context.Background(), "octo/app")
	if err == nil || !strings.HasPrefix(err.Error(), "malformed response from") {
		t.Fatalf("got %v", err)
	}
}

func TestRevalidationConcurrent(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	c := newClientOpts(t, s.URL(), forge.Options{Revalidate: true})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := c.Project(context.Background(), "octo/app"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}
