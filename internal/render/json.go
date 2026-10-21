package render

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

// The structs below are the JSON contract (docs/status.schema.json), kept apart from the
// domain types so that refactoring status never changes the output by accident.

type jsonRecord struct {
	Platform   status.Platform `json:"platform"` // first, in every record
	Project    string          `json:"project"`
	URL        string          `json:"url"`
	Archived   bool            `json:"archived"`
	Branch     string          `json:"branch"`
	Dir        string          `json:"dir,omitempty"`      // scan mode only
	Branches   json.RawMessage `json:"branches,omitempty"` // nil = not requested, "null" = unavailable
	Pipelines  json.RawMessage `json:"pipelines,omitempty"`
	Last       json.RawMessage `json:"last,omitempty"`
	MRs        json.RawMessage `json:"mrs,omitempty"`
	Issues     json.RawMessage `json:"issues,omitempty"`
	FetchedAt  string          `json:"fetched_at"`
	Stale      bool            `json:"stale"`
	StaleSince string          `json:"stale_since,omitempty"`
	Errors     []jsonError     `json:"errors"`
}

type jsonError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type jsonBranches struct {
	Active *int `json:"active"`
	Total  *int `json:"total"`
	Capped bool `json:"capped,omitempty"`
}

type jsonPipelines struct {
	Running *int `json:"running"`
	Pending *int `json:"pending"`
	Capped  bool `json:"capped,omitempty"`
}

type jsonLast struct {
	Status    string `json:"status"`
	ID        int64  `json:"id,omitempty"`
	Ref       string `json:"ref,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	WebURL    string `json:"web_url,omitempty"`
}

type jsonOpened struct {
	Opened *int `json:"opened"`
	Capped bool `json:"capped,omitempty"`
}

var jsonNull = json.RawMessage("null")

// JSON writes the snapshot as one JSON object followed by a newline.
func JSON(w io.Writer, s status.Snapshot) error {
	rec, err := snapshotRecord(s)
	if err != nil {
		return err
	}
	return encode(w, rec)
}

// JSONRow writes one scan row as JSON writes a snapshot, plus dir. A failed row has every
// requested field null and its error as the refresh error.
func JSONRow(w io.Writer, r status.Row) error {
	if !r.OK() {
		rec := baseRecord(r.Snapshot)
		rec.Dir = r.Dir
		for _, f := range r.Snapshot.Fields {
			rec.setField(f.Name, jsonNull)
		}
		if r.Err != nil {
			rec.Errors = append(rec.Errors, jsonError{Field: "refresh", Reason: r.Err.Error()})
		}
		return encode(w, rec)
	}
	rec, err := snapshotRecord(r.Snapshot)
	if err != nil {
		return err
	}
	rec.Dir = r.Dir
	return encode(w, rec)
}

func snapshotRecord(s status.Snapshot) (jsonRecord, error) {
	rec := baseRecord(s)
	for _, f := range s.Fields {
		raw, err := fieldJSON(f)
		if err != nil {
			return jsonRecord{}, err
		}
		rec.setField(f.Name, raw)
		if f.Err != nil {
			rec.Errors = append(rec.Errors, jsonError{Field: string(f.Name), Reason: f.Err.Error()})
		}
	}
	if s.Stale {
		rec.StaleSince = timestamp(s.StaleSince)
		if s.RefreshErr != nil {
			rec.Errors = append(rec.Errors, jsonError{Field: "refresh", Reason: s.RefreshErr.Error()})
		}
	}
	return rec, nil
}

// baseRecord fills the part of a record that does not depend on the fields.
func baseRecord(s status.Snapshot) jsonRecord {
	return jsonRecord{
		Platform:  s.Project.Platform,
		Project:   s.Project.Path,
		URL:       s.Project.WebURL,
		Archived:  s.Project.Archived,
		Branch:    s.Branch,
		FetchedAt: timestamp(s.FetchedAt),
		Stale:     s.Stale,
		Errors:    []jsonError{},
	}
}

func (rec *jsonRecord) setField(name status.FieldName, raw json.RawMessage) {
	switch name {
	case status.FieldBranches:
		rec.Branches = raw
	case status.FieldPipelines:
		rec.Pipelines = raw
	case status.FieldLast:
		rec.Last = raw
	case status.FieldMRs:
		rec.MRs = raw
	case status.FieldIssues:
		rec.Issues = raw
	}
}

func encode(w io.Writer, rec jsonRecord) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return fmt.Errorf("write json record: %w", err)
	}
	return nil
}

func fieldJSON(f status.FieldResult) (json.RawMessage, error) {
	if f.Err != nil {
		return jsonNull, nil
	}
	var v any
	switch f.Name {
	case status.FieldBranches:
		b := f.Branches
		v = jsonBranches{Active: countJSON(b.Active), Total: countJSON(b.Total), Capped: b.Active.Capped || b.Total.Capped}
	case status.FieldPipelines:
		p := f.Pipelines
		v = jsonPipelines{Running: countJSON(p.Running), Pending: countJSON(p.Pending), Capped: p.Running.Capped || p.Pending.Capped}
	case status.FieldLast:
		v = lastJSON(f.Last)
	case status.FieldMRs, status.FieldIssues:
		v = jsonOpened{Opened: countJSON(f.Count), Capped: f.Count.Capped}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode field %s: %w", f.Name, err)
	}
	return raw, nil
}

func lastJSON(l status.LastPipeline) jsonLast {
	if l.Status == status.PipelineNone {
		return jsonLast{Status: string(l.Status)}
	}
	return jsonLast{Status: string(l.Status), ID: l.ID, Ref: l.Ref, UpdatedAt: timestamp(l.UpdatedAt), WebURL: l.WebURL}
}

// countJSON returns nil (JSON null) for a capped count.
func countJSON(c status.Count) *int {
	if c.Capped {
		return nil
	}
	n := c.N
	return &n
}

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
