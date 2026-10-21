package forge_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
)

func newHTTPClient(t *testing.T, opts forge.Options) *http.Client {
	t.Helper()
	hc, err := forge.NewHTTPClient(opts)
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	return hc
}

// get sends a GET and returns the status code or the error.
func get(t *testing.T, hc *http.Client, u string) (int, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// hops answers /hop/N with a redirect to /hop/N-1, and /hop/0 with 200.
func hops(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		hop, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/hop/"))
		if err != nil || hop == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/hop/"+strconv.Itoa(hop-1), http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestRedirectSameHostFollowed(t *testing.T) {
	t.Parallel()
	srv, n := hops(t)
	// At most 10 requests in a row, as Go's default policy: 9 redirects are followed.
	code, err := get(t, newHTTPClient(t, forge.Options{Insecure: true}), srv.URL+"/hop/9")
	if err != nil || code != http.StatusOK || n.Load() != 10 {
		t.Fatalf("9 redirects: code %d, err %v, %d requests", code, err, n.Load())
	}
}

func TestRedirectLimit(t *testing.T) {
	t.Parallel()
	srv, _ := hops(t)
	_, err := get(t, newHTTPClient(t, forge.Options{Insecure: true}), srv.URL+"/hop/11")
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("11 redirects: got %v", err)
	}
	if strings.Contains(err.Error(), "GitLab") {
		t.Fatalf("message names GitLab: %v", err)
	}
}

func TestRedirectsAreNotFollowedAcrossHosts(t *testing.T) {
	t.Parallel()
	var reached atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(other.Close)
	srv := httptest.NewServer(http.RedirectHandler(other.URL+"/x", http.StatusFound))
	t.Cleanup(srv.Close)
	_, err := get(t, newHTTPClient(t, forge.Options{Insecure: true}), srv.URL)
	if err == nil || reached.Load() != 0 {
		t.Fatalf("cross-host redirect: err %v, other host reached %d times", err, reached.Load())
	}
	if strings.Contains(err.Error(), "GitLab") {
		t.Fatalf("message names GitLab: %v", err)
	}
}

func TestRedirectDowngradeRefused(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(nil)
	srv.Config.Handler = http.RedirectHandler("http://"+srv.Listener.Addr().String()+"/x", http.StatusFound)
	t.Cleanup(srv.Close)
	_, err := get(t, newHTTPClient(t, forge.Options{ExtraRootsPEM: caOf(srv)}), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "plain http") {
		t.Fatalf("https to http redirect: got %v", err)
	}
	if strings.Contains(err.Error(), "GitLab") {
		t.Fatalf("message names GitLab: %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	if d := forge.ParseRetryAfter("7"); d != 7*time.Second {
		t.Errorf(`ParseRetryAfter("7") = %v`, d)
	}
	date := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	if d := forge.ParseRetryAfter(date); d < 25*time.Second || d > 31*time.Second {
		t.Errorf("ParseRetryAfter(date in 30s) = %v", d)
	}
	for _, v := range []string{"", "-3", "soon"} {
		if d := forge.ParseRetryAfter(v); d != 0 {
			t.Errorf("ParseRetryAfter(%q) = %v, want 0", v, d)
		}
	}
}
