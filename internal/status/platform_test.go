package status_test

import (
	"testing"

	"github.com/sgaunet/vigie/internal/status"
)

func TestPlatform(t *testing.T) {
	t.Parallel()
	if string(status.PlatformGitLab) != "gitlab" || string(status.PlatformGitHub) != "github" {
		t.Fatalf("platform spellings changed: %q %q", status.PlatformGitLab, status.PlatformGitHub)
	}
}
