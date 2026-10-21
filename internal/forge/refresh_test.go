package forge_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/status"
)

func TestSentinelTexts(t *testing.T) {
	t.Parallel()
	cases := map[error]string{
		forge.ErrInsecure:     "refusing a plain http API URL",
		forge.ErrUnauthorized: "token rejected",
		forge.ErrForbidden:    "denied",
		forge.ErrNotFound:     "not found",
		forge.ErrNetwork:      "cannot reach",
		forge.ErrRateLimit:    "request allowance exhausted",
	}
	for err, want := range cases {
		if err.Error() != want {
			t.Errorf("sentinel text = %q, want %q", err, want)
		}
	}
}

func TestRefreshError(t *testing.T) {
	t.Parallel()
	unauthorized := fmt.Errorf("%w by gitlab.test (401): check the token", forge.ErrUnauthorized)
	net1 := fmt.Errorf("%w gitlab.test: timeout", forge.ErrNetwork)
	net2 := fmt.Errorf("%w gitlab.test: reset", forge.ErrNetwork)
	limit1 := fmt.Errorf("%w x: %w", forge.ErrRateLimit, forge.ErrNetwork)
	limit2 := fmt.Errorf("%w y: %w", forge.ErrRateLimit, forge.ErrNetwork)
	unavailable := fmt.Errorf("issues %w (403)", status.ErrUnavailable)
	field := func(name status.FieldName, err error) status.FieldResult {
		return status.FieldResult{Name: name, Err: err}
	}
	cases := []struct {
		name   string
		fields []status.FieldResult
		want   error
	}{
		{"401 among successes", []status.FieldResult{field(status.FieldMRs, nil), field(status.FieldIssues, unauthorized)}, unauthorized},
		{"every field on the network", []status.FieldResult{field(status.FieldMRs, net1), field(status.FieldIssues, net2)}, net1},
		{"every field rate-limited", []status.FieldResult{field(status.FieldMRs, limit1), field(status.FieldIssues, limit2)}, limit1},
		{"one success among network failures", []status.FieldResult{field(status.FieldMRs, net1), field(status.FieldIssues, nil)}, nil},
		{"network plus unavailable", []status.FieldResult{field(status.FieldMRs, net1), field(status.FieldIssues, unavailable)}, nil},
		{"no field", nil, nil},
	}
	for _, c := range cases {
		// The errors above wrap only sentinels, so errors.Is tells which one came back; with a
		// nil target it holds only for nil.
		if got := forge.RefreshError(c.fields); !errors.Is(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// delayed carries a server delay hint, as both clients' status errors do.
type delayed time.Duration

func (d delayed) Error() string             { return "throttled" }
func (d delayed) RetryAfter() time.Duration { return time.Duration(d) }

func TestRetryAfter(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", delayed(3*time.Second)))
	if d, ok := forge.RetryAfter(wrapped); !ok || d != 3*time.Second {
		t.Fatalf("RetryAfter = %v %v", d, ok)
	}
	for _, err := range []error{delayed(0), errors.New("x"), nil} {
		if d, ok := forge.RetryAfter(err); ok || d != 0 {
			t.Errorf("RetryAfter(%v) = %v %v, want 0 false", err, d, ok)
		}
	}
}
