package github_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/github/githubtest"
	"github.com/sgaunet/vigie/internal/status"
)

// source looks octo/app up on s and returns its metrics source.
func source(t *testing.T, s *githubtest.Server) status.Source {
	t.Helper()
	c := newClient(t, s.URL())
	p, err := c.Project(context.Background(), "octo/app")
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	return c.Source(p)
}

// requestsTo returns the requests received on e, after the lookup.
func requestsTo(s *githubtest.Server, e githubtest.Endpoint) []githubtest.Request {
	var out []githubtest.Request
	for _, r := range s.Requests() {
		if r.Endpoint == e {
			out = append(out, r)
		}
	}
	return out
}

func withBranches(now time.Time, n int) githubtest.Repo {
	r := githubtest.Fixture(now)
	r.Branches = nil
	for i := range n {
		age := 200
		if i%5 == 0 {
			age = 5
		}
		r.Branches = append(r.Branches, githubtest.Branch{Name: fmt.Sprint("b", i), CommittedDate: now.AddDate(0, 0, -age)})
	}
	return r
}

func TestBranches(t *testing.T) {
	t.Parallel()
	now := time.Now()
	b, err := source(t, githubtest.New(t, githubtest.Fixture(now))).Branches(context.Background(), now.AddDate(0, 0, -90))
	if err != nil || b != (status.Branches{Active: status.Count{N: 3}, Total: status.Count{N: 5}}) {
		t.Fatalf("Branches = %+v, %v", b, err)
	}

	s := githubtest.New(t, withBranches(now, 250))
	b, err = source(t, s).Branches(context.Background(), now.AddDate(0, 0, -90))
	if err != nil || b != (status.Branches{Active: status.Count{N: 50}, Total: status.Count{N: 250}}) {
		t.Fatalf("250 branches = %+v, %v", b, err)
	}
	reqs := requestsTo(s, githubtest.EndpointBranches)
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	for _, r := range reqs {
		if r.Method != http.MethodPost || r.Operation != "Branches" || r.Path != "/api/graphql" {
			t.Errorf("request = %+v", r)
		}
	}
}

func TestBranchesCapped(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := githubtest.New(t, withBranches(now, 10001))
	b, err := source(t, s).Branches(context.Background(), now.AddDate(0, 0, -90))
	if err != nil || !b.Active.Capped || !b.Total.Capped {
		t.Fatalf("Branches = %+v, %v", b, err)
	}
	if n := len(requestsTo(s, githubtest.EndpointBranches)); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
}

func TestBranchesEmpty(t *testing.T) {
	t.Parallel()
	r := githubtest.Fixture(time.Now())
	r.Branches = nil
	b, err := source(t, githubtest.New(t, r)).Branches(context.Background(), time.Now())
	if err != nil || b != (status.Branches{}) {
		t.Fatalf("Branches = %+v, %v", b, err)
	}
}

func TestPipelines(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	p, err := source(t, s).Pipelines(context.Background())
	if err != nil || p != (status.Pipelines{Running: status.Count{N: 1}, Pending: status.Count{N: 2}}) {
		t.Fatalf("Pipelines = %+v, %v", p, err)
	}
	reqs := requestsTo(s, githubtest.EndpointRuns)
	var states []string
	for _, r := range reqs {
		q, _ := url.ParseQuery(r.Query)
		if q.Get("per_page") != "1" || r.Path != "/api/v3/repos/octo/app/actions/runs" {
			t.Errorf("request = %+v", r)
		}
		states = append(states, q.Get("status"))
	}
	if strings.Join(slices.Sorted(slices.Values(states)), ",") != "in_progress,pending,queued,requested" {
		t.Fatalf("status filters = %v", states)
	}
}

func TestPipelinesInexact(t *testing.T) {
	t.Parallel()
	r := githubtest.Fixture(time.Now())
	for i := range 1000 {
		r.Runs = append(r.Runs, githubtest.Run{ID: int64(1000 + i), WorkflowID: 9, HeadBranch: "busy", HeadSHA: "b", Status: "queued"})
	}
	_, err := source(t, githubtest.New(t, r)).Pipelines(context.Background())
	if err == nil || err.Error() != "more than 1,000 workflow runs: GitHub does not report an exact count" {
		t.Fatalf("got %v", err)
	}
}

func TestRunsRefused(t *testing.T) {
	t.Parallel()
	for _, b := range []githubtest.Behavior{
		{Status: http.StatusForbidden, Header: map[string]string{"X-Accepted-GitHub-Permissions": "actions=read"}},
		{Status: http.StatusNotFound},
	} {
		s := newServer(t)
		src := source(t, s)
		s.Set(githubtest.EndpointRuns, b)
		_, e1 := src.Pipelines(context.Background())
		_, e2 := src.LastPipeline(context.Background(), "main")
		for _, err := range []error{e1, e2} {
			if !errors.Is(err, status.ErrUnavailable) {
				t.Errorf("%d: got %v, want ErrUnavailable", b.Status, err)
			}
			if b.Status == http.StatusForbidden && !strings.Contains(err.Error(), `"actions=read"`) {
				t.Errorf("403 reason does not name the permission: %v", err)
			}
		}
	}
}

func TestLastPipeline(t *testing.T) {
	t.Parallel()
	now := time.Now().Truncate(time.Second)
	s := githubtest.New(t, githubtest.Fixture(now))
	src := source(t, s)
	l, err := src.LastPipeline(context.Background(), "main")
	sha := githubtest.Fixture(now).Runs[0].HeadSHA
	want := status.LastPipeline{Status: status.PipelineFailed, Ref: "main", UpdatedAt: now.Add(-3 * time.Minute), WebURL: s.URL() + "/octo/app/commit/" + sha}
	if err != nil || !l.UpdatedAt.Equal(want.UpdatedAt) || l.Status != want.Status || l.Ref != want.Ref || l.WebURL != want.WebURL || l.ID != 0 {
		t.Fatalf("LastPipeline = %+v, %v; want %+v", l, err, want)
	}
	reqs := requestsTo(s, githubtest.EndpointRuns)
	if len(reqs) != 1 || reqs[0].Query != "branch=main&per_page=30" {
		t.Fatalf("requests = %+v", reqs)
	}

	if l, err := src.LastPipeline(context.Background(), "feature"); err != nil || l.Status != status.PipelineRunning {
		t.Fatalf("feature: %+v, %v", l, err)
	}
	if l, err := src.LastPipeline(context.Background(), "nope"); err != nil || l.Status != status.PipelineNone || l.Ref != "nope" {
		t.Fatalf("no run: %+v, %v", l, err)
	}
	n := len(s.Requests())
	if l, err := src.LastPipeline(context.Background(), ""); err != nil || l.Status != status.PipelineNone || len(s.Requests()) != n {
		t.Fatalf("empty ref: %+v, %v", l, err)
	}
}

func TestLastPipelineManyRunsOfOneCommit(t *testing.T) {
	t.Parallel()
	r := githubtest.Fixture(time.Now())
	r.Runs = nil
	for i := 40; i >= 1; i-- { // newest first; the oldest workflow run failed
		conclusion := "success"
		if i == 1 {
			conclusion = "failure"
		}
		r.Runs = append(r.Runs, githubtest.Run{ID: int64(100 + i), WorkflowID: int64(i), HeadBranch: "big", HeadSHA: "abc", Status: "completed", Conclusion: conclusion})
	}
	s := githubtest.New(t, r)
	l, err := source(t, s).LastPipeline(context.Background(), "big")
	if err != nil || l.Status != status.PipelineFailed {
		t.Fatalf("LastPipeline = %+v, %v", l, err)
	}
	reqs := requestsTo(s, githubtest.EndpointRuns)
	if len(reqs) != 2 || reqs[1].Query != "branch=big&head_sha=abc&per_page=100" {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestLastPipelineCommitWithoutRun(t *testing.T) {
	t.Parallel()
	// The branch head started no run: the newest run's commit, older, decides.
	r := githubtest.Fixture(time.Now())
	r.Runs = []githubtest.Run{{ID: 5, WorkflowID: 1, HeadBranch: "main", HeadSHA: "older", Status: "completed", Conclusion: "success"}}
	l, err := source(t, githubtest.New(t, r)).LastPipeline(context.Background(), "main")
	if err != nil || l.Status != status.PipelineSuccess || !strings.HasSuffix(l.WebURL, "/commit/older") {
		t.Fatalf("LastPipeline = %+v, %v", l, err)
	}
}

func TestCounts(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	src := source(t, s)
	if c, err := src.OpenMRs(context.Background()); err != nil || c != (status.Count{N: 5}) {
		t.Fatalf("OpenMRs = %+v, %v", c, err)
	}
	if c, err := src.OpenIssues(context.Background()); err != nil || c != (status.Count{N: 12}) {
		t.Fatalf("OpenIssues = %+v, %v", c, err)
	}
	for _, e := range []githubtest.Endpoint{githubtest.EndpointPullRequests, githubtest.EndpointIssues} {
		if reqs := requestsTo(s, e); len(reqs) != 1 || reqs[0].Operation == "" {
			t.Fatalf("%s: requests = %+v", e, reqs)
		}
	}

	big := githubtest.Fixture(time.Now())
	big.OpenPRs, big.OpenIssues = 10001, 10001
	src = source(t, githubtest.New(t, big))
	if c, err := src.OpenMRs(context.Background()); err != nil || !c.Capped {
		t.Fatalf("capped OpenMRs = %+v, %v", c, err)
	}
	if c, err := src.OpenIssues(context.Background()); err != nil || !c.Capped {
		t.Fatalf("capped OpenIssues = %+v, %v", c, err)
	}
}

func TestIssuesUnavailable(t *testing.T) {
	t.Parallel()
	r := githubtest.Fixture(time.Now())
	r.HasIssues = false
	if _, err := source(t, githubtest.New(t, r)).OpenIssues(context.Background()); !errors.Is(err, status.ErrUnavailable) {
		t.Fatalf("issues disabled: got %v", err)
	}

	// A repository GraphQL cannot resolve (repository: null with NOT_FOUND).
	s := newServer(t)
	c := newClient(t, s.URL())
	src := c.Source(forge.Project{ID: 9, Path: "octo/gone", WebURL: s.URL() + "/octo/gone"})
	for name, f := range map[string]func() error{
		"issues":   func() error { _, err := src.OpenIssues(context.Background()); return err },
		"mrs":      func() error { _, err := src.OpenMRs(context.Background()); return err },
		"branches": func() error { _, err := src.Branches(context.Background(), time.Now()); return err },
	} {
		if err := f(); !errors.Is(err, status.ErrUnavailable) {
			t.Errorf("%s: got %v, want ErrUnavailable", name, err)
		}
	}
}

func TestFieldUnauthorized(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	src := source(t, s)
	s.SetAll(githubtest.Behavior{Status: http.StatusUnauthorized})
	ctx := context.Background()
	_, e1 := src.Branches(ctx, time.Now())
	_, e2 := src.Pipelines(ctx)
	_, e3 := src.LastPipeline(ctx, "main")
	_, e4 := src.OpenMRs(ctx)
	_, e5 := src.OpenIssues(ctx)
	for i, err := range []error{e1, e2, e3, e4, e5} {
		if !errors.Is(err, forge.ErrUnauthorized) {
			t.Errorf("field %d: got %v, want ErrUnauthorized", i, err)
		}
	}
	fields := []status.FieldResult{{Name: status.FieldMRs, Err: e4}, {Name: status.FieldIssues}}
	if !errors.Is(forge.RefreshError(fields), forge.ErrUnauthorized) {
		t.Fatal("a rejected token must fail the refresh")
	}
}
