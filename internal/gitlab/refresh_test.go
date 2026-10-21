package gitlab_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/status"
)

func TestRefreshError(t *testing.T) {
	t.Parallel()
	unauthorized := fmt.Errorf("%w by gitlab.test (401): check the token", gitlab.ErrUnauthorized)
	net1 := fmt.Errorf("%w gitlab.test: timeout", gitlab.ErrNetwork)
	net2 := fmt.Errorf("%w gitlab.test: reset", gitlab.ErrNetwork)
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
		{"one success among network failures", []status.FieldResult{field(status.FieldMRs, net1), field(status.FieldIssues, nil)}, nil},
		{"network plus unavailable", []status.FieldResult{field(status.FieldMRs, net1), field(status.FieldIssues, unavailable)}, nil},
		{"no field", nil, nil},
	}
	for _, c := range cases {
		// The errors above wrap only sentinels, so errors.Is tells which one came back; with a
		// nil target it holds only for nil.
		if got := gitlab.RefreshError(c.fields); !errors.Is(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
