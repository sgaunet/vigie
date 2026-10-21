package scan_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/github/githubtest"
	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/scan"
	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/token"
)

// fakeHost is a gitlabtest server acting as one GitLab instance, with its own token.
type fakeHost struct {
	srv  *gitlabtest.Server
	ep   config.Endpoint
	name string
	tok  string
	next int64 // next project ID
}

func newHost(t *testing.T, name string) *fakeHost {
	t.Helper()
	srv := gitlabtest.New(t, gitlabtest.Fixture(time.Now()))
	return &fakeHost{
		srv:  srv,
		ep:   config.Endpoint{API: mustURL(t, srv.URL()), Insecure: true, Token: token.Source{Env: "TOK_" + name}, Platform: status.PlatformGitLab},
		name: name,
		tok:  "glpat-" + name,
		next: 100,
	}
}

// repos adds n projects named <name>/rNN on the host and returns one repository per project.
func (h *fakeHost) repos(n int) []scan.Repo {
	out := make([]scan.Repo, 0, n)
	for range n {
		h.next++
		path := fmt.Sprintf("%s/r%d", h.name, h.next)
		p := gitlabtest.Fixture(time.Now())
		p.ID, p.Path = h.next, path
		h.srv.AddProject(p)
		out = append(out, scan.Repo{
			Rel:      path,
			Checkout: repo.Checkout{Branch: "main", Remote: repo.Remote{Host: h.name + ".test", Path: path}},
			Endpoint: h.ep,
		})
	}
	return out
}

func fetchConfig(hosts ...*fakeHost) scan.FetchConfig {
	env := map[string]string{}
	for _, h := range hosts {
		env["TOK_"+h.name] = h.tok
	}
	return scan.FetchConfig{
		Fields:     status.DefaultFields(),
		Timeout:    10 * time.Second,
		ActiveDays: 90,
		Env:        env,
		Log:        slog.New(slog.DiscardHandler),
	}
}

func fetch(repos []scan.Repo, cfg scan.FetchConfig) []status.Row {
	return scan.Fetch(context.Background(), repos, cfg)
}

func checkOK(t *testing.T, rows []status.Row) {
	t.Helper()
	for _, r := range rows {
		if !r.OK() || r.Err != nil {
			t.Errorf("%s: failure %q: %v", r.Dir, r.Failure, r.Err)
			continue
		}
		if f := r.Snapshot.Fields; len(f) != 5 || f[3].Count.N != 5 || f[4].Count.N != 17 || r.Snapshot.FetchedAt.IsZero() {
			t.Errorf("%s: snapshot = %+v", r.Dir, r.Snapshot)
		}
	}
}

func TestFetchPerHostBound(t *testing.T) {
	t.Parallel()
	a := newHost(t, "a")
	a.srv.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Delay: 50 * time.Millisecond})
	rows := fetch(a.repos(10), fetchConfig(a))
	checkOK(t, rows)
	if m := a.srv.MaxInFlight(gitlabtest.EndpointProject); m > scan.ReposPerHost || m < 2 || scan.ReposPerHost != 4 {
		t.Fatalf("project lookups in flight = %d, want 2..%d", m, scan.ReposPerHost)
	}
}

func TestFetchHostIsolationAndOrder(t *testing.T) {
	t.Parallel()
	a, b := newHost(t, "a"), newHost(t, "b")
	a.srv.SetAll(gitlabtest.Behavior{Delay: 300 * time.Millisecond})
	repos := append(a.repos(8), b.repos(1)...)
	var calls [][2]int
	var mu sync.Mutex
	cfg := fetchConfig(a, b)
	cfg.OnDone = func(done, total int) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, [2]int{done, total})
	}
	rows := fetch(repos, cfg)
	checkOK(t, rows)
	for i, r := range rows {
		if r.Dir != repos[i].Rel {
			t.Fatalf("row %d is %s, want %s", i, r.Dir, repos[i].Rel)
		}
	}
	fast := rows[8].Snapshot.FetchedAt
	for _, r := range rows[:8] {
		if !fast.Before(r.Snapshot.FetchedAt) {
			t.Fatalf("host b waited for host a: b at %v, %s at %v", fast, r.Dir, r.Snapshot.FetchedAt)
		}
	}
	if len(calls) != len(repos) {
		t.Fatalf("OnDone called %d times", len(calls))
	}
	for i, c := range calls {
		if c != [2]int{i + 1, len(repos)} {
			t.Fatalf("OnDone call %d = %v", i, c)
		}
	}
}

func TestFetchBranchAndToken(t *testing.T) {
	t.Parallel()
	a, b := newHost(t, "a"), newHost(t, "b")
	repos := append(a.repos(2), b.repos(1)...)
	repos[0].Checkout.Branch = "feature"
	repos[1].Checkout.Branch = ""
	rows := fetch(repos, fetchConfig(a, b))
	checkOK(t, rows)
	if rows[0].Snapshot.Branch != "feature" || rows[1].Snapshot.Branch != "main" {
		t.Fatalf("branches = %q, %q", rows[0].Snapshot.Branch, rows[1].Snapshot.Branch)
	}
	if l := rows[0].Snapshot.Fields[2].Last; l.Status != status.PipelineFailed || l.Ref != "feature" {
		t.Fatalf("last on feature = %+v", l)
	}
	refs := map[string]string{}
	for _, r := range a.srv.Requests() {
		if r.Token != a.tok {
			t.Fatalf("host a got token %q on %s", r.Token, r.Path)
		}
		if r.Endpoint == gitlabtest.EndpointLast {
			refs[r.Path] = r.Query.Get("ref")
		}
	}
	for _, r := range b.srv.Requests() {
		if r.Token != b.tok {
			t.Fatalf("host b got token %q on %s", r.Token, r.Path)
		}
	}
	want := []string{"feature", "main"}
	got := []string{refs["/api/v4/projects/101/pipelines"], refs["/api/v4/projects/102/pipelines"]}
	if !slices.Equal(got, want) {
		t.Fatalf("last refs = %v, want %v", got, want)
	}
}

func TestFetchTimeoutPerRepository(t *testing.T) {
	t.Parallel()
	a, b := newHost(t, "a"), newHost(t, "b")
	a.srv.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Delay: 2 * time.Second})
	repos := append(b.repos(1), append(a.repos(1), b.repos(1)...)...)
	cfg := fetchConfig(a, b)
	cfg.Timeout = 200 * time.Millisecond
	start := time.Now()
	rows := fetch(repos, cfg)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Fetch took %v", d)
	}
	checkOK(t, []status.Row{rows[0], rows[2]})
	if rows[1].Failure != status.FailureNetwork {
		t.Fatalf("slow row = %q: %v", rows[1].Failure, rows[1].Err)
	}
}

func TestFetchFailedRows(t *testing.T) {
	t.Parallel()
	a := newHost(t, "a")
	repos := a.repos(1)
	broken := scan.Repo{Rel: "broken", Checkout: repo.Checkout{Branch: "main", Remote: repo.Remote{Host: "plain.test", Path: "group/broken"}},
		Err: fmt.Errorf("%w http://plain.test: set insecure", config.ErrInsecureHost)}
	missing := scan.Repo{Rel: "missing", Checkout: repo.Checkout{Branch: "dev", Remote: repo.Remote{Host: "a.test", Path: "group/missing"}}, Endpoint: a.ep}
	repos = append(repos, broken, missing)
	start := time.Now()
	rows := fetch(repos, fetchConfig(a))
	checkOK(t, rows[:1])

	if r := rows[1]; r.Failure != status.FailureError || !errors.Is(r.Err, broken.Err) || r.Dir != "broken" {
		t.Fatalf("broken = %+v", r)
	}
	for _, req := range a.srv.Requests() {
		if strings.Contains(req.Path, url.PathEscape("group/broken")) {
			t.Fatalf("request for a repository that has no endpoint: %s", req.Path)
		}
	}

	r := rows[2]
	if r.Failure != status.FailureNotFound || r.Snapshot.Project.Path != "group/missing" ||
		r.Snapshot.Project.WebURL != a.srv.URL()+"/group/missing" || r.Snapshot.Branch != "dev" || r.Snapshot.FetchedAt.Before(start) {
		t.Fatalf("missing = %+v", r)
	}
	var names []status.FieldName
	for _, f := range r.Snapshot.Fields {
		if f.Err != nil || f.Count != (status.Count{}) {
			t.Fatalf("failed row field has a value: %+v", f)
		}
		names = append(names, f.Name)
	}
	if !slices.Equal(names, status.DefaultFields()) {
		t.Fatalf("failed row fields = %v", names)
	}
}

func TestFetchTokenCommandOncePerHost(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	a := newHost(t, "a")
	counter := filepath.Join(t.TempDir(), "runs")
	a.ep.Token = token.Source{Command: []string{"sh", "-c", "echo run >> '" + counter + "'; echo glpat-fromcmd"}}
	rows := fetch(a.repos(3), fetchConfig(a))
	checkOK(t, rows)
	b, err := os.ReadFile(counter)
	if err != nil || strings.Count(string(b), "run\n") != 1 {
		t.Fatalf("token command runs = %q, %v", b, err)
	}
	for _, r := range a.srv.Requests() {
		if r.Token != "glpat-fromcmd" {
			t.Fatalf("request %s used token %q", r.Path, r.Token)
		}
	}
}

func TestFetchUnavailableFieldIsDebugOnly(t *testing.T) {
	t.Parallel()
	a := newHost(t, "a")
	a.srv.Set(gitlabtest.EndpointIssues, gitlabtest.Behavior{Status: http.StatusForbidden})
	var logs bytes.Buffer
	cfg := fetchConfig(a)
	cfg.Log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	rows := fetch(a.repos(2), cfg)
	for _, r := range rows {
		if !r.OK() || !errors.Is(r.Snapshot.Fields[4].Err, status.ErrUnavailable) {
			t.Fatalf("%s: %+v", r.Dir, r)
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("warnings logged: %q", logs.String())
	}
}

func TestFetchLatchesRejectedHost(t *testing.T) {
	t.Parallel()
	a, b, c, d := newHost(t, "a"), newHost(t, "b"), newHost(t, "c"), newHost(t, "d")
	a.srv.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusUnauthorized})
	c.srv.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusForbidden})
	missing := scan.Repo{Rel: "missing", Checkout: repo.Checkout{Branch: "main", Remote: repo.Remote{Host: "b.test", Path: "group/missing"}}, Endpoint: b.ep}
	repos := append(a.repos(10), b.repos(1)...)
	repos = append(repos, missing)
	repos = append(repos, c.repos(1)...)
	repos = append(repos, d.repos(2)...)
	cfg := fetchConfig(a, b, c) // no token for d
	rows := fetch(repos, cfg)

	for _, r := range rows[:10] {
		// Each 401 answer makes its own error value, and none wraps another: errors.Is
		// holds only for the one value the latch shares.
		if r.Failure != status.FailureAuth || !errors.Is(r.Err, rows[0].Err) {
			t.Fatalf("%s: %q %v, want the latched error %v", r.Dir, r.Failure, r.Err, rows[0].Err)
		}
	}
	if n := countEndpoint(a.srv, gitlabtest.EndpointProject); n > scan.ReposPerHost {
		t.Fatalf("host a received %d project lookups after rejecting the token", n)
	}
	checkOK(t, rows[10:11])
	if rows[11].Failure != status.FailureNotFound || rows[12].Failure != status.FailureAuth {
		t.Fatalf("missing %q, forbidden %q", rows[11].Failure, rows[12].Failure)
	}
	if !errors.Is(rows[13].Err, token.ErrMissing) || !errors.Is(rows[14].Err, rows[13].Err) || len(d.srv.Requests()) != 0 {
		t.Fatalf("host d: %v / %v, %d requests", rows[13].Err, rows[14].Err, len(d.srv.Requests()))
	}

	hosts, others := scan.Latched(rows)
	if len(hosts) != 2 || hosts[0] != (scan.HostFailure{Err: rows[0].Err, Rows: 10}) || hosts[1] != (scan.HostFailure{Err: rows[13].Err, Rows: 2}) {
		t.Fatalf("Latched hosts = %+v", hosts)
	}
	if len(others) != 2 || others[0].Dir != "missing" || others[1].Dir != rows[12].Dir {
		t.Fatalf("Latched others = %+v", others)
	}
}

func countEndpoint(s *gitlabtest.Server, e gitlabtest.Endpoint) int {
	n := 0
	for _, r := range s.Requests() {
		if r.Endpoint == e {
			n++
		}
	}
	return n
}

func TestFetchForbiddenProjectIsPerRow(t *testing.T) {
	t.Parallel()
	a := newHost(t, "a")
	a.srv.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusForbidden, Times: 1})
	rows := fetch(a.repos(6), fetchConfig(a))
	var failedRows, ok int
	for _, r := range rows {
		switch {
		case r.OK():
			ok++
		case r.Failure == status.FailureAuth && errors.Is(r.Err, forge.ErrForbidden):
			failedRows++
		default:
			t.Fatalf("%s: %q %v", r.Dir, r.Failure, r.Err)
		}
	}
	if failedRows != 1 || ok != 5 {
		t.Fatalf("%d forbidden, %d OK", failedRows, ok)
	}
	if hosts, others := scan.Latched(rows); len(hosts) != 0 || len(others) != 1 {
		t.Fatalf("a 403 latched the host: %+v %+v", hosts, others)
	}
}

func TestFetchThrottledHostSlowsDown(t *testing.T) {
	t.Parallel()
	a := newHost(t, "a")
	a.srv.Set(gitlabtest.EndpointProject, gitlabtest.Behavior{Status: http.StatusTooManyRequests, Times: 2})
	var logs syncBuffer
	cfg := fetchConfig(a)
	cfg.Log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rows := fetch(a.repos(6), cfg)
	checkOK(t, rows)
	var slow []string
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, `msg="slowing down"`) {
			slow = append(slow, l)
		}
	}
	host := strings.TrimPrefix(a.srv.URL(), "http://")
	if len(slow) != 1 || !strings.Contains(slow[0], "host="+host) || !strings.Contains(slow[0], "limit=2") {
		t.Fatalf("slow-down records = %q", slow)
	}
}

// syncBuffer is a bytes.Buffer safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// gitHubHost is a githubtest server acting as an Enterprise Server, with the TOK_H token.
func gitHubHost(t *testing.T) (*githubtest.Server, config.Endpoint) {
	t.Helper()
	srv := githubtest.New(t, githubtest.Fixture(time.Now()))
	return srv, config.Endpoint{API: mustURL(t, srv.URL()), Insecure: true, Platform: status.PlatformGitHub,
		Token: token.Source{Env: "TOK_H", Vars: token.EnterpriseVars, Host: "ghe.test"}}
}

func gitHubRepo(ep config.Endpoint, rel, path string) scan.Repo {
	return scan.Repo{Rel: rel, Checkout: repo.Checkout{Branch: "main", Remote: repo.Remote{Host: "ghe.test", Path: path}}, Endpoint: ep}
}

func TestFetchGitHubHost(t *testing.T) {
	t.Parallel()
	a := newHost(t, "a")
	gh, ep := gitHubHost(t)
	repos := append(a.repos(1), gitHubRepo(ep, "gh/app", "octo/app"), gitHubRepo(ep, "gh/gone", "octo/gone"))
	broken := gitHubRepo(config.Endpoint{Platform: status.PlatformGitHub}, "gh/broken", "octo/broken")
	broken.Err = fmt.Errorf("%w http://ghe.lan: set insecure", config.ErrInsecureHost)
	repos = append(repos, broken)
	cfg := fetchConfig(a)
	cfg.Env["TOK_H"] = "ghp_hostToken"
	rows := fetch(repos, cfg)

	checkOK(t, rows[:1])
	if p := rows[0].Snapshot.Project.Platform; p != status.PlatformGitLab {
		t.Errorf("GitLab row platform = %q", p)
	}
	r := rows[1]
	if !r.OK() || r.Snapshot.Project.Platform != status.PlatformGitHub || r.Snapshot.Project.Path != "octo/app" {
		t.Fatalf("GitHub row = %+v", r)
	}
	if f := r.Snapshot.Fields; f[0].Branches.Total.N != 5 || f[3].Count.N != 5 || f[4].Count.N != 12 || f[2].Last.Status != status.PipelineFailed {
		t.Errorf("GitHub row values = %+v", f)
	}
	if r := rows[2]; r.Failure != status.FailureNotFound || r.Snapshot.Project.Platform != status.PlatformGitHub ||
		r.Snapshot.Project.WebURL != gh.URL()+"/octo/gone" {
		t.Errorf("missing GitHub row = %+v", r)
	}
	if r := rows[3]; r.Failure != status.FailureError || r.Snapshot.Project.Platform != status.PlatformGitHub {
		t.Errorf("broken GitHub row = %+v", r)
	}

	if got := gh.Tokens(); len(got) != 1 || got[0] != "Bearer ghp_hostToken" {
		t.Errorf("githubtest tokens = %q", got)
	}
	for _, req := range a.srv.Requests() {
		if req.Token != a.tok {
			t.Errorf("gitlabtest got token %q", req.Token)
		}
	}
}

func TestFetchHostsByPlatform(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	a := newHost(t, "a")
	counter := filepath.Join(t.TempDir(), "runs")
	a.ep.Token = token.Source{Command: []string{"sh", "-c", "echo run >> '" + counter + "'; echo tok"}}
	gl := a.repos(1)
	gh := gl[0]
	gh.Rel, gh.Endpoint.Platform = "gh", status.PlatformGitHub
	_ = fetch(append(gl, gh), fetchConfig(a))
	b, err := os.ReadFile(counter)
	if err != nil || strings.Count(string(b), "run\n") != 2 {
		t.Fatalf("token command runs = %q, %v: one API URL on two platforms is two hosts", b, err)
	}
}

func TestFetchGitHubRetry(t *testing.T) {
	t.Parallel()
	gh, ep := gitHubHost(t)
	gh.Set(githubtest.EndpointRepo, githubtest.Behavior{Status: http.StatusBadGateway, Times: 1})
	cfg := fetchConfig()
	cfg.Env["TOK_H"] = "ghp_hostToken"
	rows := fetch([]scan.Repo{gitHubRepo(ep, "gh/app", "octo/app")}, cfg)
	if !rows[0].OK() {
		t.Fatalf("row = %+v", rows[0])
	}
}

// gitHubRepos adds n repositories octo/rN to the githubtest host and returns them.
func gitHubRepos(gh *githubtest.Server, ep config.Endpoint, n int) []scan.Repo {
	var out []scan.Repo
	for i := range n {
		r := githubtest.Fixture(time.Now())
		r.ID, r.FullName = int64(100+i), fmt.Sprintf("octo/r%d", i)
		gh.AddRepo(r)
		out = append(out, gitHubRepo(ep, r.FullName, r.FullName))
	}
	return out
}

func TestFetchLatchesExhaustedAllowance(t *testing.T) {
	t.Parallel()
	gh, ep := gitHubHost(t)
	gh.SetAll(githubtest.Behavior{RateLimited: true, Reset: time.Now().Add(13 * time.Minute)})
	cfg := fetchConfig()
	cfg.Env["TOK_H"] = "ghp_hostToken"
	rows := fetch(gitHubRepos(gh, ep, 6), cfg)
	for _, r := range rows {
		if r.Failure != status.FailureNetwork || !errors.Is(r.Err, forge.ErrRateLimit) || !errors.Is(r.Err, rows[0].Err) {
			t.Fatalf("%s: %q %v, want the latched limit %v", r.Dir, r.Failure, r.Err, rows[0].Err)
		}
	}
	if n := len(gh.Requests()); n > scan.ReposPerHost {
		t.Fatalf("the host received %d requests after exhausting the allowance", n)
	}
	hosts, others := scan.Latched(rows)
	if len(hosts) != 1 || hosts[0].Rows != 6 || !strings.HasSuffix(hosts[0].Line(), "(6 repositories)") || len(others) != 0 {
		t.Fatalf("Latched = %+v, %+v", hosts, others)
	}
}

func TestFetchSecondaryLimitDoesNotLatch(t *testing.T) {
	t.Parallel()
	gh, ep := gitHubHost(t)
	gh.Set(githubtest.EndpointRepo, githubtest.Behavior{Secondary: true, Header: map[string]string{"Retry-After": "1"}, Times: 1})
	cfg := fetchConfig()
	cfg.Env["TOK_H"] = "ghp_hostToken"
	rows := fetch(gitHubRepos(gh, ep, 3), cfg)
	for _, r := range rows {
		if !r.OK() {
			t.Fatalf("%s: %q %v", r.Dir, r.Failure, r.Err)
		}
	}
	if hosts, _ := scan.Latched(rows); len(hosts) != 0 {
		t.Fatalf("a secondary limit latched the host: %+v", hosts)
	}
}
