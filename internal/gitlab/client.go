package gitlab

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sgaunet/vigie/internal/token"
)

// Transport limits (research R9). The response deadline is the refresh context.
const (
	dialTimeout      = 5 * time.Second
	handshakeTimeout = 5 * time.Second
	maxBody          = 16 << 20
	maxRedirects     = 10
)

var errRedirect = errors.New("redirect refused")

// Options tune NewClient.
type Options struct {
	Insecure      bool   // allow an http:// base URL
	ExtraRootsPEM []byte // CA certificates trusted in addition to the system pool
}

// Client is a read-only GitLab REST API v4 client.
type Client struct {
	api  string // base URL + /api/v4, escaped, no trailing slash
	base string // base URL, for messages
	host string // host[:port], for messages
	tok  token.Token
	hc   *http.Client
}

// NewClient returns a client for the GitLab instance at base (e.g. https://gitlab.com).
// It refuses an http:// base unless opts.Insecure is set.
func NewClient(base *url.URL, tok token.Token, opts Options) (*Client, error) {
	switch {
	case base.Scheme == "http" && !opts.Insecure:
		return nil, fmt.Errorf("%w %s: set insecure: true for this host in the config file", ErrInsecure, base)
	case base.Scheme != "http" && base.Scheme != "https":
		return nil, fmt.Errorf("%w: unsupported scheme in %s", ErrInsecure, base)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if err := addRoots(tlsConfig, opts.ExtraRootsPEM); err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: handshakeTimeout,
		TLSClientConfig:     tlsConfig,
		ForceAttemptHTTP2:   true,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}
	b := strings.TrimSuffix(base.String(), "/")
	return &Client{
		api:  b + "/api/v4",
		base: b,
		host: base.Host,
		tok:  tok,
		hc:   &http.Client{Transport: transport, CheckRedirect: checkRedirect},
	}, nil
}

// checkRedirect refuses redirects that change host or downgrade the scheme: Go forwards
// custom headers such as PRIVATE-TOKEN on redirects.
func checkRedirect(req *http.Request, via []*http.Request) error {
	orig := via[0].URL
	switch {
	case req.URL.Host != orig.Host:
		return fmt.Errorf("%w: %s redirects to %s, set the GitLab URL to the final address", errRedirect, orig.Host, req.URL.Host)
	case orig.Scheme == "https" && req.URL.Scheme != "https":
		return fmt.Errorf("%w: %s redirects to plain http", errRedirect, orig.Host)
	case len(via) >= maxRedirects:
		return fmt.Errorf("%w: too many redirects from %s", errRedirect, orig.Host)
	}
	return nil
}

// statusError is a non-200 answer.
type statusError struct {
	code       int
	retryAfter time.Duration
}

func (e *statusError) Error() string {
	return fmt.Sprintf("server answered %d %s", e.code, http.StatusText(e.code))
}

// RetryAfter returns the delay the server asked for with Retry-After, if any.
func RetryAfter(err error) (time.Duration, bool) {
	var se *statusError
	if errors.As(err, &se) && se.retryAfter > 0 {
		return se.retryAfter, true
	}
	return 0, false
}

// get fetches c.api+path and decodes the JSON body into out. It returns the response
// headers, a classified transport error, or a *statusError for any status but 200.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
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
		return nil, c.transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := io.LimitReader(resp.Body, maxBody)
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, body)
		return resp.Header, &statusError{code: resp.StatusCode, retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		if ctx.Err() != nil {
			return nil, c.transportError(ctx, err)
		}
		return nil, fmt.Errorf("malformed response from %s: %w", c.host, err)
	}
	return resp.Header, nil
}

func (c *Client) transportError(ctx context.Context, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	switch {
	case errors.Is(err, errRedirect):
		return err
	case errors.Is(ctx.Err(), context.Canceled):
		return fmt.Errorf("request to %s interrupted: %w", c.host, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%w %s: no answer within the time budget, raise --timeout: %w", ErrNetwork, c.host, context.DeadlineExceeded)
	}
	return fmt.Errorf("%w %s: %w", ErrNetwork, c.host, err)
}

// httpError classifies a *statusError that has no endpoint-specific meaning.
func (c *Client) httpError(err error) error {
	var se *statusError
	if !errors.As(err, &se) {
		return err
	}
	switch {
	case se.code == http.StatusUnauthorized:
		return fmt.Errorf("%w by %s (401): check that the token is valid and not expired", ErrUnauthorized, c.host)
	case se.code == http.StatusTooManyRequests || se.code >= 500:
		return fmt.Errorf("%w %s: %w", ErrNetwork, c.host, se)
	}
	return fmt.Errorf("unexpected answer from %s: %w", c.host, se)
}

// parseRetryAfter reads a Retry-After value in seconds or as an HTTP date.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil {
		return max(0, time.Duration(s)*time.Second)
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(0, time.Until(t))
	}
	return 0
}
