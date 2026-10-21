package gitlab_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
	"github.com/sgaunet/vigie/internal/token"
)

const tokenValue = "glpat-t3st"

var _ forge.Client = (*gitlab.Client)(nil)

func newServer(t *testing.T) *gitlabtest.Server {
	t.Helper()
	return gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
}

func newClient(t *testing.T, base string) *gitlab.Client {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	c, err := gitlab.NewClient(u, token.New(tokenValue), forge.Options{Insecure: true})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestProjectByPath(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	p, err := newClient(t, s.URL()).Project(context.Background(), "group/app")
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	want := forge.Project{ID: 42, Path: "group/app", WebURL: s.URL() + "/group/app", DefaultBranch: "main"}
	if p != want {
		t.Fatalf("Project = %+v, want %+v", p, want)
	}
	reqs := s.Requests()
	if len(reqs) != 1 || reqs[0].Path != "/api/v4/projects/group%2Fapp" || reqs[0].Token != tokenValue {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestProjectByID(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	if _, err := newClient(t, s.URL()).Project(context.Background(), "42"); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got := s.Requests()[0].Path; got != "/api/v4/projects/42" {
		t.Fatalf("path = %q", got)
	}
}

func TestProjectSeveralOnOneServer(t *testing.T) {
	t.Parallel()
	a := gitlabtest.Fixture(time.Now())
	a.ID = 1
	b := a
	b.ID, b.Path = 2, "group/b"
	s := gitlabtest.New(t, a)
	s.AddProject(b)
	c := newClient(t, s.URL())
	for _, ref := range []string{"group/app", "1", "group/b", "2"} {
		if _, err := c.Project(context.Background(), ref); err != nil {
			t.Errorf("Project(%s): %v", ref, err)
		}
	}
	p, err := c.Project(context.Background(), "2")
	if err != nil || p.Path != "group/b" || p.ID != 2 {
		t.Fatalf("Project(2) = %+v, %v", p, err)
	}
	if _, err := c.Project(context.Background(), "group/c"); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("unknown path: got %v, want ErrProjectNotFound", err)
	}
}

func TestProjectBasePathPrefix(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	// A base URL with a path (GitLab under /gitlab) keeps it in front of /api/v4.
	_, err := newClient(t, s.URL()+"/gitlab").Project(context.Background(), "group/app")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if got := s.Requests()[0].Path; got != "/gitlab/api/v4/projects/group%2Fapp" {
		t.Fatalf("path = %q", got)
	}
}

func TestProjectHTTPErrors(t *testing.T) {
	t.Parallel()
	cases := map[int]error{
		http.StatusUnauthorized:        forge.ErrUnauthorized,
		http.StatusForbidden:           forge.ErrForbidden,
		http.StatusNotFound:            forge.ErrNotFound,
		http.StatusTooManyRequests:     forge.ErrNetwork,
		http.StatusBadGateway:          forge.ErrNetwork,
		http.StatusInternalServerError: forge.ErrNetwork,
	}
	for code, want := range cases {
		s := newServer(t)
		s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: code})
		_, err := newClient(t, s.URL()).Project(context.Background(), "group/app")
		if !errors.Is(err, want) {
			t.Errorf("status %d: got %v, want %v", code, err, want)
		}
	}
}

func TestProjectNotFoundMessage(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	_, err := newClient(t, s.URL()).Project(context.Background(), "no/such")
	want := "project no/such not found on " + s.URL() + ": check the hosts mapping in the config file"
	if err == nil || err.Error() != want {
		t.Fatalf("message = %v, want %q", err, want)
	}
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusTooManyRequests, RetryAfter: "7"})
	_, err := newClient(t, s.URL()).Project(context.Background(), "group/app")
	if d, ok := forge.RetryAfter(err); !ok || d != 7*time.Second {
		t.Fatalf("RetryAfter = %v %v (err %v)", d, ok, err)
	}
	date := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusServiceUnavailable, RetryAfter: date})
	_, err = newClient(t, s.URL()).Project(context.Background(), "group/app")
	if d, ok := forge.RetryAfter(err); !ok || d < 80*time.Second || d > 91*time.Second {
		t.Fatalf("RetryAfter date = %v %v", d, ok)
	}
}

func TestSource(t *testing.T) {
	t.Parallel()
	a := gitlabtest.Fixture(time.Now())
	a.ID = 1
	s := gitlabtest.New(t, a)
	c := newClient(t, s.URL())
	if n, err := c.Source(forge.Project{ID: 1}).OpenMRs(context.Background()); err != nil || n.N != 5 {
		t.Fatalf("OpenMRs = %+v, %v", n, err)
	}
	if reqs := s.Requests(); len(reqs) != 1 || reqs[0].Path != "/api/v4/projects/1/merge_requests" {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestNewClientRefusesPlainHTTP(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("http://gitlab.lan")
	if _, err := gitlab.NewClient(u, token.New(tokenValue), forge.Options{}); !errors.Is(err, forge.ErrInsecure) {
		t.Fatalf("got %v, want ErrInsecure", err)
	}
}

func TestRedirectsAreNotFollowedAcrossHosts(t *testing.T) {
	t.Parallel()
	other := newServer(t)
	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Redirect: other.URL() + "/api/v4/projects/group%2Fapp"})
	_, err := newClient(t, s.URL()).Project(context.Background(), "group/app")
	if err == nil {
		t.Fatal("expected an error")
	}
	if n := len(other.Requests()); n != 0 {
		t.Fatalf("the other host received %d requests", n)
	}
}

func TestNetworkErrors(t *testing.T) {
	t.Parallel()
	_, err := newClient(t, "http://127.0.0.1:1").Project(context.Background(), "group/app")
	if !errors.Is(err, forge.ErrNetwork) {
		t.Fatalf("connection refused: got %v", err)
	}

	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Delay: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = newClient(t, s.URL()).Project(ctx, "group/app")
	if !errors.Is(err, forge.ErrNetwork) {
		t.Fatalf("deadline: got %v", err)
	}
}

func TestCanceledIsNotNetwork(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newClient(t, s.URL()).Project(ctx, "group/app")
	if !errors.Is(err, context.Canceled) || errors.Is(err, forge.ErrNetwork) {
		t.Fatalf("got %v, want context.Canceled only", err)
	}
}

func TestMalformedJSON(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Body: "{not json"})
	_, err := newClient(t, s.URL()).Project(context.Background(), "group/app")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, sentinel := range []error{forge.ErrNetwork, forge.ErrUnauthorized, forge.ErrForbidden, forge.ErrNotFound, forge.ErrInsecure} {
		if errors.Is(err, sentinel) {
			t.Fatalf("malformed JSON matched %v", sentinel)
		}
	}
}
