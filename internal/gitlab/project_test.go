package gitlab_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
	"github.com/sgaunet/vigie/internal/token"
)

const tokenValue = "glpat-t3st"

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
	c, err := gitlab.NewClient(u, token.New(tokenValue), gitlab.Options{Insecure: true})
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
	want := gitlab.Project{ID: 42, Path: "group/app", WebURL: s.URL() + "/group/app", DefaultBranch: "main"}
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

func TestProjectBasePathPrefix(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	// A base URL with a path (GitLab under /gitlab) keeps it in front of /api/v4.
	_, err := newClient(t, s.URL()+"/gitlab").Project(context.Background(), "group/app")
	if !errors.Is(err, gitlab.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
	if got := s.Requests()[0].Path; got != "/gitlab/api/v4/projects/group%2Fapp" {
		t.Fatalf("path = %q", got)
	}
}

func TestProjectHTTPErrors(t *testing.T) {
	t.Parallel()
	cases := map[int]error{
		http.StatusUnauthorized:        gitlab.ErrUnauthorized,
		http.StatusForbidden:           gitlab.ErrForbidden,
		http.StatusNotFound:            gitlab.ErrProjectNotFound,
		http.StatusTooManyRequests:     gitlab.ErrNetwork,
		http.StatusBadGateway:          gitlab.ErrNetwork,
		http.StatusInternalServerError: gitlab.ErrNetwork,
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
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusTooManyRequests, RetryAfter: "30"})
	_, err := newClient(t, s.URL()).Project(context.Background(), "group/app")
	if d, ok := gitlab.RetryAfter(err); !ok || d != 30*time.Second {
		t.Fatalf("RetryAfter = %v %v (err %v)", d, ok, err)
	}
	date := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusServiceUnavailable, RetryAfter: date})
	_, err = newClient(t, s.URL()).Project(context.Background(), "group/app")
	if d, ok := gitlab.RetryAfter(err); !ok || d < 80*time.Second || d > 91*time.Second {
		t.Fatalf("RetryAfter date = %v %v", d, ok)
	}
	if _, ok := gitlab.RetryAfter(errors.New("x")); ok {
		t.Fatal("RetryAfter on a plain error")
	}
}

func TestNewClientRefusesPlainHTTP(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("http://gitlab.lan")
	if _, err := gitlab.NewClient(u, token.New(tokenValue), gitlab.Options{}); !errors.Is(err, gitlab.ErrInsecure) {
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
	if !errors.Is(err, gitlab.ErrNetwork) {
		t.Fatalf("connection refused: got %v", err)
	}

	s := newServer(t)
	s.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Delay: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = newClient(t, s.URL()).Project(ctx, "group/app")
	if !errors.Is(err, gitlab.ErrNetwork) {
		t.Fatalf("deadline: got %v", err)
	}
}

func TestCanceledIsNotNetwork(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newClient(t, s.URL()).Project(ctx, "group/app")
	if !errors.Is(err, context.Canceled) || errors.Is(err, gitlab.ErrNetwork) {
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
	for _, sentinel := range []error{gitlab.ErrNetwork, gitlab.ErrUnauthorized, gitlab.ErrForbidden, gitlab.ErrProjectNotFound, gitlab.ErrInsecure} {
		if errors.Is(err, sentinel) {
			t.Fatalf("malformed JSON matched %v", sentinel)
		}
	}
}
