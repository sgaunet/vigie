package status

// Platform is the kind of service hosting a project. It is shown in the scan table and the
// JSON platform property, and picks the project icon on a terminal.
type Platform string

// The two platforms.
const (
	PlatformGitLab Platform = "gitlab"
	PlatformGitHub Platform = "github"
)
