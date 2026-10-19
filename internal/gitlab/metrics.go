package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

// Branch listing limits (research R5).
const (
	branchesPerPage  = 100
	maxBranchPages   = 100
	maxPagesInFlight = 4
)

// ProjectClient fetches the metrics of one project; it implements status.Source.
type ProjectClient struct {
	c      *Client
	prefix string // /projects/<id>
}

// ForProject returns the metrics source of the project with this ID.
func (c *Client) ForProject(id int64) *ProjectClient {
	return &ProjectClient{c: c, prefix: "/projects/" + strconv.FormatInt(id, 10)}
}

type branch struct {
	Commit struct {
		CommittedDate time.Time `json:"committed_date"`
	} `json:"commit"`
}

// Branches counts all branches and those with a commit since activeSince. Pages after the
// first are fetched concurrently, at most 4 at a time.
func (p *ProjectClient) Branches(ctx context.Context, activeSince time.Time) (status.Branches, error) {
	var first []branch
	h, err := p.c.get(ctx, p.prefix+"/repository/branches", branchQuery(1), &first)
	if err != nil {
		return status.Branches{}, p.metricError(err, status.FieldBranches)
	}
	pages, err := strconv.Atoi(h.Get("X-Total-Pages"))
	if err != nil || pages > maxBranchPages {
		capped := status.Count{Capped: true}
		return status.Branches{Active: capped, Total: capped}, nil
	}

	all := make([][]branch, max(pages, 1))
	all[0] = first
	errs := make([]error, len(all))
	sem := make(chan struct{}, maxPagesInFlight)
	var wg sync.WaitGroup
	for page := 2; page <= pages; page++ {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			_, errs[page-1] = p.c.get(ctx, p.prefix+"/repository/branches", branchQuery(page), &all[page-1])
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return status.Branches{}, p.metricError(err, status.FieldBranches)
	}

	var b status.Branches
	for _, page := range all {
		for _, br := range page {
			b.Total.N++
			if !br.Commit.CommittedDate.Before(activeSince) {
				b.Active.N++
			}
		}
	}
	return b, nil
}

func branchQuery(page int) url.Values {
	return url.Values{"per_page": {strconv.Itoa(branchesPerPage)}, "page": {strconv.Itoa(page)}}
}

// Pipelines counts running and pending pipelines, concurrently.
func (p *ProjectClient) Pipelines(ctx context.Context) (status.Pipelines, error) {
	var (
		out        status.Pipelines
		errR, errP error
		wg         sync.WaitGroup
	)
	wg.Go(func() {
		out.Running, errR = p.count(ctx, "/pipelines", url.Values{"status": {"running"}})
	})
	wg.Go(func() {
		out.Pending, errP = p.count(ctx, "/pipelines", url.Values{"status": {"pending"}})
	})
	wg.Wait()
	if err := errors.Join(errR, errP); err != nil {
		return status.Pipelines{}, p.metricError(err, status.FieldPipelines)
	}
	return out, nil
}

// LastPipeline returns the latest pipeline on ref; Status is PipelineNone when there is
// none, or when ref is empty (a project without commits).
func (p *ProjectClient) LastPipeline(ctx context.Context, ref string) (status.LastPipeline, error) {
	none := status.LastPipeline{Status: status.PipelineNone, Ref: ref}
	if ref == "" {
		return none, nil
	}
	var list []struct {
		ID        int64     `json:"id"`
		Status    string    `json:"status"`
		Ref       string    `json:"ref"`
		UpdatedAt time.Time `json:"updated_at"`
		WebURL    string    `json:"web_url"`
	}
	q := url.Values{"ref": {ref}, "per_page": {"1"}, "order_by": {"id"}, "sort": {"desc"}}
	if _, err := p.c.get(ctx, p.prefix+"/pipelines", q, &list); err != nil {
		return status.LastPipeline{}, p.metricError(err, status.FieldLast)
	}
	if len(list) == 0 {
		return none, nil
	}
	l := list[0]
	return status.LastPipeline{ID: l.ID, Status: status.ParsePipelineStatus(l.Status), Ref: l.Ref, UpdatedAt: l.UpdatedAt, WebURL: l.WebURL}, nil
}

// OpenMRs counts open merge requests, drafts included.
func (p *ProjectClient) OpenMRs(ctx context.Context) (status.Count, error) {
	c, err := p.count(ctx, "/merge_requests", url.Values{"state": {"opened"}})
	if err != nil {
		return status.Count{}, p.metricError(err, status.FieldMRs)
	}
	return c, nil
}

// OpenIssues counts open issues from the issue statistics, which are never capped.
func (p *ProjectClient) OpenIssues(ctx context.Context) (status.Count, error) {
	var s struct {
		Statistics struct {
			Counts struct {
				Opened int `json:"opened"`
			} `json:"counts"`
		} `json:"statistics"`
	}
	if _, err := p.c.get(ctx, p.prefix+"/issues_statistics", nil, &s); err != nil {
		return status.Count{}, p.metricError(err, status.FieldIssues)
	}
	return status.Count{N: s.Statistics.Counts.Opened}, nil
}

// count reads X-Total from a one-item page. Without X-Total (more than 10,000 rows), a
// non-empty page means capped and an empty one means zero.
func (p *ProjectClient) count(ctx context.Context, path string, q url.Values) (status.Count, error) {
	q.Set("per_page", "1")
	var items []json.RawMessage
	h, err := p.c.get(ctx, p.prefix+path, q, &items)
	if err != nil {
		return status.Count{}, err
	}
	if n, err := strconv.Atoi(h.Get("X-Total")); err == nil {
		return status.Count{N: n}, nil
	}
	if len(items) == 0 {
		return status.Count{}, nil
	}
	return status.Count{Capped: true}, nil
}

// metricError turns 403/404 into status.ErrUnavailable (feature disabled or forbidden)
// and classifies the rest.
func (p *ProjectClient) metricError(err error, field status.FieldName) error {
	var se *statusError
	if errors.As(err, &se) && (se.code == http.StatusForbidden || se.code == http.StatusNotFound) {
		return fmt.Errorf("%s %w (%d): the feature may be disabled on this project", field, status.ErrUnavailable, se.code)
	}
	return p.c.httpError(err)
}
