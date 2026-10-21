package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/token"
)

// maxBody bounds each response body (research R9).
const maxBody = 16 << 20

// Retry limits (research S6): at most maxRetries retries of one request, waiting the
// server's Retry-After, else retryBase × 2^attempt capped at maxBackoff.
const (
	maxRetries = 4
	maxBackoff = 4 * time.Second
)

var retryBase = 250 * time.Millisecond

// Client is a read-only GitLab REST API v4 client.
type Client struct {
	api  string // base URL + /api/v4, escaped, no trailing slash
	base string // base URL, for messages
	host string // host[:port], for messages
	tok  token.Token
	hc   *http.Client

	retry      bool
	onThrottle func()
}

// NewClient returns a client for the GitLab instance at base (e.g. https://gitlab.com).
// It refuses an http:// base unless opts.Insecure is set.
func NewClient(base *url.URL, tok token.Token, opts forge.Options) (*Client, error) {
	switch {
	case base.Scheme == "http" && !opts.Insecure:
		return nil, fmt.Errorf("%w %s: set insecure: true for this host in the config file", forge.ErrInsecure, base)
	case base.Scheme != "http" && base.Scheme != "https":
		return nil, fmt.Errorf("%w: unsupported scheme in %s", forge.ErrInsecure, base)
	}
	hc, err := forge.NewHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	b := strings.TrimSuffix(base.String(), "/")
	return &Client{
		api:        b + "/api/v4",
		base:       b,
		host:       base.Host,
		tok:        tok,
		hc:         hc,
		retry:      opts.Retry,
		onThrottle: opts.OnThrottle,
	}, nil
}

// statusError is a non-200 answer.
type statusError struct {
	code       int
	retryAfter time.Duration
}

func (e *statusError) Error() string {
	return fmt.Sprintf("server answered %d %s", e.code, http.StatusText(e.code))
}

// RetryAfter returns the delay the server asked for with Retry-After, 0 for none; read by
// forge.RetryAfter.
func (e *statusError) RetryAfter() time.Duration { return e.retryAfter }

// get fetches c.api+path and decodes the JSON body into out. It returns the response
// headers, a classified transport error, or a *statusError for any status but 200. With
// Retry set, a 429 or 5xx answer is retried, at most maxRetries times and only when the
// wait ends before the context deadline.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	for attempt := 0; ; attempt++ {
		h, err := c.getOnce(ctx, path, q, out)
		var se *statusError
		if !c.retry || !errors.As(err, &se) || (se.code != http.StatusTooManyRequests && se.code < 500) {
			return h, err
		}
		if c.onThrottle != nil {
			c.onThrottle()
		}
		wait := se.retryAfter
		if wait <= 0 {
			wait = min(retryBase<<attempt, maxBackoff)
		}
		if deadline, ok := ctx.Deadline(); attempt == maxRetries || (ok && time.Now().Add(wait).After(deadline)) {
			return h, err
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, forge.TransportError(ctx, c.host, ctx.Err())
		}
	}
}

// getOnce sends one request; see get.
func (c *Client) getOnce(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	u := c.api + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", c.host, err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.tok.Value())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "vigie")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, forge.TransportError(ctx, c.host, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := io.LimitReader(resp.Body, maxBody)
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, body)
		return resp.Header, &statusError{code: resp.StatusCode, retryAfter: forge.ParseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		if ctx.Err() != nil {
			return nil, forge.TransportError(ctx, c.host, err)
		}
		return nil, fmt.Errorf("malformed response from %s: %w", c.host, err)
	}
	return resp.Header, nil
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
