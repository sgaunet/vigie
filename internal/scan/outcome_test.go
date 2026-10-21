package scan_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/sgaunet/vigie/internal/scan"
	"github.com/sgaunet/vigie/internal/status"
)

func failed(dir string, f status.Failure, err error) status.Row {
	return status.Row{Dir: dir, Failure: f, Err: err}
}

func TestOutcome(t *testing.T) {
	t.Parallel()
	e1, e2, e3 := errors.New("e1"), errors.New("e2"), errors.New("e3")
	sum := scan.Summarize(3, skips(scan.ReasonUnknownHost), "origin")
	if err := scan.Outcome([]status.Row{failed("a", status.FailureError, e1), {Dir: "b"}, failed("c", status.FailureError, e2)}, sum); err != nil {
		t.Fatalf("one row OK: %v", err)
	}
	rows := []status.Row{failed("a", status.FailureError, e1), failed("b", status.FailureAuth, e2), failed("c", status.FailureNetwork, e3)}
	err := scan.Outcome(rows, sum)
	var af *scan.AllFailedError
	if !errors.As(err, &af) || !slices.Equal(af.Errs, []error{e1, e2, e3}) || af.Skipped.Found != sum.Found {
		t.Fatalf("all failed: %#v", err)
	}
	if errors.Unwrap(err) != nil {
		t.Fatal("AllFailedError must not unwrap: no single row error stands for the run")
	}
	cases := []struct {
		err  error
		want string
	}{
		{err, "all 3 listed repositories failed; 1 skipped (1 unknown host); run with -v to list them"},
		{scan.Outcome(rows, scan.Summarize(3, nil, "origin")), "all 3 listed repositories failed"},
		{scan.Outcome(rows[:1], scan.Summarize(1, nil, "origin")), "1 listed repository failed"},
	}
	for _, c := range cases {
		if c.err == nil || c.err.Error() != c.want {
			t.Errorf("message = %v, want %q", c.err, c.want)
		}
	}
}

func TestHostFailureLine(t *testing.T) {
	t.Parallel()
	err := errors.New("token rejected by gitlab.example.com (401): check that the token is valid and not expired")
	if got := (scan.HostFailure{Err: err, Rows: 2}).Line(); got != err.Error()+" (2 repositories)" {
		t.Errorf("Line() = %q", got)
	}
	if got := (scan.HostFailure{Err: err, Rows: 1}).Line(); got != err.Error()+" (1 repository)" {
		t.Errorf("Line() = %q", got)
	}
}
