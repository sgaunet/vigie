package config_test

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/status"
)

func dur(d time.Duration) *time.Duration { return &d }

func load(t *testing.T, ov config.Overrides, env map[string]string) (config.Settings, error) {
	t.Helper()
	return config.Load(ov, env, fstest.MapFS{})
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()
	s, err := load(t, config.Overrides{}, map[string]string{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Watch || s.Interval != 5*time.Second || s.Dir != "." || s.Remote != "origin" ||
		s.Project != "" || s.Host != nil || s.Output != config.OutputText || !s.Color || !s.Emoji ||
		s.Timeout != 10*time.Second || s.LogLevel != slog.LevelWarn || s.ActiveDays != 90 ||
		len(s.Hosts) != 0 || s.FallbackHost != nil {
		t.Fatalf("unexpected defaults: %+v", s)
	}
	if !slices.Equal(s.Fields, status.DefaultFields()) {
		t.Fatalf("Fields = %v", s.Fields)
	}
}

func TestLoadPrecedence(t *testing.T) {
	t.Parallel()
	env := map[string]string{"VIGIE_INTERVAL": "7s"}
	s, err := load(t, config.Overrides{}, env)
	if err != nil || s.Interval != 7*time.Second {
		t.Fatalf("env interval: %v, %v", s.Interval, err)
	}
	s, err = load(t, config.Overrides{Interval: dur(3 * time.Second)}, env)
	if err != nil || s.Interval != 3*time.Second {
		t.Fatalf("flag interval: %v, %v", s.Interval, err)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Parallel()
	ov := config.Overrides{
		Watch: true, Dir: "/src", Remote: "upstream", Project: "group/sub/app",
		Host: "https://gitlab.example.com", Fields: "mrs,last", Output: config.OutputJSON,
		NoColor: true, NoEmoji: true, Timeout: dur(4 * time.Second), Verbose: true,
	}
	s, err := load(t, ov, map[string]string{"GITLAB_HOST": "https://fallback.example.com"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Watch || s.Dir != "/src" || s.Remote != "upstream" || s.Project != "group/sub/app" ||
		s.Host.String() != "https://gitlab.example.com" || s.Output != config.OutputJSON ||
		s.Color || s.Emoji || s.Timeout != 4*time.Second || s.LogLevel != slog.LevelDebug {
		t.Fatalf("overrides not applied: %+v", s)
	}
	if s.FallbackHost == nil || s.FallbackHost.Host != "fallback.example.com" {
		t.Fatalf("FallbackHost = %v", s.FallbackHost)
	}
	if !slices.Equal(s.Fields, []status.FieldName{status.FieldMRs, status.FieldLast}) {
		t.Fatalf("Fields = %v", s.Fields)
	}
	s, err = load(t, config.Overrides{Quiet: true, Project: "1234"}, map[string]string{})
	if err != nil || s.LogLevel != slog.LevelError || s.Project != "1234" {
		t.Fatalf("quiet/numeric project: %+v, %v", s, err)
	}
}

func TestLoadNoColor(t *testing.T) {
	t.Parallel()
	s, _ := load(t, config.Overrides{}, map[string]string{"NO_COLOR": "1"})
	if s.Color {
		t.Error("NO_COLOR=1 must disable color")
	}
	s, _ = load(t, config.Overrides{}, map[string]string{"NO_COLOR": ""})
	if !s.Color {
		t.Error("empty NO_COLOR must be ignored")
	}
}

func TestLoadInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ov   config.Overrides
		env  map[string]string
	}{
		{"interval below 2s", config.Overrides{Interval: dur(time.Second)}, nil},
		{"env interval below 2s", config.Overrides{}, map[string]string{"VIGIE_INTERVAL": "1s"}},
		{"env interval unparsable", config.Overrides{}, map[string]string{"VIGIE_INTERVAL": "soon"}},
		{"zero timeout", config.Overrides{Timeout: dur(0)}, nil},
		{"negative timeout", config.Overrides{Timeout: dur(-time.Second)}, nil},
		{"one-segment project", config.Overrides{Project: "app"}, nil},
		{"empty segment project", config.Overrides{Project: "group//app"}, nil},
		{"unknown field", config.Overrides{Fields: "bogus"}, nil},
		{"duplicate field", config.Overrides{Fields: "mrs,mrs"}, nil},
		{"relative host", config.Overrides{Host: "gitlab.example.com"}, nil},
		{"ftp host", config.Overrides{Host: "ftp://gitlab.example.com"}, nil},
		{"bad GITLAB_HOST", config.Overrides{}, map[string]string{"GITLAB_HOST": "::"}},
		{"bad output", config.Overrides{Output: "yaml"}, nil},
	}
	for _, c := range cases {
		env := c.env
		if env == nil {
			env = map[string]string{}
		}
		if _, err := load(t, c.ov, env); !errors.Is(err, config.ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", c.name, err)
		}
	}
}

func intp(n int) *int { return &n }

func TestLoadDepth(t *testing.T) {
	t.Parallel()
	s, err := load(t, config.Overrides{}, map[string]string{})
	if err != nil || s.Depth != config.DefaultDepth || config.DefaultDepth != 4 {
		t.Fatalf("default depth = %d (DefaultDepth %d), %v", s.Depth, config.DefaultDepth, err)
	}
	s, err = load(t, config.Overrides{Depth: intp(2)}, map[string]string{})
	if err != nil || s.Depth != 2 {
		t.Fatalf("--depth 2: %d, %v", s.Depth, err)
	}
	for _, n := range []int{0, -1} {
		_, err := load(t, config.Overrides{Depth: intp(n)}, map[string]string{})
		if !errors.Is(err, config.ErrInvalid) {
			t.Fatalf("--depth %d: got %v, want ErrInvalid", n, err)
		}
		if want := fmt.Sprintf("invalid --depth %d: minimum is 1", n); err.Error() != want {
			t.Fatalf("--depth %d: message = %q, want %q", n, err, want)
		}
	}
}

func TestLoadIntervalMessage(t *testing.T) {
	t.Parallel()
	_, err := load(t, config.Overrides{Interval: dur(time.Second)}, map[string]string{})
	if err == nil || err.Error() != "invalid --interval 1s: minimum is 2s" {
		t.Fatalf("message = %v", err)
	}
}

func TestGitLabHostOnGitHub(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"https://github.com", "https://www.github.com"} {
		_, err := load(t, config.Overrides{}, map[string]string{"GITLAB_HOST": v})
		want := "GITLAB_HOST " + v + " is a GitHub host: unset it, and pass --host https://github.com outside a clone"
		if !errors.Is(err, config.ErrInvalid) || err.Error() != want {
			t.Errorf("%s: got %v, want %q", v, err, want)
		}
	}
	if _, err := load(t, config.Overrides{}, map[string]string{"GITLAB_HOST": "https://gitlab.example.com"}); err != nil {
		t.Errorf("GitLab GITLAB_HOST: %v", err)
	}
}
