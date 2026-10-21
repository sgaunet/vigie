package scan

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/status"
)

// Reason tells why a repository gets no row.
type Reason string

// Skip reasons, in the order the summary lists them.
const (
	ReasonBare        Reason = "bare"
	ReasonUnreadable  Reason = "unreadable"
	ReasonNoRemote    Reason = "no remote"
	ReasonRemoteURL   Reason = "remote URL"
	ReasonUnknownHost Reason = "unknown host"
	ReasonOtherHost   Reason = "other host"
)

var reasonOrder = []Reason{ReasonBare, ReasonUnreadable, ReasonNoRemote, ReasonRemoteURL, ReasonUnknownHost, ReasonOtherHost}

// Text is the reason as printed; remote is the --remote name.
func (r Reason) Text(remote string) string {
	switch r {
	case ReasonNoRemote:
		return "no remote " + strconv.Quote(remote)
	case ReasonRemoteURL:
		return "remote URL not understood"
	}
	return string(r)
}

// Skip is a repository, or a directory that may hold one, left out of the table. Err is
// only for debug logs.
type Skip struct {
	Rel    string
	Reason Reason
	Err    error
}

// Summary counts what a scan found.
type Summary struct {
	Found   int // Listed plus every skip, so the summary line always adds up
	Listed  int
	Skipped map[Reason]int
	Remote  string // --remote, for the no remote "<name>" text
}

// Summarize counts listed repositories and skips by reason.
func Summarize(listed int, skips []Skip, remote string) Summary {
	s := Summary{Found: listed + len(skips), Listed: listed, Skipped: map[Reason]int{}, Remote: remote}
	for _, sk := range skips {
		s.Skipped[sk.Reason]++
	}
	return s
}

// Skips gives "2 skipped (1 no remote "origin", 1 unknown host)", or "" when nothing was
// skipped. Every message carrying skip counts uses it.
func (s Summary) Skips() string {
	n := s.Found - s.Listed
	if n == 0 {
		return ""
	}
	parts := make([]string, 0, len(reasonOrder))
	for _, r := range reasonOrder {
		if c := s.Skipped[r]; c > 0 {
			parts = append(parts, strconv.Itoa(c)+" "+r.Text(s.Remote))
		}
	}
	return fmt.Sprintf("%d skipped (%s)", n, strings.Join(parts, ", "))
}

// Line is the summary printed on stderr after the table.
func (s Summary) Line() string {
	line := fmt.Sprintf("%s found: %d listed", plural(s.Found, "repository", "repositories"), s.Listed)
	if skipped := s.Skips(); skipped != "" {
		line += ", " + skipped + "; run with -v to list them"
	}
	return line
}

// Err is nil when something is listed. Otherwise it is the "nothing to list" error: the
// v1 "not a git repository" when no clone was found at all, else ErrNoGitLab.
func (s Summary) Err(depth int) error {
	switch {
	case s.Listed > 0:
		return nil
	case s.Found == 0:
		return fmt.Errorf("%w: no clone found below it either (depth %d); run vigie in a clone or a folder of clones, or pass --project and --host",
			repo.ErrNotRepository, depth)
	}
	return fmt.Errorf(`%w: %s; add self-hosted hosts to the config file "hosts", or run with -v`, ErrNoGitLab, s.Skips())
}

// Outcome is nil when at least one row was fetched, else an *AllFailedError.
func Outcome(rows []status.Row, sum Summary) error {
	errs := make([]error, 0, len(rows))
	for _, r := range rows {
		if r.OK() {
			return nil
		}
		errs = append(errs, r.Err)
	}
	return &AllFailedError{Errs: errs, Skipped: sum}
}

// HostFailure is a host that rejected the token, or whose token could not be read.
type HostFailure struct {
	Err  error
	Rows int // repositories on that host
}

// Line is the stderr line of the host: its error and how many repositories it holds.
func (f HostFailure) Line() string {
	return fmt.Sprintf("%v (%s)", f.Err, plural(f.Rows, "repository", "repositories"))
}

// Latched groups the failed rows whose error latched their host by error value, in order
// of first appearance. The second result holds the other failed rows, in row order.
func Latched(rows []status.Row) ([]HostFailure, []status.Row) {
	var (
		hosts  []HostFailure
		others []status.Row
	)
	for _, r := range rows {
		switch {
		case r.OK():
			continue
		case !latches(r.Err):
			others = append(others, r)
			continue
		}
		// Every row of a latched host carries the same error value (host.fail).
		i := slices.IndexFunc(hosts, func(h HostFailure) bool { return errors.Is(r.Err, h.Err) })
		if i < 0 {
			hosts = append(hosts, HostFailure{Err: r.Err})
			i = len(hosts) - 1
		}
		hosts[i].Rows++
	}
	return hosts, others
}

// plural gives "1 repository" or "3 repositories".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
