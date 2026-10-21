package github

import (
	"bytes"
	"context"
	"encoding/json"
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

// Retry limits (research S6), as in gitlab: at most maxRetries retries of one request,
// waiting the server's Retry-After, else retryBase × 2^attempt capped at maxBackoff.
const (
	maxRetries = 4
	maxBackoff = 4 * time.Second
)

var retryBase = 250 * time.Millisecond

// Headers sent with every request (research G4).
const (
	apiVersion = "2022-11-28"
	mediaType  = "application/vnd.github+json"
)

// Client is a read-only client for the GitHub REST and GraphQL APIs of one instance.
type Client struct {
	rest    string // REST API address, no trailing slash
	graphql string // GraphQL endpoint
	host    string // web host[:port], for messages
	tok     token.Token
	hc      *http.Client

	retry      bool
	onThrottle func()
	cache      *cache // nil unless Options.Revalidate
	gate       *gate
}

// NewClient returns a client for the GitHub instance whose web address is base:
// https://github.com, whose API is at api.github.com, or an Enterprise Server, whose API is
// at base/api/v3 and base/api/graphql. It refuses an http:// base unless opts.Insecure is
// set.
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
	c := &Client{tok: tok, hc: hc, retry: opts.Retry, onThrottle: opts.OnThrottle, gate: newGate()}
	if opts.Revalidate {
		c.cache = newCache()
	}
	if dotCom(base) {
		c.rest, c.graphql, c.host = "https://api.github.com", "https://api.github.com/graphql", "github.com"
	} else {
		b := strings.TrimSuffix(base.String(), "/")
		c.rest, c.graphql, c.host = b+"/api/v3", b+"/api/graphql", base.Host
	}
	return c, nil
}

// dotCom reports whether base names github.com, whose API lives at api.github.com.
func dotCom(base *url.URL) bool {
	switch strings.ToLower(base.Hostname()) {
	case "github.com", "www.github.com", "ssh.github.com", "api.github.com":
		return true
	}
	return false
}

// get fetches c.rest+path and decodes the JSON body into out. It returns the response
// headers, a classified transport error, or a *statusError for any status but 200.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	u := c.rest + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return c.do(ctx, poolCore, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	}, out)
}

// graphQLRequest is the body of a GraphQL POST.
type graphQLRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

// graphQLError is the first error of a GraphQL answer, such as NOT_FOUND or FORBIDDEN.
type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (e *graphQLError) Error() string { return e.Type + ": " + e.Message }

// query runs the named GraphQL operation and decodes its data into out. A non-empty errors
// array becomes a *graphQLError carrying the first error, or a rate limit when the
// allowance is exhausted.
func (c *Client) query(ctx context.Context, op, doc string, vars map[string]any, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: doc, OperationName: op, Variables: vars})
	if err != nil {
		return fmt.Errorf("encode query %s: %w", op, err)
	}
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphQLError  `json:"errors"`
	}
	h, err := c.do(ctx, poolGraphQL, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, c.graphql, bytes.NewReader(body))
	}, &answer)
	switch {
	case err != nil:
		return err
	case len(answer.Errors) > 0 && h.Get("X-Ratelimit-Remaining") == "0":
		return c.primaryLimit(h, poolGraphQL)
	case len(answer.Errors) > 0:
		return &answer.Errors[0]
	}
	if err := json.Unmarshal(answer.Data, out); err != nil {
		return fmt.Errorf("malformed response from %s: %w", c.host, err)
	}
	return nil
}

// do sends the request build makes to pool p, and decodes a 200 answer into out. With Retry
// set, a 429 or 5xx answer or a secondary limit is retried, at most maxRetries times and
// only when the wait ends before the context deadline.
func (c *Client) do(ctx context.Context, p pool, build func() (*http.Request, error), out any) (http.Header, error) {
	for attempt := 0; ; attempt++ {
		req, err := build()
		if err != nil {
			return nil, fmt.Errorf("build request for %s: %w", c.host, err)
		}
		h, err := c.send(ctx, p, req, out)
		wait, again := retryable(err)
		if !c.retry || !again {
			return h, err
		}
		if c.onThrottle != nil {
			c.onThrottle()
		}
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

// send sends one request to pool p with the token and the API headers, and decodes a 200
// answer. While p is blocked, it answers locally with the stored limit. A GET is revalidated
// against the cache when there is one: a 304 decodes the cached body.
func (c *Client) send(ctx context.Context, p pool, req *http.Request, out any) (http.Header, error) {
	if err := c.gate.check(p); err != nil {
		return nil, err
	}
	var (
		key    string
		cached entry
		have   bool
	)
	if c.cache != nil && req.Method == http.MethodGet {
		key = req.URL.String()
		if cached, have = c.cache.get(key); have {
			req.Header.Set("If-None-Match", cached.etag)
		}
	}
	req.Header.Set("Authorization", "Bearer "+c.tok.Value())
	req.Header.Set("Accept", mediaType)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "vigie")
	if req.Method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, forge.TransportError(ctx, c.host, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, forge.TransportError(ctx, c.host, err)
	}
	switch {
	case resp.StatusCode == http.StatusNotModified && key != "":
		if !have {
			return nil, fmt.Errorf("malformed response from %s: 304 Not Modified without a cached answer", c.host)
		}
		body = cached.body
	case resp.StatusCode != http.StatusOK:
		return resp.Header, c.classify(resp, body, p)
	case key != "" && resp.Header.Get("ETag") != "":
		c.cache.put(key, entry{etag: resp.Header.Get("ETag"), body: body})
	}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("malformed response from %s: %w", c.host, err)
	}
	return resp.Header, nil
}
