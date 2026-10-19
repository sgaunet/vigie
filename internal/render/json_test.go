package render_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/render"
	"github.com/sgaunet/vigie/internal/status"
)

func jsonOf(t *testing.T, s status.Snapshot) string {
	t.Helper()
	var buf bytes.Buffer
	if err := render.JSON(&buf, s); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("not exactly one line: %q", out)
	}
	return out
}

func jsonSnapshots() map[string]status.Snapshot {
	full := fullSnapshot()

	unavailable := fullSnapshot()
	unavailable.Fields[4] = status.FieldResult{Name: status.FieldIssues, Err: errors.New("issues unavailable (403): the feature may be disabled on this project")}

	capped := fullSnapshot()
	c := status.Count{Capped: true}
	capped.Fields[0].Branches = status.Branches{Active: c, Total: c}
	capped.Fields[1].Pipelines = status.Pipelines{Running: c, Pending: count(1)}
	capped.Fields[3].Count = c

	none := fullSnapshot()
	none.Fields[2].Last = status.LastPipeline{Status: status.PipelineNone, Ref: "main"}

	subset := fullSnapshot()
	subset.Fields = []status.FieldResult{subset.Fields[3], subset.Fields[1]}

	stale := fullSnapshot()
	stale.Stale = true
	stale.StaleSince = now.Add(15 * time.Second)
	stale.RefreshErr = errors.New("cannot reach gitlab.com: no answer within the time budget, raise --timeout")

	archived := fullSnapshot()
	archived.Project.Archived = true
	archived.Fields = nil

	return map[string]status.Snapshot{
		"json_full": full, "json_unavailable": unavailable, "json_capped": capped,
		"json_none": none, "json_subset": subset, "json_stale": stale, "json_archived": archived,
	}
}

func TestJSONGoldens(t *testing.T) {
	t.Parallel()
	for name, s := range jsonSnapshots() {
		golden(t, name, jsonOf(t, s))
	}
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", s, err)
	}
	return m
}

func TestJSONShapes(t *testing.T) {
	t.Parallel()
	snaps := jsonSnapshots()

	m := decode(t, jsonOf(t, snaps["json_unavailable"]))
	if v, ok := m["issues"]; !ok || v != nil {
		t.Errorf("unavailable issues = %v (present %v), want null", v, ok)
	}
	errs, _ := m["errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["field"] != "issues" {
		t.Errorf("errors = %v", m["errors"])
	}

	m = decode(t, jsonOf(t, snaps["json_capped"]))
	b := m["branches"].(map[string]any)
	if b["active"] != nil || b["total"] != nil || b["capped"] != true {
		t.Errorf("capped branches = %v", b)
	}
	if mrs := m["mrs"].(map[string]any); mrs["opened"] != nil || mrs["capped"] != true {
		t.Errorf("capped mrs = %v", mrs)
	}

	m = decode(t, jsonOf(t, snaps["json_none"]))
	if last := m["last"].(map[string]any); len(last) != 1 || last["status"] != "none" {
		t.Errorf("none last = %v", last)
	}

	m = decode(t, jsonOf(t, snaps["json_subset"]))
	for _, k := range []string{"branches", "last", "issues"} {
		if _, ok := m[k]; ok {
			t.Errorf("unrequested field %q present", k)
		}
	}

	m = decode(t, jsonOf(t, snaps["json_full"]))
	if m["stale"] != false || m["stale_since"] != nil || len(m["errors"].([]any)) != 0 {
		t.Errorf("fresh record: stale=%v stale_since=%v errors=%v", m["stale"], m["stale_since"], m["errors"])
	}
	if _, ok := m["stale_since"]; ok {
		t.Error("stale_since present on a fresh record")
	}

	m = decode(t, jsonOf(t, snaps["json_stale"]))
	if m["stale"] != true || m["stale_since"] == nil {
		t.Errorf("stale record: %v", m)
	}
	errs, _ = m["errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["field"] != "refresh" {
		t.Errorf("stale errors = %v", m["errors"])
	}
}

// schemaNode is the subset of JSON Schema used by status.schema.json.
type schemaNode struct {
	Type                 any                    `json:"type"`
	Required             []string               `json:"required"`
	Properties           map[string]*schemaNode `json:"properties"`
	AdditionalProperties *bool                  `json:"additionalProperties"`
	OneOf                []*schemaNode          `json:"oneOf"`
	Ref                  string                 `json:"$ref"`
	Enum                 []any                  `json:"enum"`
	Items                *schemaNode            `json:"items"`
	Defs                 map[string]*schemaNode `json:"$defs"`
}

// TestJSONMatchesSchema checks every golden record against the published schema: required
// keys, no extra keys, types and enums.
func TestJSONMatchesSchema(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../docs/status.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var root schemaNode
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	for name, s := range jsonSnapshots() {
		var v any
		if err := json.Unmarshal([]byte(jsonOf(t, s)), &v); err != nil {
			t.Fatal(err)
		}
		if err := validate(&root, &root, v, "$"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func validate(root, n *schemaNode, v any, path string) error {
	if n.Ref != "" {
		return validate(root, root.Defs[strings.TrimPrefix(n.Ref, "#/$defs/")], v, path)
	}
	if len(n.OneOf) > 0 {
		var errs []error
		for _, alt := range n.OneOf {
			err := validate(root, alt, v, path)
			if err == nil {
				return nil
			}
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}
	if len(n.Enum) > 0 && !slices.Contains(n.Enum, v) {
		return errors.New(path + ": value not in enum")
	}
	if n.Type != nil && !typeMatches(n.Type, v) {
		return errors.New(path + ": wrong type")
	}
	if items, ok := v.([]any); ok && n.Items != nil {
		for _, it := range items {
			if err := validate(root, n.Items, it, path+"[]"); err != nil {
				return err
			}
		}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range n.Required {
		if _, ok := obj[k]; !ok {
			return errors.New(path + ": missing " + k)
		}
	}
	for k, val := range obj {
		p, ok := n.Properties[k]
		if !ok {
			if n.AdditionalProperties != nil && !*n.AdditionalProperties {
				return errors.New(path + ": unexpected key " + k)
			}
			continue
		}
		if err := validate(root, p, val, path+"."+k); err != nil {
			return err
		}
	}
	return nil
}

func typeMatches(t any, v any) bool {
	switch tt := t.(type) {
	case string:
		switch tt {
		case "null":
			return v == nil
		case "object":
			_, ok := v.(map[string]any)
			return ok
		case "string":
			_, ok := v.(string)
			return ok
		case "boolean":
			_, ok := v.(bool)
			return ok
		case "integer":
			f, ok := v.(float64)
			return ok && f == float64(int64(f))
		case "array":
			_, ok := v.([]any)
			return ok
		}
	case []any:
		for _, x := range tt {
			if typeMatches(x, v) {
				return true
			}
		}
	}
	return false
}
