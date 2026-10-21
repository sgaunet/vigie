package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/github"
	"github.com/sgaunet/vigie/internal/github/githubtest"
	"github.com/sgaunet/vigie/internal/token"
)

const tokenValue = "ghp_t3stT0ken"

var _ forge.Client = (*github.Client)(nil)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func newServer(t *testing.T) *githubtest.Server {
	t.Helper()
	return githubtest.New(t, githubtest.Fixture(time.Now()))
}

func newClientOpts(t *testing.T, base string, opts forge.Options) *github.Client {
	t.Helper()
	opts.Insecure = true
	c, err := github.NewClient(mustURL(t, base), token.New(tokenValue), opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func newClient(t *testing.T, base string) *github.Client {
	t.Helper()
	return newClientOpts(t, base, forge.Options{})
}

func TestAPIBases(t *testing.T) {
	t.Parallel()
	cases := []struct{ base, rest, graphql string }{
		{"https://github.com", "https://api.github.com", "https://api.github.com/graphql"},
		{"https://github.example.com", "https://github.example.com/api/v3", "https://github.example.com/api/graphql"},
		{"https://ghe.corp/sub", "https://ghe.corp/sub/api/v3", "https://ghe.corp/sub/api/graphql"},
	}
	for _, c := range cases {
		cl, err := github.NewClient(mustURL(t, c.base), token.New(tokenValue), forge.Options{})
		if err != nil {
			t.Fatalf("%s: %v", c.base, err)
		}
		if rest, graphql := github.APIBases(cl); rest != c.rest || graphql != c.graphql {
			t.Errorf("%s: %s %s, want %s %s", c.base, rest, graphql, c.rest, c.graphql)
		}
	}
}

func TestNewClientPlainHTTP(t *testing.T) {
	t.Parallel()
	u := mustURL(t, "http://127.0.0.1:1")
	if _, err := github.NewClient(u, token.New(tokenValue), forge.Options{}); !errors.Is(err, forge.ErrInsecure) {
		t.Fatalf("got %v, want ErrInsecure", err)
	}
	if _, err := github.NewClient(u, token.New(tokenValue), forge.Options{Insecure: true}); err != nil {
		t.Fatalf("insecure: %v", err)
	}
}

func TestProjectByPath(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	p, err := newClient(t, s.URL()).Project(context.Background(), "octo/app")
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	want := forge.Project{ID: 42, Path: "octo/app", WebURL: s.URL() + "/octo/app", DefaultBranch: "main"}
	if p != want {
		t.Fatalf("Project = %+v, want %+v", p, want)
	}
	reqs := s.Requests()
	if len(reqs) != 1 || reqs[0].Path != "/api/v3/repos/octo/app" {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestProjectByID(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	p, err := newClient(t, s.URL()).Project(context.Background(), "42")
	if err != nil || p.Path != "octo/app" {
		t.Fatalf("Project = %+v, %v", p, err)
	}
	if got := s.Requests()[0].Path; got != "/api/v3/repositories/42" {
		t.Fatalf("path = %q", got)
	}
}

func TestProjectArchivedAndEmpty(t *testing.T) {
	t.Parallel()
	r := githubtest.Fixture(time.Now())
	r.Archived = true
	s := githubtest.New(t, r)
	empty := githubtest.Fixture(time.Now())
	empty.ID, empty.FullName, empty.Branches, empty.Runs = 43, "octo/empty", nil, nil
	s.AddRepo(empty)
	c := newClient(t, s.URL())
	if p, err := c.Project(context.Background(), "octo/app"); err != nil || !p.Archived {
		t.Fatalf("archived: %+v, %v", p, err)
	}
	if p, err := c.Project(context.Background(), "octo/empty"); err != nil || p.DefaultBranch != "" || p.ID != 43 {
		t.Fatalf("empty: %+v, %v", p, err)
	}
}

func TestRequestHeaders(t *testing.T) {
	t.Parallel()
	var got []string
	srv := headerServer(t, &got)
	if _, err := newClient(t, srv).Project(context.Background(), "octo/app"); err != nil {
		t.Fatal(err)
	}
	want := []string{"Bearer " + tokenValue, "application/vnd.github+json", "2022-11-28", "vigie"}
	if len(got) != len(want) {
		t.Fatalf("headers = %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("header %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// headerServer answers every request with a repository and records the request headers
// vigie sets.
func headerServer(t *testing.T, got *[]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = []string{r.Header.Get("Authorization"), r.Header.Get("Accept"), r.Header.Get("X-GitHub-Api-Version"), r.Header.Get("User-Agent")}
		_, _ = w.Write([]byte(`{"id":1,"full_name":"octo/app","html_url":"https://x/octo/app","default_branch":"main"}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
