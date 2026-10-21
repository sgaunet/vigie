package status

// Failure tells why a scan row has no values. The zero value means the row was fetched.
type Failure string

// Failure classes, shown as the row marker.
const (
	FailureAuth     Failure = "auth"      // token missing, rejected, or not allowed to see the project
	FailureNotFound Failure = "not found" // no such project on the host
	FailureNetwork  Failure = "network"   // host unreachable, throttled, or too slow
	FailureError    Failure = "error"     // anything else, e.g. an http host not marked insecure
)

// Row is one listed repository of a scan.
type Row struct {
	Dir string // relative to the scan root, /-separated on every platform
	// Snapshot holds the values when fetched. When failed, it holds the project path from
	// the remote, its web URL, the checkout branch, the requested field names with no
	// values, and the time of the attempt.
	Snapshot Snapshot
	Failure  Failure // "" when fetched
	Err      error   // why the row failed; never contains the token
}

// OK reports whether the row was fetched.
func (r Row) OK() bool { return r.Failure == "" }
