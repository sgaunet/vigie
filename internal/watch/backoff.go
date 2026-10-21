package watch

import "time"

// Backoff limits. MaxRetryAfter bounds a server's delay hint: GitHub's allowance can reset
// up to an hour away, and no request may be sent before (research G8).
const (
	MaxBackoff    = 60 * time.Second
	MaxRetryAfter = time.Hour
)

// Backoff computes the delay before the next refresh: Base normally, doubled per
// consecutive failure up to 60 s, or the server's Retry-After when larger (up to 1 h).
type Backoff struct {
	Base       time.Duration
	Failures   int
	RetryAfter time.Duration
}

// Next returns the delay before the next refresh.
func (b *Backoff) Next() time.Duration {
	d := b.Base
	for i := 0; i < b.Failures && d < MaxBackoff; i++ {
		d *= 2
	}
	d = min(d, MaxBackoff)
	return max(d, min(b.RetryAfter, MaxRetryAfter))
}

// Failure records a failed refresh and the server's Retry-After hint (0 for none).
func (b *Backoff) Failure(retryAfter time.Duration) {
	b.Failures++
	b.RetryAfter = retryAfter
}

// Success resets the backoff.
func (b *Backoff) Success() {
	b.Failures = 0
	b.RetryAfter = 0
}
