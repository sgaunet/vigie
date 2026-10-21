package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// schemaKeys returns the properties declared at the top of the published schema.
func schemaKeys(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "status.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for k := range schema.Properties {
		keys[k] = true
	}
	return keys
}

// records parses one JSON object per line.
func records(t *testing.T, out string) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line is not JSON: %v: %q", err, l)
		}
		recs = append(recs, m)
	}
	return recs
}

// jsonFolder is the main folder plus a clone on a branch whose last pipeline failed and
// a clone of a project that does not exist.
func jsonFolder(t *testing.T) *scanE2E {
	t.Helper()
	e := newScanE2E(t)
	e.mainFolder(t)
	cloneAt(t, e.root, "feat", "git@gitlab.test:backend/api.git", "feature")
	cloneAt(t, e.root, "legacy", "git@gitlab.test:group/legacy.git", "main")
	return e
}

func TestE2EScanJSON(t *testing.T) {
	t.Parallel()
	e := jsonFolder(t)
	out, errOut, code := e.run(t, nil, "-o", "json")
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	recs := records(t, out)
	var dirs []string
	keys := schemaKeys(t)
	for _, r := range recs {
		dirs = append(dirs, r["dir"].(string))
		for k := range r {
			if !keys[k] {
				t.Errorf("%s: key %q is not in the schema", r["dir"], k)
			}
		}
	}
	if want := []string{"api", "feat", "legacy", "team/tools/cli", "team/web"}; !slices.Equal(dirs, want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}

	legacy := recs[2]
	errs, _ := legacy["errors"].([]any)
	if legacy["project"] != "group/legacy" || legacy["branch"] != "main" || len(errs) != 1 || errs[0].(map[string]any)["field"] != "refresh" {
		t.Fatalf("failed record = %v", legacy)
	}
	for _, f := range []string{"branches", "pipelines", "last", "mrs", "issues"} {
		if v, ok := legacy[f]; !ok || v != nil {
			t.Errorf("failed record: %s = %v (present %v), want null", f, v, ok)
		}
	}

	var failing []string // jq -r 'select(.last.status=="failed") | .dir'
	for _, r := range recs {
		if last, ok := r["last"].(map[string]any); ok && last["status"] == "failed" {
			failing = append(failing, r["dir"].(string))
		}
	}
	if !slices.Equal(failing, []string{"feat"}) {
		t.Fatalf("failing pipelines in %v", failing)
	}
}

func TestE2EScanPipedTextIsPlain(t *testing.T) {
	t.Parallel()
	e := jsonFolder(t)
	out, _, code := e.run(t, nil)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || len(lines) != 6 || strings.ContainsRune(out, '\x1b') {
		t.Fatalf("code=%d stdout=%q", code, out)
	}
	for _, r := range out {
		if r > 0x7f && r != '–' {
			t.Fatalf("non-ASCII rune %q in piped output: %q", r, out)
		}
	}
	for i, dir := range []string{"DIR", "api", "feat", "legacy", "team/tools/cli", "team/web"} {
		if !strings.HasPrefix(lines[i], dir+" ") {
			t.Fatalf("line %d = %q, want it to start with %s", i, lines[i], dir)
		}
	}
}

// TestE2EScanRowIsSingleMode checks that a row is what vigie -C <clone> prints.
func TestE2EScanRowIsSingleMode(t *testing.T) {
	t.Parallel()
	e := newScanE2E(t)
	e.mainFolder(t)
	cloneAt(t, e.root, "feat", "git@gitlab.test:backend/api.git", "feature")
	out, errOut, code := e.run(t, nil, "-o", "json")
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for _, rec := range records(t, out) {
		dir := rec["dir"].(string)
		single, errOut, code := e.run(t, nil, "-o", "json", "-C", filepath.Join(e.root, filepath.FromSlash(dir)))
		if code != 0 {
			t.Fatalf("%s: code=%d stderr=%q", dir, code, errOut)
		}
		want := records(t, single)[0]
		if _, ok := want["dir"]; ok {
			t.Fatalf("%s: single mode printed dir", dir)
		}
		delete(rec, "dir")
		delete(rec, "fetched_at")
		delete(want, "fetched_at")
		if !reflect.DeepEqual(rec, want) {
			t.Errorf("%s:\n scan: %v\nsingle: %v", dir, rec, want)
		}
	}
}
