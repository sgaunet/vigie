package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
)

// now is the clock of the gate.
var now = time.Now

// secondaryWait is the wait of a secondary limit without Retry-After: GitHub asks for at
// least one minute.
const secondaryWait = time.Minute

// pool is one allowance pool: REST requests or GraphQL points (research G7).
type pool string

const (
	poolCore    pool = "core"
	poolGraphQL pool = "graphql"
)

// rateLimitError is an exhausted allowance (primary) or a request to slow down
// (secondary). Both are throttling, never an authentication failure: a primary limit
// wraps forge.ErrRateLimit and forge.ErrNetwork, a secondary one forge.ErrNetwork only.
type rateLimitError struct {
	host    string
	primary bool
	reset   time.Time     // primary: when the allowance resets
	until   time.Time     // when requests may be sent again
	after   time.Duration // until − now when it happened, for the message
}

func (e *rateLimitError) Error() string {
	if e.primary {
		return fmt.Sprintf("%s request allowance exhausted until %s (in %s): wait for the reset; it is shared by all your GitHub tokens",
			e.host, e.reset.Local().Format("15:04"), inText(e.after))
	}
	return fmt.Sprintf("%s asked to slow down (secondary rate limit): wait %s", e.host, inText(e.after))
}

// RetryAfter returns the time left until requests may be sent again; read by
// forge.RetryAfter.
func (e *rateLimitError) RetryAfter() time.Duration { return max(0, e.until.Sub(now())) }

func (e *rateLimitError) Unwrap() []error {
	if e.primary {
		return []error{forge.ErrRateLimit, forge.ErrNetwork}
	}
	return []error{forge.ErrNetwork}
}

// inText gives a wait as 13m, or 40s under a minute.
func inText(d time.Duration) string {
	if d < time.Minute {
		return strconv.Itoa(int((d+time.Second-1)/time.Second)) + "s"
	}
	return strconv.Itoa(int(d.Round(time.Minute)/time.Minute)) + "m"
}

// gate stops the client's own requests to a pool while its allowance is exhausted or GitHub
// asked to slow down (research G8): they are answered locally with the stored error.
type gate struct {
	mu      sync.Mutex
	blocked map[pool]*rateLimitError
}

func newGate() *gate { return &gate{blocked: map[pool]*rateLimitError{}} }

// check returns the error p is blocked with, or nil.
func (g *gate) check(p pool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e := g.blocked[p]; e != nil && now().Before(e.until) {
		return e
	}
	return nil
}

// block blocks pools until e.until, unless one is already blocked for longer.
func (g *gate) block(e *rateLimitError, pools ...pool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range pools {
		if cur := g.blocked[p]; cur == nil || e.until.After(cur.until) {
			g.blocked[p] = e
		}
	}
}

// primaryLimit records an exhausted allowance of p, which resets at x-ratelimit-reset; the
// gate opens 1 s later, for clock skew.
func (c *Client) primaryLimit(h http.Header, p pool) error {
	t := now()
	reset := t.Add(secondaryWait)
	if n, err := strconv.ParseInt(h.Get("X-Ratelimit-Reset"), 10, 64); err == nil {
		reset = time.Unix(n, 0)
	}
	until := reset.Add(time.Second)
	e := &rateLimitError{host: c.host, primary: true, reset: reset, until: until, after: until.Sub(t)}
	c.gate.block(e, p)
	return e
}

// secondaryLimit records a request to slow down, which blocks both pools.
func (c *Client) secondaryLimit(after time.Duration) error {
	if after <= 0 {
		after = secondaryWait
	}
	e := &rateLimitError{host: c.host, until: now().Add(after), after: after}
	c.gate.block(e, poolCore, poolGraphQL)
	return e
}

// statusError is a non-200 answer: its code, the server's retry delay, its headers (which
// name a missing permission or single sign-on) and GitHub's message.
type statusError struct {
	code       int
	retryAfter time.Duration
	header     http.Header
	message    string
}

func (e *statusError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("server answered %d %s: %s", e.code, http.StatusText(e.code), e.message)
	}
	return fmt.Sprintf("server answered %d %s", e.code, http.StatusText(e.code))
}

// RetryAfter returns the delay the server asked for with Retry-After, 0 for none; read by
// forge.RetryAfter.
func (e *statusError) RetryAfter() time.Duration { return e.retryAfter }

// classify turns a non-200 answer of pool p into an error: rate limits first, in the order
// of research G8, then a *statusError.
func (c *Client) classify(resp *http.Response, body []byte, p pool) error {
	retryAfter := forge.ParseRetryAfter(resp.Header.Get("Retry-After"))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		switch {
		case resp.Header.Get("X-Ratelimit-Remaining") == "0":
			return c.primaryLimit(resp.Header, p)
		case resp.Header.Get("Retry-After") != "" || bytes.Contains(bytes.ToLower(body), []byte("secondary rate limit")):
			return c.secondaryLimit(retryAfter)
		}
	}
	var m struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &m)
	return &statusError{
		code:       resp.StatusCode,
		retryAfter: retryAfter,
		header:     resp.Header,
		message:    m.Message,
	}
}

// retryable returns the wait before retrying err, and whether to retry it at all: a 429 or
// 5xx answer, or a secondary limit, but never an exhausted allowance.
func retryable(err error) (time.Duration, bool) {
	var (
		se *statusError
		le *rateLimitError
	)
	switch {
	case errors.As(err, &se):
		return se.retryAfter, se.code == http.StatusTooManyRequests || se.code >= 500
	case errors.As(err, &le):
		return le.RetryAfter(), !le.primary
	}
	return 0, false
}

// httpError classifies a *statusError that has no endpoint-specific meaning.
func (c *Client) httpError(err error) error {
	var se *statusError
	if !errors.As(err, &se) {
		return err
	}
	switch {
	case se.code == http.StatusUnauthorized:
		return fmt.Errorf("%w by %s (401): check that the token is valid and not expired", forge.ErrUnauthorized, c.host)
	case se.code == http.StatusTooManyRequests || se.code >= 500:
		return fmt.Errorf("%w %s: %w", forge.ErrNetwork, c.host, se)
	}
	return fmt.Errorf("unexpected answer from %s: %w", c.host, se)
}
