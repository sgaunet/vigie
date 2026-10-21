package status

// PipelineStatus is the status of a GitLab pipeline, plus None (no pipeline) and Unknown
// (a status this version does not know). GitHub workflow runs map onto the same values.
type PipelineStatus string

// Every GitLab pipeline status, then None and Unknown.
const (
	PipelineCreated            PipelineStatus = "created"
	PipelineWaitingForResource PipelineStatus = "waiting_for_resource"
	PipelinePreparing          PipelineStatus = "preparing"
	PipelinePending            PipelineStatus = "pending"
	PipelineRunning            PipelineStatus = "running"
	PipelineSuccess            PipelineStatus = "success"
	PipelineFailed             PipelineStatus = "failed"
	PipelineCanceled           PipelineStatus = "canceled"
	PipelineSkipped            PipelineStatus = "skipped"
	PipelineManual             PipelineStatus = "manual"
	PipelineScheduled          PipelineStatus = "scheduled"
	PipelineNone               PipelineStatus = "none"
	PipelineUnknown            PipelineStatus = "unknown"
)

// ParsePipelineStatus maps an API status string to its constant: "" is None and any
// unrecognised string is Unknown, so a new GitLab status never fails a refresh.
func ParsePipelineStatus(s string) PipelineStatus {
	switch p := PipelineStatus(s); p {
	case PipelineCreated, PipelineWaitingForResource, PipelinePreparing, PipelinePending,
		PipelineRunning, PipelineSuccess, PipelineFailed, PipelineCanceled, PipelineSkipped,
		PipelineManual, PipelineScheduled:
		return p
	case "":
		return PipelineNone
	default:
		return PipelineUnknown
	}
}
