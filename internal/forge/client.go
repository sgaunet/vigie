package forge

import (
	"context"

	"github.com/sgaunet/vigie/internal/status"
)

// Client is a read-only client of one hosting instance. *gitlab.Client and *github.Client
// implement it.
type Client interface {
	// Project looks up a project by path (group/sub/name, owner/repo) or numeric ID.
	Project(ctx context.Context, ref string) (Project, error)
	// Source returns the metrics of p, for status.Collect and watch.
	Source(p Project) status.Source
}

// Project is the project or repository behind a clone, as looked up.
type Project struct {
	ID            int64  // project or repository ID
	Path          string // group/sub/name on GitLab, owner/repo on GitHub (after any rename)
	WebURL        string // web page of the project
	DefaultBranch string // "" for an empty project
	Archived      bool
}

// Options tune the construction of a client.
type Options struct {
	Insecure      bool   // allow an http:// base URL
	ExtraRootsPEM []byte // CA certificates trusted in addition to the system pool
	Retry         bool   // retry 429 and 5xx answers within the context deadline (scan mode)
	OnThrottle    func() // called on every 429 or 5xx answer when Retry is set; may be nil
	Revalidate    bool   // GitHub: send conditional REST requests against an in-memory ETag cache (single mode)
}
