//go:build unix

package main_test

import (
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/github/githubtest"
)

// TestE2EGitHubWatchAllowance checks SC-006: an exhausted allowance makes the line stale,
// nothing is sent before the reset, and fresh values come back within one interval of it.
func TestE2EGitHubWatchAllowance(t *testing.T) {
	t.Parallel()
	e := newGitHubE2E(t)
	w := startWatchIn(t, e.repo, e.env, "-o", "json")
	if l := w.next(t, 5*time.Second); !strings.Contains(l, `"stale":false`) {
		t.Fatalf("first record = %q", l)
	}
	reset := time.Now().Add(3 * time.Second)
	e.srv.SetAll(githubtest.Behavior{RateLimited: true, Reset: reset})
	if l := w.next(t, 5*time.Second); !strings.Contains(l, `"stale":true`) {
		t.Fatalf("limited record = %q", l)
	}
	sent := len(e.srv.Requests())
	e.srv.SetAll(githubtest.Behavior{})
	for time.Now().Before(reset) {
		if n := len(e.srv.Requests()); n != sent {
			t.Fatalf("%d requests sent before the reset", n-sent)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Reset, 1 s of margin, one interval, and slack for the request itself.
	if l := w.next(t, time.Until(reset)+time.Second+2*time.Second+1500*time.Millisecond); !strings.Contains(l, `"stale":false`) {
		t.Fatalf("recovered record = %q", l)
	}
}
