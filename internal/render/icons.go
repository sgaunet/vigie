package render

import "github.com/sgaunet/vigie/internal/status"

// look is how one thing appears: an emoji icon, its ASCII label, and a color.
type look struct {
	icon  string
	label string
	color sgr
}

// Field icons and labels (FR-019, FR-021). Colors of these segments depend on values.
var (
	projectLook   = look{icon: "🗼", color: bold}
	branchesLook  = look{icon: "🌿", label: "br"}
	pipelinesLook = look{icon: "⚙️", label: "ci"}
	mrsLook       = look{icon: "🔀", label: "mr"}
	issuesLook    = look{icon: "🐛", label: "is"}
	lastLook      = look{icon: "❔", label: "last:"} // the last field when unavailable
	archivedLook  = look{label: "archived", color: gray}
	staleLook     = look{icon: "⚠", label: "!", color: yellow}
)

// platformLooks is the project look of each platform; projectLook is the fallback for a
// snapshot without one (FR-021).
var platformLooks = map[status.Platform]look{
	status.PlatformGitLab: {icon: "🦊", color: bold},
	status.PlatformGitHub: {icon: "🐙", color: bold},
}

func platformLook(p status.Platform) look {
	if l, ok := platformLooks[p]; ok {
		return l
	}
	return projectLook
}

// pipelineLooks is the icon, ASCII label and color of each pipeline status.
var pipelineLooks = map[status.PipelineStatus]look{
	status.PipelineSuccess:            {"✅", "ok", green},
	status.PipelineFailed:             {"❌", "fail", red},
	status.PipelineRunning:            {"🔄", "run", blue},
	status.PipelinePending:            {"⏳", "wait", yellow},
	status.PipelineCreated:            {"⏳", "wait", yellow},
	status.PipelinePreparing:          {"⏳", "wait", yellow},
	status.PipelineWaitingForResource: {"⏳", "wait", yellow},
	status.PipelineManual:             {"✋", "manual", magenta},
	status.PipelineScheduled:          {"🕒", "sched", cyan},
	status.PipelineCanceled:           {"⏭️", "skip", gray},
	status.PipelineSkipped:            {"⏭️", "skip", gray},
	status.PipelineNone:               {"∅", "none", dim},
	status.PipelineUnknown:            {"❓", "?", gray},
}

func pipelineLook(s status.PipelineStatus) look {
	if l, ok := pipelineLooks[s]; ok {
		return l
	}
	return pipelineLooks[status.PipelineUnknown]
}
