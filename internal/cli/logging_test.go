package cli_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/sgaunet/vigie/internal/cli"
)

func TestLoggerLevels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		level            slog.Level
		debug, info, warn, err bool
	}{
		{slog.LevelWarn, false, false, true, true},
		{slog.LevelDebug, true, true, true, true},
		{slog.LevelError, false, false, false, true},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		log := cli.NewLogger(&buf, c.level)
		log.Debug("m-debug")
		log.Info("m-info")
		log.Warn("m-warn")
		log.Error("m-error")
		out := buf.String()
		for msg, want := range map[string]bool{"m-debug": c.debug, "m-info": c.info, "m-warn": c.warn, "m-error": c.err} {
			if strings.Contains(out, msg) != want {
				t.Errorf("level %v: %s printed=%v, want %v", c.level, msg, !want, want)
			}
		}
	}
}

func TestLoggerMasksTokenAttributes(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := cli.NewLogger(&buf, slog.LevelDebug)
	log.Debug("x", "token", "glpat-s3cr3t", "gitlab_token", "s3cr3t", slog.Group("auth", "PRIVATE_TOKEN", "s3cr3t"), "host", "gitlab.com")
	out := buf.String()
	if strings.Contains(out, "s3cr3t") {
		t.Fatalf("token leaked: %q", out)
	}
	if !strings.Contains(out, "glpat-****") || !strings.Contains(out, "gitlab.com") {
		t.Fatalf("unexpected output: %q", out)
	}
}
