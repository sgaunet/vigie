package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"time"

	"golang.org/x/term"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/render"
	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/status"
	"github.com/sgaunet/vigie/internal/token"
	"github.com/sgaunet/vigie/internal/watch"
)

// IO is the process environment handed to Run.
type IO struct {
	Stdout io.Writer
	Stderr io.Writer
	Env    map[string]string
	FS     fs.FS // root filesystem for the config file; see config.Load
}

// Run executes the status command: resolve the project, collect the fields, print them.
func Run(ctx context.Context, ov config.Overrides, std IO) error {
	s, err := config.Load(ov, std.Env, std.FS)
	if err != nil {
		return err
	}
	log := NewLogger(std.Stderr, s.LogLevel)
	if s.ConfigPath != "" {
		log.Debug("config file loaded", "path", s.ConfigPath)
	}
	r, err := newRunner(ctx, s, std.Env, log)
	if err != nil {
		return err
	}
	t, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	if s.Watch {
		return r.watch(ctx, t, std)
	}
	return r.once(ctx, t, std.Stdout)
}

// runner holds what one invocation needs to talk to GitLab.
type runner struct {
	s      config.Settings
	log    *slog.Logger
	client *gitlab.Client
	ref    string // project path or numeric ID
	detect bool   // the project comes from the git remote, so the branch does too
}

// target is a resolved project with its reporting branch.
type target struct {
	project status.Project
	branch  string
	source  status.Source
}

func newRunner(ctx context.Context, s config.Settings, env map[string]string, log *slog.Logger) (*runner, error) {
	var co repo.Checkout
	if s.Project == "" || s.Host == nil {
		c, err := repo.Detect(ctx, s.Dir, s.Remote, log)
		switch {
		case err == nil:
			co = c
		case s.Project != "":
			log.Debug("no remote detected, using --project", "reason", err)
		default:
			return nil, err
		}
	}
	ep, err := config.ResolveHost(s, co.Remote.Host)
	if err != nil {
		return nil, err
	}
	tok, err := token.Resolve(ctx, ep.Token, env)
	if err != nil {
		return nil, err
	}
	log.Debug("gitlab endpoint", "url", ep.API.String(), "insecure", ep.Insecure, "token", tok)
	client, err := gitlab.NewClient(ep.API, tok, gitlab.Options{Insecure: ep.Insecure, ExtraRootsPEM: ep.ExtraRootsPEM})
	if err != nil {
		return nil, err
	}
	r := &runner{s: s, log: log, client: client, ref: s.Project}
	if r.ref == "" {
		r.ref, r.detect = co.Remote.Path, true
	}
	return r, nil
}

// resolve looks up the project and picks the reporting branch: the current branch, or the
// default branch when HEAD is detached or the project was given with --project.
func (r *runner) resolve(ctx context.Context) (target, error) {
	p, err := r.client.Project(ctx, r.ref)
	if err != nil {
		return target{}, err
	}
	branch := p.DefaultBranch
	if r.detect {
		if co, err := repo.Detect(ctx, r.s.Dir, r.s.Remote, r.log); err == nil && co.Branch != "" {
			branch = co.Branch
		}
	}
	r.log.Debug("project resolved", "project", p.Path, "id", p.ID, "branch", branch)
	return target{
		project: status.Project{Path: p.Path, WebURL: p.WebURL, Archived: p.Archived},
		branch:  branch,
		source:  r.client.ForProject(p.ID),
	}, nil
}

func (r *runner) request(branch string, now time.Time) status.Request {
	return status.Request{
		Fields:      r.s.Fields,
		Branch:      branch,
		ActiveSince: now.AddDate(0, 0, -r.s.ActiveDays),
		Timeout:     r.s.Timeout,
	}
}

// once runs a single refresh and prints one record.
func (r *runner) once(ctx context.Context, t target, stdout io.Writer) error {
	now := time.Now()
	fields := status.Collect(ctx, t.source, r.request(t.branch, now))
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("interrupted: %w", err)
	}
	if err := r.fatal(fields); err != nil {
		return err
	}
	r.logFields(fields, slog.LevelWarn)
	snap := status.Snapshot{Project: t.project, Branch: t.branch, Fields: fields, FetchedAt: time.Now()}
	if r.s.Output == config.OutputJSON {
		return render.JSON(stdout, snap)
	}
	style := textStyle(r.s, stdout)
	if _, err := fmt.Fprintln(stdout, render.Text(snap, style, time.Now())); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}

// watch redraws until interrupted (nil, exit 0) or a fatal error. Network failures never
// end it; they mark the values stale.
func (r *runner) watch(ctx context.Context, t target, std IO) error {
	d := r.display(std)
	defer func() { _ = d.Close() }() // also runs while a panic unwinds, restoring the cursor
	err := watch.Run(ctx, watch.Config{
		Interval:   r.s.Interval,
		Fields:     r.s.Fields,
		Timeout:    r.s.Timeout,
		ActiveDays: r.s.ActiveDays,
		Target:     watch.Target{Project: t.project, Branch: t.branch, Source: t.source},
		Resolve: func(ctx context.Context) (watch.Target, error) {
			t, err := r.resolve(ctx)
			if err != nil {
				r.log.Debug("project refresh failed", "reason", err)
			}
			return watch.Target{Project: t.project, Branch: t.branch, Source: t.source}, err
		},
		Draw: func(s status.Snapshot, inFlight bool) error {
			if !inFlight {
				// Debug only: the line shows "–" and the stale marker, and warnings
				// on stderr would break the line redrawn in place.
				r.logFields(s.Fields, slog.LevelDebug)
			}
			return d.Draw(s, inFlight)
		},
		Resize:     watch.ResizeSignal(ctx),
		Fatal:      fatalInWatch,
		RetryAfter: gitlab.RetryAfter,
	})
	if err != nil {
		_ = d.Close() // end the status line before main prints the error
	}
	return err
}

// display builds the watch display for the output format and the terminal state.
func (r *runner) display(std IO) *render.Display {
	if r.s.Output == config.OutputJSON {
		return render.NewDisplay(std.Stdout, std.Stderr, render.ModeJSON, render.Style{})
	}
	fd, tty := terminal(std.Stdout)
	if !tty {
		return render.NewDisplay(std.Stdout, std.Stderr, render.ModeLine, render.Style{})
	}
	st := textStyle(r.s, std.Stdout)
	_, st.InFlight = terminal(std.Stderr)
	d := render.NewDisplay(std.Stdout, std.Stderr, render.ModeTTY, st)
	d.SetWidth(func() int {
		w, _, err := term.GetSize(int(fd))
		if err != nil {
			return 0
		}
		return w
	})
	return d
}

// fatalInWatch tells which errors stop watch mode instead of marking values stale.
func fatalInWatch(err error) bool {
	return errors.Is(err, gitlab.ErrUnauthorized) || errors.Is(err, gitlab.ErrForbidden) ||
		errors.Is(err, gitlab.ErrProjectNotFound)
}

// logFields logs field errors: unavailable fields at debug level, the rest at level.
func (r *runner) logFields(fields []status.FieldResult, level slog.Level) {
	ctx := context.Background()
	for _, f := range fields {
		switch {
		case f.Err == nil:
		case errors.Is(f.Err, status.ErrUnavailable):
			r.log.Debug("field unavailable", "field", f.Name, "reason", f.Err)
		default:
			r.log.Log(ctx, level, "field failed", "field", f.Name, "reason", f.Err)
		}
	}
}

// fatal logs field errors and returns the error that makes the whole run fail: a rejected
// token, or a network error on every field (nothing usable was fetched).
func (r *runner) fatal(fields []status.FieldResult) error {
	allNetwork := len(fields) > 0
	var first error
	for _, f := range fields {
		switch {
		case f.Err == nil:
			allNetwork = false
			continue
		case errors.Is(f.Err, gitlab.ErrUnauthorized):
			return f.Err
		}
		if !errors.Is(f.Err, gitlab.ErrNetwork) {
			allNetwork = false
		}
		if first == nil {
			first = f.Err
		}
	}
	if allNetwork {
		return first
	}
	return nil
}

// textStyle enables color and icons only on a terminal (FR-022).
func textStyle(s config.Settings, stdout io.Writer) render.Style {
	fd, tty := terminal(stdout)
	if !tty {
		return render.Style{}
	}
	st := render.Style{Color: s.Color && render.EnableVT(fd), Emoji: s.Emoji}
	if w, _, err := term.GetSize(int(fd)); err == nil {
		st.Width = w
	}
	return st
}

// terminal reports whether w is a terminal, and its file descriptor.
func terminal(w io.Writer) (uintptr, bool) {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	fd := f.Fd()
	return fd, term.IsTerminal(int(fd))
}
