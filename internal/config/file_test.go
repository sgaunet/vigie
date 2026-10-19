package config_test

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/status"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fsWith returns a filesystem holding one config file at the absolute path p.
func fsWith(t *testing.T, p, name string) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{strings.TrimPrefix(p, "/"): {Data: fixture(t, name)}}
}

func TestConfigPathDiscovery(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  map[string]string
		path string
	}{
		{"VIGIE_CONFIG", map[string]string{"VIGIE_CONFIG": "/etc/vigie.yaml", "XDG_CONFIG_HOME": "/xdg", "HOME": "/home/u"}, "/etc/vigie.yaml"},
		{"XDG", map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/u"}, "/xdg/vigie/config.yaml"},
		{"HOME", map[string]string{"HOME": "/home/u"}, "/home/u/.config/vigie/config.yaml"},
	}
	for _, c := range cases {
		s, err := config.Load(config.Overrides{}, c.env, fsWith(t, c.path, "interval.yaml"))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if s.ConfigPath != c.path || s.Interval != 30*time.Second {
			t.Errorf("%s: ConfigPath=%q Interval=%v", c.name, s.ConfigPath, s.Interval)
		}
	}
}

func TestMissingConfigFile(t *testing.T) {
	t.Parallel()
	s, err := config.Load(config.Overrides{}, map[string]string{"HOME": "/home/u"}, fstest.MapFS{})
	if err != nil || s.ConfigPath != "" {
		t.Fatalf("missing default file: %q, %v", s.ConfigPath, err)
	}
	_, err = config.Load(config.Overrides{}, map[string]string{"VIGIE_CONFIG": "/nope.yaml"}, fstest.MapFS{})
	if !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("missing VIGIE_CONFIG file: %v", err)
	}
}

func loadFile(t *testing.T, name string, ov config.Overrides, env map[string]string) (config.Settings, error) {
	t.Helper()
	if env == nil {
		env = map[string]string{}
	}
	env["VIGIE_CONFIG"] = "/cfg/config.yaml"
	return config.Load(ov, env, fsWith(t, "/cfg/config.yaml", name))
}

func TestConfigFileErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]error{
		"unknown_key.yaml":     config.ErrInvalid,
		"token_both.yaml":      config.ErrInvalid,
		"bad_fields.yaml":      config.ErrInvalid,
		"bad_active_days.yaml": config.ErrInvalid,
		"bad_api.yaml":         config.ErrInvalid,
		"malformed.yaml":       config.ErrInvalid,
		"token_top.yaml":       config.ErrTokenInFile,
		"token_nested.yaml":    config.ErrTokenInFile,
	}
	for name, want := range cases {
		_, err := loadFile(t, name, config.Overrides{}, nil)
		if !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", name, err, want)
			continue
		}
		if strings.Contains(err.Error(), "glpat-fromfile") {
			t.Errorf("%s: error message leaks the token: %q", name, err)
		}
	}
	_, err := loadFile(t, "token_nested.yaml", config.Overrides{}, nil)
	if !strings.Contains(err.Error(), "/cfg/config.yaml") {
		t.Errorf("token error does not name the file: %q", err)
	}
	_, err = loadFile(t, "bad_fields.yaml", config.Overrides{}, nil)
	if !strings.Contains(err.Error(), "branches, pipelines, last, mrs, issues") {
		t.Errorf("fields error does not list the valid fields: %q", err)
	}
}

func TestConfigFileValues(t *testing.T) {
	t.Parallel()
	s, err := loadFile(t, "full.yaml", config.Overrides{}, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Interval != 30*time.Second || s.Emoji || s.ActiveDays != 30 {
		t.Fatalf("file values not applied: %+v", s)
	}
	if !slices.Equal(s.Fields, []status.FieldName{status.FieldMRs, status.FieldLast}) {
		t.Fatalf("Fields = %v", s.Fields)
	}

	p, ok := s.Profile("ssh.gitlab.example.com")
	if !ok || p.API.String() != "https://gitlab.example.com" || p.Insecure ||
		!slices.Equal(p.Token.Command, []string{"pass", "show", "gitlab/example"}) || p.Token.Env != "" {
		t.Fatalf("ssh profile = %+v, %v", p, ok)
	}
	p, ok = s.Profile("gitlab.lan")
	if !ok || !p.Insecure || p.Token.Env != "LAN_TOKEN" || p.API.Scheme != "http" {
		t.Fatalf("lan profile = %+v, %v", p, ok)
	}
	if _, ok := s.Profile("gitlab.com"); ok {
		t.Fatal("unexpected profile for gitlab.com")
	}
}

func TestConfigFileRanksBelowEnvAndFlags(t *testing.T) {
	t.Parallel()
	s, err := loadFile(t, "full.yaml", config.Overrides{}, map[string]string{"VIGIE_INTERVAL": "7s"})
	if err != nil || s.Interval != 7*time.Second {
		t.Fatalf("env over file: %v, %v", s.Interval, err)
	}
	ov := config.Overrides{Interval: dur(3 * time.Second), Fields: "issues", NoEmoji: false}
	s, err = loadFile(t, "full.yaml", ov, map[string]string{"VIGIE_INTERVAL": "7s"})
	if err != nil || s.Interval != 3*time.Second || !slices.Equal(s.Fields, []status.FieldName{status.FieldIssues}) {
		t.Fatalf("flags over file: %+v, %v", s, err)
	}
}

func TestConfigFileShapingKeys(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"cfg/config.yaml": {Data: []byte("fields: [pipelines, mrs]\nemoji: true\nactive_days: 7\n")}}
	env := map[string]string{"VIGIE_CONFIG": "/cfg/config.yaml"}
	s, err := config.Load(config.Overrides{}, env, fsys)
	if err != nil || !s.Emoji || s.ActiveDays != 7 || !slices.Equal(s.Fields, []status.FieldName{status.FieldPipelines, status.FieldMRs}) {
		t.Fatalf("file keys: %+v, %v", s, err)
	}
	s, err = config.Load(config.Overrides{NoEmoji: true, Fields: "last"}, env, fsys)
	if err != nil || s.Emoji || !slices.Equal(s.Fields, []status.FieldName{status.FieldLast}) {
		t.Fatalf("flags over file: %+v, %v", s, err)
	}
}
