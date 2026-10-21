package github

import "net/http"

// APIBases returns the REST and GraphQL addresses the client derived from its base URL.
func APIBases(c *Client) (rest, graphql string) { return c.rest, c.graphql }

// The pure functions of the last field, and the run they read.
type Run = run

var (
	RunState = runState
	Combine  = combine
)

// RetryBase exposes the retry backoff base so a test can shorten it.
var RetryBase = &retryBase

// WithRoundTripper makes c send its requests through rt, such as a handler served in
// process: real sockets keep a testing/synctest bubble from advancing time.
func WithRoundTripper(c *Client, rt http.RoundTripper) { c.hc.Transport = rt }

// Now exposes the clock of the rate-limit gate.
var Now = &now
