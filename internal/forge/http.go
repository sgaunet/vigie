package forge

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Transport limits (research R9). The response deadline is the refresh context.
const (
	dialTimeout      = 5 * time.Second
	handshakeTimeout = 5 * time.Second
	maxRedirects     = 10
)

var errRedirect = errors.New("redirect refused")

// NewHTTPClient returns the HTTP client both platforms use: proxy from the environment,
// 5 s dial and TLS handshake timeouts, TLS 1.2 or later with opts.ExtraRootsPEM trusted,
// and redirects kept on the same host and scheme.
func NewHTTPClient(opts Options) (*http.Client, error) {
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
	return &http.Client{Transport: transport, CheckRedirect: checkRedirect}, nil
}

// checkRedirect refuses redirects that change host or downgrade the scheme: Go forwards
// custom headers such as PRIVATE-TOKEN on redirects.
func checkRedirect(req *http.Request, via []*http.Request) error {
	orig := via[0].URL
	switch {
	case req.URL.Host != orig.Host:
		return fmt.Errorf("%w: %s redirects to %s, set the base URL to the final address", errRedirect, orig.Host, req.URL.Host)
	case orig.Scheme == "https" && req.URL.Scheme != "https":
		return fmt.Errorf("%w: %s redirects to plain http", errRedirect, orig.Host)
	case len(via) >= maxRedirects:
		return fmt.Errorf("%w: too many redirects from %s", errRedirect, orig.Host)
	}
	return nil
}

// TransportError classifies an error of a request to host sent under ctx: a refused
// redirect as is, an interrupt as context.Canceled, a deadline or any other failure as
// ErrNetwork.
func TransportError(ctx context.Context, host string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	switch {
	case errors.Is(err, errRedirect):
		return err
	case errors.Is(ctx.Err(), context.Canceled):
		return fmt.Errorf("request to %s interrupted: %w", host, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%w %s: no answer within the time budget, raise --timeout: %w", ErrNetwork, host, context.DeadlineExceeded)
	}
	return fmt.Errorf("%w %s: %w", ErrNetwork, host, err)
}

// ParseRetryAfter reads a Retry-After value in seconds or as an HTTP date; anything else,
// or a time in the past, is 0.
func ParseRetryAfter(v string) time.Duration {
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
