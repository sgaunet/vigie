package status

import (
	"fmt"
	"strings"
)

// Fingerprint identifies the content of a snapshot for change detection (FR-031): project
// and its platform, branch, field values and errors, and the stale flag. Fetch times, the stale-since time
// and the refresh error text are left out, so ages alone never count as a change.
func (s Snapshot) Fingerprint() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q %q %t %q %q %t|", s.Project.Path, s.Project.WebURL, s.Project.Archived, s.Project.Platform, s.Branch, s.Stale)
	for _, f := range s.Fields {
		fmt.Fprintf(&b, "%s %v %v %d %q %q %d %q %v %q;",
			f.Name, f.Branches, f.Pipelines,
			f.Last.ID, f.Last.Status, f.Last.Ref, f.Last.UpdatedAt.UnixNano(), f.Last.WebURL,
			f.Count, errText(f.Err))
	}
	return b.String()
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
