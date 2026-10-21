package scan

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/token"
)

// ReposPerHost bounds the repositories fetched at once from one GitLab instance. Each
// sends up to 9 requests at a time, so an instance sees at most about 36.
const ReposPerHost = 4

// FetchConfig is what every repository fetch shares.
type FetchConfig struct {
	Fields     []status.FieldName
	Timeout    time.Duration // per repository, from slot to last field, project lookup included
	ActiveDays int
	Env        map[string]string
	Log        *slog.Logger
	OnDone     func(done, total int) // called under a mutex as each repository ends; may be nil
}

// host is one GitLab instance: a distinct API base URL and token source.
type host struct {
	ep    config.Endpoint
	slots *slots
	log   *slog.Logger

	once    sync.Once
	client  *gitlab.Client
	initErr error

	mu      sync.Mutex
	latched error // the first rejected or unreadable token; later repositories fail with it
}

// hostID tells hosts apart: two remote names mapped to one API with one token share a host.
func hostID(ep config.Endpoint) string {
	return ep.API.String() + "\x00" + ep.Token.Env + "\x00" + strings.Join(ep.Token.Command, "\x00")
}

// connect resolves the token and builds the client, once per host, so a token_command
// runs at most once. The client retries throttled requests and slows the host down.
func (h *host) connect(ctx context.Context, env map[string]string) (*gitlab.Client, error) {
	h.once.Do(func() {
		tok, err := token.Resolve(ctx, h.ep.Token, env)
		if err != nil {
			h.initErr = err
			return
		}
		h.client, h.initErr = gitlab.NewClient(h.ep.API, tok, gitlab.Options{
			Insecure:      h.ep.Insecure,
			ExtraRootsPEM: h.ep.ExtraRootsPEM,
			Retry:         true,
			OnThrottle:    h.throttled,
		})
	})
	return h.client, h.initErr
}

// throttled halves the repositories in flight on the host; those already running finish.
func (h *host) throttled() {
	if limit, changed := h.slots.Halve(); changed {
		h.log.Debug("slowing down", "host", h.ep.API.Host, "limit", limit)
	}
}

// fail returns the error a repository fails with. A rejected or unreadable token latches
// the host: no further request is sent to it, and every one of its rows carries the first
// such error, so Latched can report the host once.
func (h *host) fail(err error) error {
	if !latches(err) {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.latched == nil {
		h.latched = err
	}
	return h.latched
}

func (h *host) latchedErr() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latched
}

// latches reports whether err stops every request to its host.
func latches(err error) bool {
	return errors.Is(err, gitlab.ErrUnauthorized) || errors.Is(err, token.ErrMissing) || errors.Is(err, token.ErrCommand)
}

// Fetch fetches every repository concurrently, at most ReposPerHost at a time per host, so
// a slow host never holds back another. A failing repository gets a failed row and never
// stops the others. Rows keep the input order.
func Fetch(ctx context.Context, repos []Repo, cfg FetchConfig) []status.Row {
	hosts := map[string]*host{}
	rows := make([]status.Row, len(repos))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
	)
	for i, r := range repos {
		var h *host
		if r.Err == nil {
			id := hostID(r.Endpoint)
			if h = hosts[id]; h == nil {
				h = &host{ep: r.Endpoint, slots: newSlots(ReposPerHost), log: cfg.Log}
				hosts[id] = h
			}
		}
		wg.Go(func() {
			rows[i] = fetchRepo(ctx, r, h, cfg)
			mu.Lock()
			defer mu.Unlock()
			done++
			if cfg.OnDone != nil {
				cfg.OnDone(done, len(repos))
			}
		})
	}
	wg.Wait()
	return rows
}

// fetchRepo looks the project up and collects its fields as single mode does, under one
// deadline that starts once a slot is free.
func fetchRepo(ctx context.Context, r Repo, h *host, cfg FetchConfig) status.Row {
	if r.Err != nil {
		return failedRow(r, cfg, r.Err)
	}
	if err := h.slots.Acquire(ctx); err != nil {
		return failedRow(r, cfg, err)
	}
	defer h.slots.Release()
	if err := h.latchedErr(); err != nil {
		return failedRow(r, cfg, err)
	}
	client, err := h.connect(ctx, cfg.Env)
	if err != nil {
		return failedRow(r, cfg, h.fail(err))
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	p, err := client.Project(ctx, r.Checkout.Remote.Path)
	if err != nil {
		return failedRow(r, cfg, h.fail(err))
	}
	branch := r.Checkout.Branch
	if branch == "" {
		branch = p.DefaultBranch
	}
	fields := status.Collect(ctx, client.ForProject(p.ID), status.Request{
		Fields:      cfg.Fields,
		Branch:      branch,
		ActiveSince: time.Now().AddDate(0, 0, -cfg.ActiveDays),
		Timeout:     cfg.Timeout,
	})
	if err := gitlab.RefreshError(fields); err != nil {
		return failedRow(r, cfg, h.fail(err))
	}
	logFields(cfg.Log, r.Rel, fields)
	return status.Row{Dir: r.Rel, Snapshot: status.Snapshot{
		Project:   status.Project{Path: p.Path, WebURL: p.WebURL, Archived: p.Archived},
		Branch:    branch,
		Fields:    fields,
		FetchedAt: time.Now(),
	}}
}

// failedRow is the row of a repository whose project could not be fetched: the project
// path from the remote, the checkout branch, and the requested fields without values.
func failedRow(r Repo, cfg FetchConfig, err error) status.Row {
	fields := make([]status.FieldResult, len(cfg.Fields))
	for i, name := range cfg.Fields {
		fields[i].Name = name
	}
	project := status.Project{Path: r.Checkout.Remote.Path}
	if r.Endpoint.API != nil {
		project.WebURL = strings.TrimSuffix(r.Endpoint.API.String(), "/") + "/" + project.Path
	}
	return status.Row{
		Dir:      r.Rel,
		Snapshot: status.Snapshot{Project: project, Branch: r.Checkout.Branch, Fields: fields, FetchedAt: time.Now()},
		Failure:  Classify(err),
		Err:      err,
	}
}

// logFields logs field errors at debug level: the table already shows "–", and a warning
// per row would flood stderr.
func logFields(log *slog.Logger, dir string, fields []status.FieldResult) {
	for _, f := range fields {
		switch {
		case f.Err == nil:
		case errors.Is(f.Err, status.ErrUnavailable):
			log.Debug("field unavailable", "dir", dir, "field", f.Name, "reason", f.Err)
		default:
			log.Debug("field failed", "dir", dir, "field", f.Name, "reason", f.Err)
		}
	}
}

var failures = []struct {
	err     error
	failure status.Failure
}{
	{token.ErrMissing, status.FailureAuth},
	{token.ErrCommand, status.FailureAuth},
	{gitlab.ErrUnauthorized, status.FailureAuth},
	{gitlab.ErrForbidden, status.FailureAuth},
	{gitlab.ErrProjectNotFound, status.FailureNotFound},
	{gitlab.ErrNetwork, status.FailureNetwork},
}

// Classify gives the failure class of a row error: "" for nil, FailureError for anything
// without a class of its own.
func Classify(err error) status.Failure {
	if err == nil {
		return ""
	}
	for _, f := range failures {
		if errors.Is(err, f.err) {
			return f.failure
		}
	}
	return status.FailureError
}
