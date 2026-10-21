package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/status"
)

// Listing limits (research G5, G6).
const (
	branchesPerPage = 100    // first: 100 in branchesQuery
	maxBranchPages  = 100    // beyond 10,000 branches, both counts are capped
	maxCount        = 10_000 // a larger GraphQL count shows as capped
	maxRunCount     = 1_000  // GitHub caps filtered run listings at 1,000
	runsPerPage     = 30     // runs of the branch read for the last field
	commitRuns      = 100    // runs of one commit read when the first page holds no other
)

// errInexact is a run count GitHub cannot give exactly: shown "–" with its reason.
var errInexact = &unavailableError{msg: "more than 1,000 workflow runs: GitHub does not report an exact count"}

// unavailableError is a status.ErrUnavailable whose message reads on its own.
type unavailableError struct{ msg string }

func (e *unavailableError) Error() string { return e.msg }
func (e *unavailableError) Unwrap() error { return status.ErrUnavailable }

// repoSource fetches the five fields of one repository; it implements status.Source.
type repoSource struct {
	c *Client
	p forge.Project
}

// vars returns the GraphQL variables naming the repository, plus more.
func (s *repoSource) vars(more map[string]any) map[string]any {
	owner, name, _ := strings.Cut(s.p.Path, "/")
	v := map[string]any{"owner": owner, "name": name}
	for k, x := range more {
		v[k] = x
	}
	return v
}

// Branches counts all branches and those whose head commit is from activeSince on, 100 per
// GraphQL page.
func (s *repoSource) Branches(ctx context.Context, activeSince time.Time) (status.Branches, error) {
	var (
		b      status.Branches
		cursor any // null for the first page
	)
	for page := 1; ; page++ {
		var data struct {
			Repository *struct {
				Refs struct {
					TotalCount int `json:"totalCount"`
					PageInfo   struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						Target struct {
							CommittedDate time.Time `json:"committedDate"`
						} `json:"target"`
					} `json:"nodes"`
				} `json:"refs"`
			} `json:"repository"`
		}
		if err := s.c.query(ctx, "Branches", branchesQuery, s.vars(map[string]any{"cursor": cursor}), &data); err != nil {
			return status.Branches{}, s.fieldError(status.FieldBranches, err)
		}
		if data.Repository == nil {
			return status.Branches{}, s.notFound(status.FieldBranches)
		}
		refs := data.Repository.Refs
		if refs.TotalCount > maxBranchPages*branchesPerPage || (refs.PageInfo.HasNextPage && page == maxBranchPages) {
			capped := status.Count{Capped: true}
			return status.Branches{Active: capped, Total: capped}, nil
		}
		b.Total.N = refs.TotalCount
		for _, n := range refs.Nodes {
			if d := n.Target.CommittedDate; !d.IsZero() && !d.Before(activeSince) {
				b.Active.N++
			}
		}
		if !refs.PageInfo.HasNextPage {
			return b, nil
		}
		cursor = refs.PageInfo.EndCursor
	}
}

// Pipelines counts running and pending workflow runs, one request per state, concurrently.
// Runs waiting for an approval are not counted.
func (s *repoSource) Pipelines(ctx context.Context) (status.Pipelines, error) {
	states := []string{"in_progress", "queued", "requested", "pending"}
	counts := make([]int, len(states))
	errs := make([]error, len(states))
	var wg sync.WaitGroup
	for i, st := range states {
		wg.Go(func() {
			var list struct {
				TotalCount int `json:"total_count"`
			}
			_, errs[i] = s.c.get(ctx, s.runsPath(), url.Values{"status": {st}, "per_page": {"1"}}, &list)
			counts[i] = list.TotalCount
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return status.Pipelines{}, s.fieldError(status.FieldPipelines, err)
		}
	}
	for _, n := range counts {
		if n >= maxRunCount {
			return status.Pipelines{}, errInexact
		}
	}
	return status.Pipelines{Running: status.Count{N: counts[0]}, Pending: status.Count{N: counts[1] + counts[2] + counts[3]}}, nil
}

// LastPipeline combines the workflow runs of the latest commit of ref that has any (research
// G6). Status is PipelineNone when ref has no run, or is empty (a repository without
// commits).
func (s *repoSource) LastPipeline(ctx context.Context, ref string) (status.LastPipeline, error) {
	none := status.LastPipeline{Status: status.PipelineNone, Ref: ref}
	if ref == "" {
		return none, nil
	}
	runs, err := s.runs(ctx, url.Values{"branch": {ref}, "per_page": {fmt.Sprint(runsPerPage)}})
	if err != nil {
		return status.LastPipeline{}, s.fieldError(status.FieldLast, err)
	}
	if len(runs) == 0 {
		return none, nil
	}
	sha := newest(runs).HeadSHA
	if len(runs) == runsPerPage && sameCommit(runs, sha) {
		// The page may not hold every run of that commit.
		runs, err = s.runs(ctx, url.Values{"branch": {ref}, "head_sha": {sha}, "per_page": {fmt.Sprint(commitRuns)}})
		if err != nil {
			return status.LastPipeline{}, s.fieldError(status.FieldLast, err)
		}
	}
	return combine(runs, ref, s.p.WebURL+"/commit/"+sha), nil
}

func sameCommit(runs []run, sha string) bool {
	for _, r := range runs {
		if r.HeadSHA != sha {
			return false
		}
	}
	return true
}

func (s *repoSource) runsPath() string { return "/repos/" + escapePath(s.p.Path) + "/actions/runs" }

func (s *repoSource) runs(ctx context.Context, q url.Values) ([]run, error) {
	var list struct {
		WorkflowRuns []run `json:"workflow_runs"`
	}
	if _, err := s.c.get(ctx, s.runsPath(), q, &list); err != nil {
		return nil, err
	}
	return list.WorkflowRuns, nil
}

// OpenMRs counts open pull requests, drafts included.
func (s *repoSource) OpenMRs(ctx context.Context) (status.Count, error) {
	var data struct {
		Repository *struct {
			PullRequests struct {
				TotalCount int `json:"totalCount"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	if err := s.c.query(ctx, "PullRequests", pullRequestsQuery, s.vars(nil), &data); err != nil {
		return status.Count{}, s.fieldError(status.FieldMRs, err)
	}
	if data.Repository == nil {
		return status.Count{}, s.notFound(status.FieldMRs)
	}
	return count(data.Repository.PullRequests.TotalCount), nil
}

// OpenIssues counts open issues; pull requests are another type, so they are not counted.
func (s *repoSource) OpenIssues(ctx context.Context) (status.Count, error) {
	var data struct {
		Repository *struct {
			HasIssuesEnabled bool `json:"hasIssuesEnabled"`
			Issues           struct {
				TotalCount int `json:"totalCount"`
			} `json:"issues"`
		} `json:"repository"`
	}
	if err := s.c.query(ctx, "Issues", issuesQuery, s.vars(nil), &data); err != nil {
		return status.Count{}, s.fieldError(status.FieldIssues, err)
	}
	switch {
	case data.Repository == nil:
		return status.Count{}, s.notFound(status.FieldIssues)
	case !data.Repository.HasIssuesEnabled:
		return status.Count{}, fmt.Errorf("%s %w: issues are disabled on this repository", status.FieldIssues, status.ErrUnavailable)
	}
	return count(data.Repository.Issues.TotalCount), nil
}

func count(n int) status.Count {
	if n > maxCount {
		return status.Count{Capped: true}
	}
	return status.Count{N: n}
}

// fieldError turns a refused field (403 or 404, GraphQL NOT_FOUND or FORBIDDEN) into
// status.ErrUnavailable, naming a missing permission when GitHub does, and classifies the
// rest.
func (s *repoSource) fieldError(field status.FieldName, err error) error {
	var (
		se *statusError
		ge *graphQLError
	)
	switch {
	case errors.As(err, &se) && (se.code == http.StatusForbidden || se.code == http.StatusNotFound):
		reason := "the feature may be disabled on this repository"
		if perm := se.header.Get("X-Accepted-GitHub-Permissions"); perm != "" {
			reason = fmt.Sprintf("the token lacks the permission %q", perm)
		}
		return fmt.Errorf("%s %w (%d): %s", field, status.ErrUnavailable, se.code, reason)
	case errors.As(err, &ge) && (ge.Type == "NOT_FOUND" || ge.Type == "FORBIDDEN"):
		return fmt.Errorf("%s %w: %s", field, status.ErrUnavailable, ge.Message)
	case errors.As(err, &ge):
		return fmt.Errorf("unexpected answer from %s: %w", s.c.host, ge)
	}
	return s.c.httpError(err)
}

func (s *repoSource) notFound(field status.FieldName) error {
	return fmt.Errorf("%s %w: GitHub did not resolve repository %s", field, status.ErrUnavailable, s.p.Path)
}
