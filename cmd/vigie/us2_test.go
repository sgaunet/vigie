package main_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
)

func TestE2EJSONOutput(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, errOut, code := e.run(t, "-o", "json")
	if code != 0 || errOut != "" || strings.Count(out, "\n") != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	var rec struct {
		Project   string `json:"project"`
		Branch    string `json:"branch"`
		Pipelines struct {
			Running int `json:"running"`
			Pending int `json:"pending"`
		} `json:"pipelines"`
		Last struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"last"`
		Issues struct {
			Opened int `json:"opened"`
		} `json:"issues"`
		Stale  *bool `json:"stale"`
		Errors []any `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("stdout is not JSON: %v: %q", err, out)
	}
	if rec.Project != "group/app" || rec.Branch != "main" || rec.Pipelines.Running != 2 || rec.Pipelines.Pending != 1 ||
		rec.Last.ID != 48213 || rec.Last.Status != "success" || rec.Issues.Opened != 17 ||
		rec.Stale == nil || *rec.Stale || rec.Errors == nil || len(rec.Errors) != 0 {
		t.Fatalf("record = %+v", rec)
	}
}

func TestE2EJSONUnavailableField(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.srv.Set(gitlabtest.EndpointMRs, gitlabtest.Behavior{Status: http.StatusNotFound})
	out, _, code := e.run(t, "--output=json")
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stdout=%q", code, err, out)
	}
	if v, ok := m["mrs"]; !ok || v != nil {
		t.Fatalf("mrs = %v (present %v)", v, ok)
	}
	errs := m["errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["field"] != "mrs" {
		t.Fatalf("errors = %v", errs)
	}
}

func TestE2EPipedTextIsPlain(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, _, code := e.run(t)
	if code != 0 || strings.ContainsRune(out, '\x1b') {
		t.Fatalf("escape codes in piped output: %q", out)
	}
	for _, r := range out {
		if r > 0x7f && r != '–' {
			t.Fatalf("non-ASCII rune %q in piped output: %q", r, out)
		}
	}
}

func TestE2EVerboseGoesToStderr(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, errOut, code := e.run(t, "-v")
	if code != 0 || strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "group/app ") {
		t.Fatalf("code=%d stdout=%q", code, out)
	}
	if !strings.Contains(errOut, "level=DEBUG") || !strings.Contains(errOut, "token=glpat-****") {
		t.Fatalf("stderr lacks debug logs with the masked token: %q", errOut)
	}
}

func TestE2EQuiet(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.srv.Set(gitlabtest.EndpointMRs, gitlabtest.Behavior{Status: http.StatusBadGateway})
	out, errOut, code := e.run(t)
	if code != 0 || !strings.Contains(out, "mr –") || !strings.Contains(errOut, "level=WARN") {
		t.Fatalf("default: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	_, errOut, code = e.run(t, "-q")
	if code != 0 || errOut != "" {
		t.Fatalf("quiet: code=%d stderr=%q", code, errOut)
	}
}
