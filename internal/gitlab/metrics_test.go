package gitlab_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
	"github.com/sgaunet/vigie/internal/status"
)

var _ status.Source = (*gitlab.ProjectClient)(nil)

func metrics(t *testing.T, s *gitlabtest.Server) *gitlab.ProjectClient {
	t.Helper()
	return newClient(t, s.URL()).ForProject(42)
}

func TestBranchesPaginated(t *testing.T) {
	t.Parallel()
	now := time.Now()
	p := gitlabtest.Fixture(now)
	p.Branches = nil
	for i := range 1000 {
		age := 200
		if i%4 == 0 {
			age = 5
		}
		p.Branches = append(p.Branches, gitlabtest.Branch{Name: fmt.Sprint("b", i), CommittedDate: now.AddDate(0, 0, -age)})
	}
	s := gitlabtest.New(t, p)
	s.Set(gitlabtest.EndpointBranches, gitlabtest.Behavior{Delay: 20 * time.Millisecond})
	b, err := metrics(t, s).Branches(context.Background(), now.AddDate(0, 0, -90))
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if b != (status.Branches{Active: status.Count{N: 250}, Total: status.Count{N: 1000}}) {
		t.Fatalf("Branches = %+v", b)
	}
	if n := len(s.Requests()); n != 10 {
		t.Fatalf("requests = %d, want 10", n)
	}
	if m := s.MaxInFlight(gitlabtest.EndpointBranches); m > 4 || m < 2 {
		t.Fatalf("max in flight = %d, want 2..4", m)
	}
	q := s.Requests()[0].Query
	if q.Get("per_page") != "100" {
		t.Fatalf("query = %v", q)
	}
}

func TestBranchesSmall(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	b, err := metrics(t, s).Branches(context.Background(), time.Now().AddDate(0, 0, -90))
	if err != nil || b != (status.Branches{Active: status.Count{N: 2}, Total: status.Count{N: 3}}) {
		t.Fatalf("Branches = %+v, %v", b, err)
	}
}

func TestBranchesCapped(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.Set(gitlabtest.EndpointBranches, gitlabtest.Behavior{NoTotal: true})
	b, err := metrics(t, s).Branches(context.Background(), time.Now().AddDate(0, 0, -90))
	if err != nil || !b.Total.Capped || !b.Active.Capped {
		t.Fatalf("Branches = %+v, %v", b, err)
	}
	if n := len(s.Requests()); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
}

func TestPipelines(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	p, err := metrics(t, s).Pipelines(context.Background())
	if err != nil || p != (status.Pipelines{Running: status.Count{N: 2}, Pending: status.Count{N: 1}}) {
		t.Fatalf("Pipelines = %+v, %v", p, err)
	}
	for _, r := range s.Requests() {
		if r.Query.Get("per_page") != "1" {
			t.Errorf("query = %v", r.Query)
		}
	}
}

func TestLastPipeline(t *testing.T) {
	t.Parallel()
	now := time.Now().Truncate(time.Second)
	s := gitlabtest.New(t, gitlabtest.Fixture(now))
	l, err := metrics(t, s).LastPipeline(context.Background(), "main")
	if err != nil {
		t.Fatalf("LastPipeline: %v", err)
	}
	if l.ID != 48213 || l.Status != status.PipelineSuccess || l.Ref != "main" ||
		!l.UpdatedAt.Equal(now.Add(-3*time.Minute)) || l.WebURL == "" {
		t.Fatalf("LastPipeline = %+v", l)
	}
	q := s.Requests()[0].Query
	if q.Get("ref") != "main" || q.Get("per_page") != "1" || q.Get("order_by") != "id" || q.Get("sort") != "desc" {
		t.Fatalf("query = %v", q)
	}

	l, err = metrics(t, s).LastPipeline(context.Background(), "nopipeline")
	if err != nil || l.Status != status.PipelineNone {
		t.Fatalf("no pipeline: %+v, %v", l, err)
	}
	n := len(s.Requests())
	l, err = metrics(t, s).LastPipeline(context.Background(), "")
	if err != nil || l.Status != status.PipelineNone || len(s.Requests()) != n {
		t.Fatalf("empty ref: %+v, %v", l, err)
	}
}

func TestOpenMRs(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	c, err := metrics(t, s).OpenMRs(context.Background())
	if err != nil || c != (status.Count{N: 5}) {
		t.Fatalf("OpenMRs = %+v, %v", c, err)
	}
	q := s.Requests()[0].Query
	if q.Get("state") != "opened" || q.Get("per_page") != "1" {
		t.Fatalf("query = %v", q)
	}
	s.Set(gitlabtest.EndpointMRs, gitlabtest.Behavior{NoTotal: true})
	if c, err = metrics(t, s).OpenMRs(context.Background()); err != nil || !c.Capped {
		t.Fatalf("capped: %+v, %v", c, err)
	}
}

func TestCountWithoutTotalOnEmptyPage(t *testing.T) {
	t.Parallel()
	p := gitlabtest.Fixture(time.Now())
	p.OpenMRs = 0
	s := gitlabtest.New(t, p)
	s.Set(gitlabtest.EndpointMRs, gitlabtest.Behavior{NoTotal: true})
	if c, err := metrics(t, s).OpenMRs(context.Background()); err != nil || c != (status.Count{}) {
		t.Fatalf("empty page: %+v, %v", c, err)
	}
}

func TestOpenIssues(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	c, err := metrics(t, s).OpenIssues(context.Background())
	if err != nil || c != (status.Count{N: 17}) {
		t.Fatalf("OpenIssues = %+v, %v", c, err)
	}
}

func TestMetricUnavailable(t *testing.T) {
	t.Parallel()
	for _, code := range []int{http.StatusForbidden, http.StatusNotFound} {
		s := newServer(t)
		s.SetAll(gitlabtest.Behavior{Status: code})
		m := metrics(t, s)
		ctx := context.Background()
		_, e1 := m.Branches(ctx, time.Now())
		_, e2 := m.Pipelines(ctx)
		_, e3 := m.LastPipeline(ctx, "main")
		_, e4 := m.OpenMRs(ctx)
		_, e5 := m.OpenIssues(ctx)
		for i, err := range []error{e1, e2, e3, e4, e5} {
			if !errors.Is(err, status.ErrUnavailable) {
				t.Errorf("status %d, metric %d: got %v, want ErrUnavailable", code, i, err)
			}
		}
	}
}

func TestMetricUnauthorized(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.Set(gitlabtest.EndpointMRs, gitlabtest.Behavior{Status: http.StatusUnauthorized})
	if _, err := metrics(t, s).OpenMRs(context.Background()); !errors.Is(err, forge.ErrUnauthorized) {
		t.Fatalf("got %v", err)
	}
}
