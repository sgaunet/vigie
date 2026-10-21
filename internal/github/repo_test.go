package github_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/github/githubtest"
)

// hostOf returns host:port of the fake server, the host vigie names in its messages.
func hostOf(s *githubtest.Server) string { return strings.TrimPrefix(s.URL(), "http://") }

func TestProjectErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ref  string
		b    githubtest.Behavior
		want error
		text string // with <host> for the server's host
	}{
		{"401", "octo/app", githubtest.Behavior{Status: http.StatusUnauthorized}, forge.ErrUnauthorized,
			"token rejected by <host> (401): check that the token is valid and not expired"},
		{"403 SSO", "octo/app", githubtest.Behavior{Status: http.StatusForbidden, Header: map[string]string{"X-GitHub-SSO": "required; url=https://x"}}, forge.ErrForbidden,
			"access to repository octo/app denied (403): authorize the token for the organization's SAML single sign-on"},
		{"403 permission", "octo/app", githubtest.Behavior{Status: http.StatusForbidden, Header: map[string]string{"X-Accepted-GitHub-Permissions": "actions=read"}}, forge.ErrForbidden,
			`access to repository octo/app denied (403): the token lacks the permission "actions=read"`},
		{"403", "octo/app", githubtest.Behavior{Status: http.StatusForbidden}, forge.ErrForbidden,
			"access to repository octo/app denied (403)"},
		{"404", "octo/gone", githubtest.Behavior{}, forge.ErrNotFound,
			"repository octo/gone not found on <host>: check that the token can see it (for a fine-grained token, add the repository to it; for an organization with single sign-on, authorize the token)"},
		{"429", "octo/app", githubtest.Behavior{Status: http.StatusTooManyRequests}, forge.ErrNetwork, ""},
		{"500", "octo/app", githubtest.Behavior{Status: http.StatusInternalServerError}, forge.ErrNetwork, ""},
		{"502", "octo/app", githubtest.Behavior{Status: http.StatusBadGateway}, forge.ErrNetwork, ""},
	}
	for _, c := range cases {
		s := newServer(t)
		s.Set(githubtest.EndpointRepo, c.b)
		_, err := newClient(t, s.URL()).Project(context.Background(), c.ref)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
			continue
		}
		if want := strings.ReplaceAll(c.text, "<host>", hostOf(s)); c.text != "" && err.Error() != want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, err, want)
		}
		if strings.Contains(err.Error(), tokenValue) {
			t.Errorf("%s: the message holds the token: %q", c.name, err)
		}
	}
}

func TestProjectMalformed(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{Body: "{not json"})
	_, err := newClient(t, s.URL()).Project(context.Background(), "octo/app")
	if err == nil || !strings.HasPrefix(err.Error(), "malformed response from") {
		t.Fatalf("got %v", err)
	}
	for _, sentinel := range []error{forge.ErrNetwork, forge.ErrUnauthorized, forge.ErrForbidden, forge.ErrNotFound, forge.ErrInsecure} {
		if errors.Is(err, sentinel) {
			t.Fatalf("malformed JSON matched %v", sentinel)
		}
	}
}

func TestProjectRenamed(t *testing.T) {
	t.Parallel()
	r := githubtest.Fixture(time.Now())
	r.MovedFrom = "octo/old"
	s := githubtest.New(t, r)
	p, err := newClient(t, s.URL()).Project(context.Background(), "octo/old")
	if err != nil || p.Path != "octo/app" || p.ID != 42 {
		t.Fatalf("Project = %+v, %v", p, err)
	}
}

func TestProjectRedirectAcrossHosts(t *testing.T) {
	t.Parallel()
	other := newServer(t)
	s := newServer(t)
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{Status: http.StatusFound, Header: map[string]string{"Location": other.URL() + "/api/v3/repos/octo/app"}})
	if _, err := newClient(t, s.URL()).Project(context.Background(), "octo/app"); err == nil {
		t.Fatal("expected an error")
	}
	if n := len(other.Requests()); n != 0 {
		t.Fatalf("the other host received %d requests", n)
	}
}

func TestProjectCanceledIsNotNetwork(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newClient(t, s.URL()).Project(ctx, "octo/app")
	if !errors.Is(err, context.Canceled) || errors.Is(err, forge.ErrNetwork) {
		t.Fatalf("got %v, want context.Canceled only", err)
	}
}

func TestProjectDeadline(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.Set(githubtest.EndpointRepo, githubtest.Behavior{Delay: 2 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := newClient(t, s.URL()).Project(ctx, "octo/app")
	if !errors.Is(err, forge.ErrNetwork) || !strings.Contains(err.Error(), "raise --timeout") {
		t.Fatalf("got %v", err)
	}
}
