package status_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sgaunet/vigie/internal/status"
)

func TestDefaultFields(t *testing.T) {
	t.Parallel()
	want := []status.FieldName{"branches", "pipelines", "last", "mrs", "issues"}
	if got := status.DefaultFields(); !slices.Equal(got, want) {
		t.Fatalf("DefaultFields() = %v, want %v", got, want)
	}
}

func TestParseFields(t *testing.T) {
	t.Parallel()
	got, err := status.ParseFields("mrs,last,pipelines")
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}
	want := []status.FieldName{status.FieldMRs, status.FieldLast, status.FieldPipelines}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	got, err = status.ParseFields(" branches , issues ")
	if err != nil || !slices.Equal(got, []status.FieldName{status.FieldBranches, status.FieldIssues}) {
		t.Fatalf("spaces: got %v, %v", got, err)
	}
}

func TestParseFieldsErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"bogus", "mrs,mrs", "", "mrs,,last", " "} {
		_, err := status.ParseFields(in)
		if err == nil {
			t.Errorf("ParseFields(%q): want an error", in)
			continue
		}
		if !errors.Is(err, status.ErrFieldName) {
			t.Errorf("ParseFields(%q): error %v does not wrap ErrFieldName", in, err)
		}
		if !strings.Contains(err.Error(), "branches, pipelines, last, mrs, issues") {
			t.Errorf("ParseFields(%q): error %q does not list the valid fields", in, err)
		}
	}
}

func TestParseFieldList(t *testing.T) {
	t.Parallel()
	got, err := status.ParseFieldList([]string{"issues", "branches"})
	if err != nil || !slices.Equal(got, []status.FieldName{status.FieldIssues, status.FieldBranches}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := status.ParseFieldList(nil); !errors.Is(err, status.ErrFieldName) {
		t.Fatalf("empty list: got %v", err)
	}
}
