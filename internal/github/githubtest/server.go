// Package githubtest provides a fake GitHub server for tests, in the spirit of
// net/http/httptest: the REST API under /api/v3 and the GraphQL API at /api/graphql, the
// GitHub Enterprise Server layout. GraphQL requests are dispatched on their operationName.
package githubtest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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

// Run is one workflow run; Repo.Runs is ordered newest first.
type Run struct {
	ID, WorkflowID int64
	HeadBranch     string
	HeadSHA        string
	Event          string
	Status         string // queued, in_progress, completed, …
	Conclusion     string // "" until completed
	UpdatedAt      time.Time
}

// Repo is the state the server exposes for one repository.
type Repo struct {
	ID            int64
	FullName      string // owner/repo
	DefaultBranch string // answered as "" when the repository has no branch
	Archived      bool
	HasIssues     bool
	Branches      []Branch
	Runs          []Run
	OpenPRs       int
	OpenIssues    int
	MovedFrom     string // an old full name, answered with a 301 to the repository
}

// Endpoint names one API call the server answers.
type Endpoint string

// Endpoints.
const (
	EndpointRepo         Endpoint = "repo"         // GET /repos/{owner}/{repo}, GET /repositories/{id}
	EndpointRuns         Endpoint = "runs"         // GET /repos/{owner}/{repo}/actions/runs
	EndpointBranches     Endpoint = "branches"     // GraphQL Branches
	EndpointPullRequests Endpoint = "pullrequests" // GraphQL PullRequests
	EndpointIssues       Endpoint = "issues"       // GraphQL Issues
	endpointUnknown      Endpoint = "unknown"
)

var endpoints = []Endpoint{EndpointRepo, EndpointRuns, EndpointBranches, EndpointPullRequests, EndpointIssues}

// Behavior changes how one endpoint answers. The zero value answers normally.
type Behavior struct {
	Status      int               // answer with this HTTP status and a {"message": …} body
	Header      map[string]string // extra response headers, e.g. X-GitHub-SSO or Retry-After
	Body        string            // raw body instead of the generated one (status 200 unless Status is set)
	Delay       time.Duration     // wait before answering (or until the client gives up)
	Times       int               // apply to the first Times requests of the endpoint only; 0 = always
	RateLimited bool              // the primary allowance is exhausted until Reset
	Reset       time.Time         // with RateLimited: when the allowance resets
	Secondary   bool              // a secondary rate limit: 403 and its message (add Retry-After with Header)
}

// Pool is one allowance pool of GitHub: REST requests or GraphQL points.
type Pool string

// The two pools.
const (
	PoolCore    Pool = "core"
	PoolGraphQL Pool = "graphql"
)

// secondaryMessage is the body GitHub sends with a secondary rate limit.
const secondaryMessage = "You have exceeded a secondary rate limit. Please wait a few minutes before you try again."

// Request is one recorded request.
type Request struct {
	Endpoint      Endpoint
	Method        string
	Path          string // escaped path, e.g. /api/v3/repos/octo/app
	Query         string // raw query
	Operation     string // GraphQL operationName
	Authorization string
	IfNoneMatch   string
}

// Server is a fake GitHub instance.
type Server struct {
	srv *httptest.Server

	mu          sync.Mutex
	repos       []Repo // the first one is given to New
	behavior    map[Endpoint]Behavior
	applied     map[Endpoint]int // requests that got a Behavior with Times set
	requests    []Request
	tokens      []string
	inFlight    map[Endpoint]int
	maxInFlight map[Endpoint]int
	billed      map[Pool]int
}

// New starts a server exposing r; it stops when the test ends.
func New(t testing.TB, r Repo) *Server {
	t.Helper()
	s := &Server{
		repos:       []Repo{r},
		behavior:    map[Endpoint]Behavior{},
		applied:     map[Endpoint]int{},
		inFlight:    map[Endpoint]int{},
		maxInFlight: map[Endpoint]int{},
		billed:      map[Pool]int{},
	}
	s.srv = httptest.NewServer(s.Handler())
	t.Cleanup(s.srv.Close)
	return s
}

// URL returns the base URL, e.g. http://127.0.0.1:1234: the web address of an Enterprise
// Server.
func (s *Server) URL() string { return s.srv.URL }

// Handler returns the server's handler, to serve it without a socket.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }

// AddRepo makes the server also answer for r.
func (s *Server) AddRepo(r Repo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos = append(s.repos, r)
}

// SetRepo replaces the repository with r's ID.
func (s *Server) SetRepo(r Repo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.repos {
		if s.repos[i].ID == r.ID {
			s.repos[i] = r
		}
	}
}

// Set changes the behaviour of one endpoint, and restarts its Times count.
func (s *Server) Set(e Endpoint, b Behavior) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.behavior[e] = b
	s.applied[e] = 0
}

// SetAll gives every endpoint the same behaviour.
func (s *Server) SetAll(b Behavior) {
	for _, e := range endpoints {
		s.Set(e, b)
	}
}

// Requests returns the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// Tokens returns the distinct Authorization header values received, in order.
func (s *Server) Tokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.tokens)
}

// Billed returns what GitHub would charge pool so far: every REST answer but a 304, and
// every GraphQL query.
func (s *Server) Billed(p Pool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.billed[p]
}

// MaxInFlight returns the highest number of concurrent requests seen on e.
func (s *Server) MaxInFlight(e Endpoint) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxInFlight[e]
}

// Fixture returns the default repository, octo/app (ID 42), with dates relative to now. Its
// line reads br 3/5 ci 1r/2p last:fail main 3m mr 5 is 12.
func Fixture(now time.Time) Repo {
	sha := func(prefix string) string { return prefix + strings.Repeat("0", 40-len(prefix)) }
	return Repo{
		ID: 42, FullName: "octo/app", DefaultBranch: "main", HasIssues: true,
		Branches: []Branch{
			{"main", now.Add(-time.Hour)},
			{"feature", now.AddDate(0, 0, -2)},
			{"docs", now.AddDate(0, 0, -10)},
			{"old1", now.AddDate(0, 0, -200)},
			{"old2", now.AddDate(0, 0, -400)},
		},
		Runs: []Run{
			{110, 2, "main", sha("c0ffee"), "push", "completed", "failure", now.Add(-3 * time.Minute)},
			{109, 1, "main", sha("c0ffee"), "push", "completed", "success", now.Add(-5 * time.Minute)},
			{108, 3, "feature", sha("feed"), "pull_request", "in_progress", "", now.Add(-time.Minute)},
			{107, 3, "feature", sha("beef"), "push", "queued", "", now.Add(-time.Minute)},
			{106, 3, "docs", sha("d0c5"), "push", "pending", "", now.Add(-time.Minute)},
			{105, 4, "docs", sha("d0c5"), "push", "waiting", "", now.Add(-time.Minute)},
			{100, 1, "main", sha("dead"), "push", "completed", "success", now.Add(-2 * time.Hour)},
		},
		OpenPRs:    5,
		OpenIssues: 12,
	}
}

// graphQLRequest is the body of a GraphQL POST.
type graphQLRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	var (
		e  Endpoint
		gq graphQLRequest
	)
	switch {
	case path == "/api/graphql" && r.Method == http.MethodPost:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = json.Unmarshal(body, &gq)
		e = operationEndpoint(gq.OperationName)
	case strings.HasPrefix(path, "/api/v3/") && r.Method == http.MethodGet:
		_, rest, _ := s.splitRepoPath(strings.TrimPrefix(path, "/api/v3"))
		e = restEndpoint(rest)
	default:
		e = endpointUnknown
	}

	pool := PoolCore
	if path == "/api/graphql" {
		pool = PoolGraphQL
	}
	sw := &statusWriter{ResponseWriter: w}
	w = sw
	defer func() {
		if sw.code != http.StatusNotModified {
			s.mu.Lock()
			s.billed[pool]++
			s.mu.Unlock()
		}
	}()

	s.mu.Lock()
	auth := r.Header.Get("Authorization")
	s.requests = append(s.requests, Request{Endpoint: e, Method: r.Method, Path: path, Query: r.URL.RawQuery, Operation: gq.OperationName,
		Authorization: auth, IfNoneMatch: r.Header.Get("If-None-Match")})
	if !slices.Contains(s.tokens, auth) {
		s.tokens = append(s.tokens, auth)
	}
	s.inFlight[e]++
	s.maxInFlight[e] = max(s.maxInFlight[e], s.inFlight[e])
	b := s.behavior[e]
	if b.Times > 0 {
		if s.applied[e] < b.Times {
			s.applied[e]++
		} else {
			b = Behavior{}
		}
	}
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
	for k, v := range b.Header {
		w.Header().Set(k, v)
	}
	switch {
	case b.RateLimited:
		rateHeaders(w, pool, 0, b.Reset)
		if pool == PoolGraphQL {
			writeJSON(w, http.StatusOK, map[string]any{"data": nil, "errors": []map[string]string{{"type": "RATE_LIMITED", "message": "API rate limit exceeded"}}})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "API rate limit exceeded for user ID 1."})
	case b.Secondary:
		writeJSON(w, http.StatusForbidden, map[string]string{"message": secondaryMessage})
	case b.Status != 0 && b.Body != "":
		writeRaw(w, b.Status, b.Body)
	case b.Status != 0:
		writeJSON(w, b.Status, map[string]string{"message": http.StatusText(b.Status)})
	case b.Body != "":
		writeRaw(w, http.StatusOK, b.Body)
	case e == endpointUnknown:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unknown request"})
	case e == EndpointRepo || e == EndpointRuns:
		rateHeaders(w, pool, 4999, time.Now().Add(time.Hour))
		s.serveREST(w, r, e)
	default:
		rateHeaders(w, pool, 4999, time.Now().Add(time.Hour))
		s.serveGraphQL(w, e, gq)
	}
}

// statusWriter records the status code of an answer.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// rateHeaders sets the allowance headers GitHub sends with every answer.
func rateHeaders(w http.ResponseWriter, p Pool, remaining int, reset time.Time) {
	w.Header().Set("X-Ratelimit-Remaining", strconv.Itoa(remaining))
	w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(reset.Unix(), 10))
	w.Header().Set("X-Ratelimit-Resource", string(p))
}

// writeREST answers v with an ETag, or 304 when the request already holds it.
func writeREST(w http.ResponseWriter, r *http.Request, v any) {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(v)
	sum := sha256.Sum256(buf.Bytes())
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeRaw(w, http.StatusOK, buf.String())
}

func operationEndpoint(op string) Endpoint {
	switch op {
	case "Branches":
		return EndpointBranches
	case "PullRequests":
		return EndpointPullRequests
	case "Issues":
		return EndpointIssues
	}
	return endpointUnknown
}

func restEndpoint(rest string) Endpoint {
	switch rest {
	case "":
		return EndpointRepo
	case "/actions/runs":
		return EndpointRuns
	}
	return endpointUnknown
}

// splitRepoPath splits /repos/{owner}/{repo}<rest> or /repositories/{id}<rest> into the
// repository reference (full name or ID) and the rest.
func (s *Server) splitRepoPath(p string) (string, string, bool) {
	switch {
	case strings.HasPrefix(p, "/repos/"):
		parts := strings.SplitN(strings.TrimPrefix(p, "/repos/"), "/", 3)
		if len(parts) < 2 {
			return "", "", false
		}
		rest := ""
		if len(parts) == 3 {
			rest = "/" + parts[2]
		}
		owner, err1 := url.PathUnescape(parts[0])
		name, err2 := url.PathUnescape(parts[1])
		return owner + "/" + name, rest, err1 == nil && err2 == nil
	case strings.HasPrefix(p, "/repositories/"):
		id, rest, _ := strings.Cut(strings.TrimPrefix(p, "/repositories/"), "/")
		if rest != "" {
			rest = "/" + rest
		}
		return id, rest, true
	}
	return "", "", false
}

// find returns the repository whose full name, old full name or ID is ref, and whether ref
// is its old name.
func (s *Server) find(ref string) (Repo, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		switch {
		case strings.EqualFold(ref, r.FullName) || ref == strconv.FormatInt(r.ID, 10):
			return r, false, true
		case r.MovedFrom != "" && strings.EqualFold(ref, r.MovedFrom):
			return r, true, true
		}
	}
	return Repo{}, false, false
}

func (s *Server) serveREST(w http.ResponseWriter, r *http.Request, e Endpoint) {
	ref, rest, _ := s.splitRepoPath(strings.TrimPrefix(r.URL.EscapedPath(), "/api/v3"))
	repo, moved, ok := s.find(ref)
	switch {
	case !ok:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	case moved:
		loc := fmt.Sprintf("%s/api/v3/repositories/%d%s", s.srv.URL, repo.ID, rest)
		if r.URL.RawQuery != "" {
			loc += "?" + r.URL.RawQuery
		}
		w.Header().Set("Location", loc)
		writeJSON(w, http.StatusMovedPermanently, map[string]string{"message": "Moved Permanently", "url": loc})
		return
	}
	if e == EndpointRepo {
		def := repo.DefaultBranch
		if len(repo.Branches) == 0 {
			def = ""
		}
		writeREST(w, r, map[string]any{
			"id": repo.ID, "full_name": repo.FullName, "html_url": s.srv.URL + "/" + repo.FullName,
			"default_branch": def, "archived": repo.Archived, "has_issues": repo.HasIssues,
		})
		return
	}
	writeREST(w, r, runsPage(repo.Runs, r.URL.Query()))
}

// runsPage filters runs as GitHub does: status (one value, a status or a conclusion; a
// comma list matches nothing), branch and head_sha, then pages them.
func runsPage(runs []Run, q url.Values) map[string]any {
	var match []Run
	for _, run := range runs {
		if st := q.Get("status"); st != "" && st != run.Status && st != run.Conclusion {
			continue
		}
		if b := q.Get("branch"); b != "" && b != run.HeadBranch {
			continue
		}
		if sha := q.Get("head_sha"); sha != "" && sha != run.HeadSHA {
			continue
		}
		match = append(match, run)
	}
	perPage := min(100, max(1, atoi(q.Get("per_page"), 30)))
	page := max(1, atoi(q.Get("page"), 1))
	out := []map[string]any{}
	for i := (page - 1) * perPage; i < min(page*perPage, len(match)); i++ {
		run := match[i]
		var conclusion any
		if run.Conclusion != "" {
			conclusion = run.Conclusion
		}
		out = append(out, map[string]any{
			"id": run.ID, "workflow_id": run.WorkflowID, "head_branch": run.HeadBranch, "head_sha": run.HeadSHA,
			"event": run.Event, "status": run.Status, "conclusion": conclusion,
			"updated_at": run.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return map[string]any{"total_count": len(match), "workflow_runs": out}
}

// branchesPerPage is the page size of the Branches query (first: 100).
const branchesPerPage = 100

func (s *Server) serveGraphQL(w http.ResponseWriter, e Endpoint, gq graphQLRequest) {
	owner, _ := gq.Variables["owner"].(string)
	name, _ := gq.Variables["name"].(string)
	repo, _, ok := s.find(owner + "/" + name)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"repository": nil},
			"errors": []map[string]any{{"type": "NOT_FOUND", "path": []string{"repository"},
				"message": fmt.Sprintf("Could not resolve to a Repository with the name '%s/%s'.", owner, name)}},
		})
		return
	}
	var data map[string]any
	switch e {
	case EndpointBranches:
		cursor, _ := gq.Variables["cursor"].(string)
		from := 0
		if c, found := strings.CutPrefix(cursor, "cursor:"); found {
			from = atoi(c, 0)
		}
		to := min(from+branchesPerPage, len(repo.Branches))
		nodes := []map[string]any{}
		for _, b := range repo.Branches[min(from, to):to] {
			nodes = append(nodes, map[string]any{"name": b.Name, "target": map[string]any{"committedDate": b.CommittedDate.UTC().Format(time.RFC3339)}})
		}
		data = map[string]any{"refs": map[string]any{
			"totalCount": len(repo.Branches),
			"pageInfo":   map[string]any{"hasNextPage": to < len(repo.Branches), "endCursor": "cursor:" + strconv.Itoa(to)},
			"nodes":      nodes,
		}}
	case EndpointPullRequests:
		data = map[string]any{"pullRequests": map[string]any{"totalCount": repo.OpenPRs}}
	case EndpointIssues:
		data = map[string]any{"hasIssuesEnabled": repo.HasIssues, "issues": map[string]any{"totalCount": repo.OpenIssues}}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": data}})
}

func atoi(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(v)
	writeRaw(w, code, buf.String())
}

func writeRaw(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}
