package status

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// FieldName names one metric of the status line.
type FieldName string

// The five fields, in default display order.
const (
	FieldBranches  FieldName = "branches"
	FieldPipelines FieldName = "pipelines"
	FieldLast      FieldName = "last"
	FieldMRs       FieldName = "mrs"
	FieldIssues    FieldName = "issues"
)

// ErrFieldName marks an unknown, duplicated or missing field name. Its text is the hint.
var ErrFieldName = errors.New("valid fields are branches, pipelines, last, mrs, issues")

// DefaultFields returns every field in default display order.
func DefaultFields() []FieldName {
	return []FieldName{FieldBranches, FieldPipelines, FieldLast, FieldMRs, FieldIssues}
}

// ParseFields parses a comma-separated list such as "pipelines,last,mrs", keeping its order.
func ParseFields(s string) ([]FieldName, error) {
	return ParseFieldList(strings.Split(s, ","))
}

// ParseFieldList validates a list of field names, keeping its order.
func ParseFieldList(names []string) ([]FieldName, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("no field: %w", ErrFieldName)
	}
	fields := make([]FieldName, 0, len(names))
	for _, n := range names {
		f := FieldName(strings.TrimSpace(n))
		switch {
		case f == "":
			return nil, fmt.Errorf("empty field name: %w", ErrFieldName)
		case !slices.Contains(DefaultFields(), f):
			return nil, fmt.Errorf("unknown field %q: %w", f, ErrFieldName)
		case slices.Contains(fields, f):
			return nil, fmt.Errorf("duplicate field %q: %w", f, ErrFieldName)
		}
		fields = append(fields, f)
	}
	return fields, nil
}
