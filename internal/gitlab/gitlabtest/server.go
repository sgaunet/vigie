// Package gitlabtest provides a fake GitLab REST API v4 server for tests, in the spirit
// of net/http/httptest.
package gitlabtest

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Branch is one repository branch.
type Branch struct {
	Name          string
	CommittedDate time.Time
}

// Pipeline is one pipeline; Project.Pipelines is ordered newest first.
type Pipeline struct {
	ID        int64
	Ref       string
	Status    string
	UpdatedAt time.Time
}

// Project is the state the server exposes.
type Project struct {
	ID            int64
	Path          string
	DefaultBranch string
	Archived      bool
	Branches      []Branch
	Running       int
	Pending       int
	Pipelines     []Pipeline
	OpenMRs       int
	OpenIssues    int
}

// Endpoint names one API call the server answers.
type Endpoint string

// Endpoints.
const (
	EndpointProject  Endpoint = "project"   // GET /projects/:id
	EndpointBranches Endpoint = "branches"  // GET /projects/:id/repository/branches
	EndpointRunning  Endpoint = "running"   // GET /projects/:id/pipelines?status=running
	EndpointPending  Endpoint = "pending"   // GET /projects/:id/pipelines?status=pending
	EndpointLast     Endpoint = "last"      // GET /projects/:id/pipelines?ref=…
	EndpointMRs      Endpoint = "mrs"       // GET /projects/:id/merge_requests
	EndpointIssues   Endpoint = "issues"    // GET /projects/:id/issues_statistics
	endpointUnknown  Endpoint = "unknown"
)

// Behavior changes how one endpoint answers. The zero value answers normally.
type Behavior struct {
	Status       int           // answer with this HTTP status and an error body
	NoTotal      bool          // omit X-Total and X-Total-Pages
	NoTotalPages bool          // omit X-Total-Pages only
	RetryAfter   string        // Retry-After header value
	Delay        time.Duration // wait before answering (or until the client gives up)
	Body         string        // raw body instead of the generated one
	Redirect     string        // answer 302 with this Location
}

// Request is one recorded request.
type Request struct {
	Endpoint Endpoint
	Path     string // escaped path, e.g. /api/v4/projects/group%2Fapp
	Query    url.Values
	Token    string // PRIVATE-TOKEN header
}

// Server is a fake GitLab instance.
type Server struct {
	srv *httptest.Server

	mu          sync.Mutex
	project     Project
	behavior    map[Endpoint]Behavior
	requests    []Request
	inFlight    map[Endpoint]int
	maxInFlight map[Endpoint]int
}

// New starts a server exposing p; it stops when the test ends.
func New(t testing.TB, p Project) *Server {
	t.Helper()
	s := &Server{
		project:     p,
		behavior:    map[Endpoint]Behavior{},
		inFlight:    map[Endpoint]int{},
		maxInFlight: map[Endpoint]int{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

// URL returns the base URL, e.g. http://127.0.0.1:1234.
func (s *Server) URL() string { return s.srv.URL }

// SetProject replaces the exposed state.
func (s *Server) SetProject(p Project) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.project = p
}

// Set changes the behaviour of one endpoint.
func (s *Server) Set(e Endpoint, b Behavior) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.behavior[e] = b
}

// SetAll gives every endpoint the same behaviour.
func (s *Server) SetAll(b Behavior) {
	for _, e := range []Endpoint{EndpointProject, EndpointBranches, EndpointRunning, EndpointPending, EndpointLast, EndpointMRs, EndpointIssues} {
		s.Set(e, b)
	}
}

// Requests returns the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// MaxInFlight returns the highest number of concurrent requests seen on e.
func (s *Server) MaxInFlight(e Endpoint) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxInFlight[e]
}

//go:embed testdata/project.json
var fixtureJSON []byte

// Fixture returns the default project (group/app, id 42), with dates relative to now.
func Fixture(now time.Time) Project {
	var f struct {
		ID            int64  `json:"id"`
		Path          string `json:"path_with_namespace"`
		DefaultBranch string `json:"default_branch"`
		Archived      bool   `json:"archived"`
		Branches      []struct {
			Name    string `json:"name"`
			DaysAgo int    `json:"days_ago"`
		} `json:"branches"`
		Running   int `json:"running"`
		Pending   int `json:"pending"`
		Pipelines []struct {
			ID         int64  `json:"id"`
			Ref        string `json:"ref"`
			Status     string `json:"status"`
			MinutesAgo int    `json:"minutes_ago"`
		} `json:"pipelines"`
		OpenMRs    int `json:"open_mrs"`
		OpenIssues int `json:"open_issues"`
	}
	if err := json.Unmarshal(fixtureJSON, &f); err != nil {
		panic(err)
	}
	p := Project{ID: f.ID, Path: f.Path, DefaultBranch: f.DefaultBranch, Archived: f.Archived,
		Running: f.Running, Pending: f.Pending, OpenMRs: f.OpenMRs, OpenIssues: f.OpenIssues}
	for _, b := range f.Branches {
		p.Branches = append(p.Branches, Branch{Name: b.Name, CommittedDate: now.AddDate(0, 0, -b.DaysAgo)})
	}
	for _, pl := range f.Pipelines {
		p.Pipelines = append(p.Pipelines, Pipeline{ID: pl.ID, Ref: pl.Ref, Status: pl.Status,
			UpdatedAt: now.Add(-time.Duration(pl.MinutesAgo) * time.Minute)})
	}
	return p
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	q := r.URL.Query()
	id, rest := splitProjectPath(path)
	e := endpointOf(rest, q)

	s.mu.Lock()
	s.requests = append(s.requests, Request{Endpoint: e, Path: path, Query: q, Token: r.Header.Get("PRIVATE-TOKEN")})
	s.inFlight[e]++
	s.maxInFlight[e] = max(s.maxInFlight[e], s.inFlight[e])
	b := s.behavior[e]
	p := s.project
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight[e]--
		s.mu.Unlock()
	}()

	if b.Delay > 0 {
		select {
		case <-time.After(b.Delay):
		case <-r.Context().Done():
			return
		}
	}
	if b.RetryAfter != "" {
		w.Header().Set("Retry-After", b.RetryAfter)
	}
	switch {
	case b.Redirect != "":
		http.Redirect(w, r, b.Redirect, http.StatusFound)
		return
	case b.Status != 0:
		writeJSON(w, b.Status, map[string]string{"message": http.StatusText(b.Status)})
		return
	case e == endpointUnknown || !p.matches(id):
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "404 Project Not Found"})
		return
	}

	total, body := s.answer(e, p, q)
	if b.Body != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(b.Body))
		return
	}
	if total >= 0 && !b.NoTotal {
		perPage := max(1, atoi(q.Get("per_page"), 20))
		w.Header().Set("X-Total", strconv.Itoa(total))
		if !b.NoTotalPages {
			w.Header().Set("X-Total-Pages", strconv.Itoa(max(1, (total+perPage-1)/perPage)))
		}
	}
	writeJSON(w, http.StatusOK, body)
}

// answer returns the X-Total value (-1 for none) and the body for a known endpoint.
func (s *Server) answer(e Endpoint, p Project, q url.Values) (int, any) {
	perPage := max(1, atoi(q.Get("per_page"), 20))
	page := max(1, atoi(q.Get("page"), 1))
	switch e {
	case EndpointProject:
		return -1, map[string]any{
			"id": p.ID, "path_with_namespace": p.Path, "default_branch": p.DefaultBranch,
			"archived": p.Archived, "web_url": s.srv.URL + "/" + p.Path,
		}
	case EndpointBranches:
		var out []map[string]any
		for i := (page - 1) * perPage; i < min(page*perPage, len(p.Branches)); i++ {
			b := p.Branches[i]
			out = append(out, map[string]any{"name": b.Name, "commit": map[string]any{"committed_date": b.CommittedDate.Format(time.RFC3339)}})
		}
		return len(p.Branches), nonNil(out)
	case EndpointRunning:
		return p.Running, items(min(p.Running, perPage))
	case EndpointPending:
		return p.Pending, items(min(p.Pending, perPage))
	case EndpointLast:
		var out []map[string]any
		for _, pl := range p.Pipelines {
			if pl.Ref == q.Get("ref") && len(out) < perPage {
				out = append(out, map[string]any{
					"id": pl.ID, "ref": pl.Ref, "status": pl.Status,
					"updated_at": pl.UpdatedAt.Format(time.RFC3339),
					"web_url":    fmt.Sprintf("%s/%s/-/pipelines/%d", s.srv.URL, p.Path, pl.ID),
				})
			}
		}
		return len(out), nonNil(out)
	case EndpointMRs:
		return p.OpenMRs, items(min(p.OpenMRs, perPage))
	case EndpointIssues:
		return -1, map[string]any{"statistics": map[string]any{"counts": map[string]int{
			"all": p.OpenIssues, "closed": 0, "opened": p.OpenIssues,
		}}}
	}
	return -1, nil
}

func (p Project) matches(id string) bool {
	return id == p.Path || id == strconv.FormatInt(p.ID, 10)
}

// splitProjectPath splits /api/v4/projects/<id><rest> into the decoded id and rest.
func splitProjectPath(path string) (string, string) {
	const prefix = "/api/v4/projects/"
	if !strings.HasPrefix(path, prefix) {
		return "", "?"
	}
	path = strings.TrimPrefix(path, prefix)
	id, rest, _ := strings.Cut(path, "/")
	if rest != "" {
		rest = "/" + rest
	}
	dec, err := url.PathUnescape(id)
	if err != nil {
		return "", "?"
	}
	return dec, rest
}

func endpointOf(rest string, q url.Values) Endpoint {
	switch rest {
	case "":
		return EndpointProject
	case "/repository/branches":
		return EndpointBranches
	case "/merge_requests":
		return EndpointMRs
	case "/issues_statistics":
		return EndpointIssues
	case "/pipelines":
		switch {
		case q.Get("status") == "running":
			return EndpointRunning
		case q.Get("status") == "pending":
			return EndpointPending
		case q.Has("ref"):
			return EndpointLast
		}
	}
	return endpointUnknown
}

func items(n int) []map[string]int {
	out := make([]map[string]int, n)
	for i := range out {
		out[i] = map[string]int{"id": i + 1}
	}
	return out
}

func nonNil(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}

func atoi(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
