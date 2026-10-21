package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/render"
	"github.com/sgaunet/vigie/internal/scan"
	"github.com/sgaunet/vigie/internal/status"
)

// runScan lists the clones below s.Dir: walk, inspect, select, fetch, then one table on
// stdout, and on stderr the summary and one line per host that rejected the token. When
// every row failed, stdout gets nothing and stderr says why each one failed.
func runScan(ctx context.Context, s config.Settings, std IO, log *slog.Logger) error {
	cands, skips, err := find(ctx, s)
	if err != nil {
		return err
	}
	inspected, more := scan.Inspect(ctx, cands, s.Remote, log)
	if err := interrupted(ctx); err != nil {
		return err
	}
	skips = append(skips, more...)
	repos, more := scan.Select(inspected, s)
	skips = append(skips, more...)
	for _, sk := range skips {
		attrs := []any{"dir", sk.Rel, "reason", sk.Reason.Text(s.Remote)}
		if sk.Err != nil {
			attrs = append(attrs, "err", sk.Err)
		}
		log.Debug("repository skipped", attrs...)
	}
	sum := scan.Summarize(len(repos), skips, s.Remote)
	if err := sum.Err(s.Depth); err != nil {
		return err
	}

	counter := newCounter(s, std)
	defer func() { _ = counter.Clear() }() // also on interrupt and errors
	_ = counter.Update(0, len(repos))
	rows := scan.Fetch(ctx, repos, scan.FetchConfig{
		Fields:     s.Fields,
		Timeout:    s.Timeout,
		ActiveDays: s.ActiveDays,
		Env:        std.Env,
		Log:        log,
		OnDone:     func(done, total int) { _ = counter.Update(done, total) },
	})
	if err := interrupted(ctx); err != nil {
		return err
	}
	_ = counter.Clear()
	for _, r := range rows {
		if !r.OK() {
			log.Debug("repository failed", "dir", r.Dir, "failure", r.Failure, "reason", r.Err)
		}
	}

	hosts, others := scan.Latched(rows)
	if err := scan.Outcome(rows, sum); err != nil {
		printHostFailures(std.Stderr, hosts)
		for _, r := range others {
			_, _ = fmt.Fprintf(std.Stderr, "%s: %v\n", r.Dir, r.Err)
		}
		return err // main prints it last
	}
	if err := printRows(std.Stdout, rows, s); err != nil {
		return err
	}
	if s.LogLevel != slog.LevelError {
		_, _ = fmt.Fprintln(std.Stderr, sum.Line())
	}
	printHostFailures(std.Stderr, hosts) // errors: shown even with --quiet
	return nil
}

// printRows writes the table, or one JSON record per row, in tree order.
func printRows(w io.Writer, rows []status.Row, s config.Settings) error {
	if s.Output == config.OutputJSON {
		for _, r := range rows {
			if err := render.JSONRow(w, r); err != nil {
				return err
			}
		}
		return nil
	}
	if _, err := io.WriteString(w, render.Table(rows, s.Fields, textStyle(s, w), time.Now())); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}

func printHostFailures(w io.Writer, hosts []scan.HostFailure) {
	for _, h := range hosts {
		_, _ = fmt.Fprintln(w, h.Line())
	}
}

// find runs the walk in a goroutine: a directory read blocked on a dead network mount
// cannot be interrupted, so Ctrl+C returns at once and the walk ends with the process.
func find(ctx context.Context, s config.Settings) ([]scan.Candidate, []scan.Skip, error) {
	type result struct {
		cands []scan.Candidate
		skips []scan.Skip
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		cands, skips, err := scan.Find(ctx, s.Dir, s.Depth)
		ch <- result{cands, skips, err}
	}()
	select {
	case r := <-ch:
		if err := interrupted(ctx); err != nil {
			return nil, nil, err
		}
		return r.cands, r.skips, r.err
	case <-ctx.Done():
		return nil, nil, interrupted(ctx)
	}
}

// newCounter returns the progress counter on stderr, or one writing nowhere when it must
// not show: stdout or stderr is not a terminal, or --quiet or --verbose is set.
func newCounter(s config.Settings, std IO) *render.Progress {
	_, outTTY := terminal(std.Stdout)
	fd, errTTY := terminal(std.Stderr)
	if !render.ShowProgress(outTTY, errTTY, s.LogLevel == slog.LevelError, s.LogLevel == slog.LevelDebug) {
		return render.NewProgress(io.Discard, false)
	}
	return render.NewProgress(std.Stderr, s.Color && render.EnableVT(fd))
}

// interrupted returns the error of a run stopped by a signal, or nil.
func interrupted(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("interrupted: %w", err)
	}
	return nil
}
