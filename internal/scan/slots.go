package scan

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// slots is a semaphore of the repositories in flight on one host. Its limit can shrink
// when the host throttles; slots already taken are kept until released.
type slots struct {
	mu       sync.Mutex
	limit    int
	inUse    int
	wake     chan struct{} // closed and replaced on every Release
	halvedAt time.Time
}

func newSlots(n int) *slots {
	return &slots{limit: n, wake: make(chan struct{})}
}

// Acquire waits for a free slot, or for ctx to end; then it takes no slot.
func (s *slots) Acquire(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait for a free slot: %w", err)
		}
		s.mu.Lock()
		if s.inUse < s.limit {
			s.inUse++
			s.mu.Unlock()
			return nil
		}
		wake := s.wake
		s.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
		}
	}
}

// Halve halves the limit, down to 1, at most once per second, so a burst of throttled
// answers from one wave counts once. It returns the limit and whether it changed.
func (s *slots) Halve() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.limit == 1 || (!s.halvedAt.IsZero() && now.Sub(s.halvedAt) < time.Second) {
		return s.limit, false
	}
	s.limit = max(1, s.limit/2)
	s.halvedAt = now
	return s.limit, true
}

// Release gives a slot back and wakes every waiter.
func (s *slots) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inUse--
	close(s.wake)
	s.wake = make(chan struct{})
}
