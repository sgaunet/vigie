package status_test

import (
	"testing"

	"github.com/sgaunet/vigie/internal/status"
)

func TestRowOK(t *testing.T) {
	t.Parallel()
	if !(status.Row{}).OK() {
		t.Fatal("a row without failure must be OK")
	}
	cases := map[status.Failure]string{
		status.FailureAuth:     "auth",
		status.FailureNotFound: "not found",
		status.FailureNetwork:  "network",
		status.FailureError:    "error",
	}
	for f, text := range cases {
		if (status.Row{Failure: f}).OK() {
			t.Errorf("row with failure %q is OK", f)
		}
		if string(f) != text {
			t.Errorf("failure %q, want %q", f, text)
		}
	}
}
