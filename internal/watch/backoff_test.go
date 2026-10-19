package watch_test

import (
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/watch"
)

func TestBackoff(t *testing.T) {
	t.Parallel()
	b := watch.Backoff{Base: 5 * time.Second}
	if got := b.Next(); got != 5*time.Second {
		t.Fatalf("no failure: %v", got)
	}
	want := []time.Duration{10, 20, 40, 60, 60}
	for i, w := range want {
		b.Failure(0)
		if got := b.Next(); got != w*time.Second {
			t.Fatalf("failure %d: %v, want %v", i+1, got, w*time.Second)
		}
	}
	b.Success()
	if got := b.Next(); got != 5*time.Second {
		t.Fatalf("after success: %v", got)
	}
}

func TestBackoffRetryAfter(t *testing.T) {
	t.Parallel()
	b := watch.Backoff{Base: 5 * time.Second}
	b.Failure(30 * time.Second)
	if got := b.Next(); got != 30*time.Second {
		t.Fatalf("Retry-After 30s: %v", got)
	}
	b.Failure(time.Second) // smaller hint: the doubling wins
	if got := b.Next(); got != 20*time.Second {
		t.Fatalf("small hint: %v", got)
	}
	b.Failure(time.Hour)
	if got := b.Next(); got != 5*time.Minute {
		t.Fatalf("Retry-After capped: %v", got)
	}
	b.Success()
	if got := b.Next(); got != 5*time.Second {
		t.Fatalf("after success: %v", got)
	}
}
