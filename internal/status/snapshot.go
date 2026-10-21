package status

import (
	"errors"
	"time"
)

// ErrUnavailable marks a field the project does not offer (feature disabled or forbidden).
// It is a value shown as "–", not a failed refresh.
var ErrUnavailable = errors.New("unavailable")

// Count is a number of items; Capped means more than 10,000 and N is then meaningless.
type Count struct {
	N      int
	Capped bool
}

// Branches holds the active and total branch counts.
type Branches struct {
	Active Count
	Total  Count
}

// Pipelines holds the running and pending pipeline counts.
type Pipelines struct {
	Running Count
	Pending Count
}

// LastPipeline describes the latest pipeline on the reporting branch.
// Status is PipelineNone when no pipeline ran on it.
type LastPipeline struct {
	ID        int64
	Status    PipelineStatus
	Ref       string
	UpdatedAt time.Time
	WebURL    string
}

// FieldResult is the outcome of one field: the value matching Name, or Err.
type FieldResult struct {
	Name      FieldName
	Branches  Branches     // FieldBranches
	Pipelines Pipelines    // FieldPipelines
	Last      LastPipeline // FieldLast
	Count     Count        // FieldMRs, FieldIssues
	Err       error
}

// Project is the part of the project or repository shown on the line.
type Project struct {
	Path     string
	WebURL   string
	Archived bool
	Platform Platform // set from the endpoint wherever a Project is built
}

// Snapshot is the content of one refresh, rendered identically in every output format.
type Snapshot struct {
	Project    Project
	Branch     string
	Fields     []FieldResult
	FetchedAt  time.Time // last successful collection
	Stale      bool      // the latest refresh failed; values are from FetchedAt
	StaleSince time.Time // first failed refresh of the current failure streak
	RefreshErr error     // why the latest refresh failed, when Stale
}
