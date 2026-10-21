package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/sgaunet/vigie/internal/status"
)

// Output selects the record format on stdout.
type Output string

// Output formats.
const (
	OutputText Output = "text"
	OutputJSON Output = "json"
)

// Defaults and limits.
const (
	DefaultInterval   = 5 * time.Second
	MinInterval       = 2 * time.Second
	DefaultTimeout    = 10 * time.Second
	DefaultActiveDays = 90
	DefaultDepth      = 4 // scan mode: levels below --dir searched for clones
)

// Overrides holds the values given on the command line. Zero values (and nil pointers)
// mean "not given".
type Overrides struct {
	Watch    bool
	Interval *time.Duration
	Dir      string
	Remote   string
	Project  string
	Host     string
	Fields   string
	Output   Output
	NoColor  bool
	NoEmoji  bool
	Timeout  *time.Duration
	Verbose  bool
	Quiet    bool
	Depth    *int
}

// Settings is the merged configuration: flags > environment > config file > defaults.
type Settings struct {
	Watch         bool
	Interval      time.Duration
	Dir           string
	Remote        string
	Project       string   // group/…/name or numeric ID; "" = detect
	Host          *url.URL // --host; nil = detect
	FallbackHost  *url.URL // GITLAB_HOST, used only when the host cannot be detected; never github.com
	Fields        []status.FieldName
	Output        Output
	Color         bool
	Emoji         bool
	Timeout       time.Duration
	LogLevel      slog.Level
	ActiveDays    int
	Hosts         map[string]HostProfile // keyed by remote host name, lowercase, no port
	ConfigPath    string                 // config file that was read; "" when none
	ExtraRootsPEM []byte                 // content of SSL_CERT_FILE
	Depth         int                    // scan mode: levels below Dir searched for clones
}

// Load merges overrides, the environment and the config file found through env in fsys.
// fsys is rooted at the filesystem root: "/etc/x" is read as "etc/x".
func Load(ov Overrides, env map[string]string, fsys fs.FS) (Settings, error) {
	s := Settings{
		Interval:   DefaultInterval,
		Dir:        ".",
		Remote:     "origin",
		Fields:     status.DefaultFields(),
		Output:     OutputText,
		Color:      true,
		Emoji:      true,
		Timeout:    DefaultTimeout,
		LogLevel:   slog.LevelWarn,
		ActiveDays: DefaultActiveDays,
		Hosts:      map[string]HostProfile{},
		Depth:      DefaultDepth,
	}
	if err := s.loadFile(env, fsys); err != nil {
		return Settings{}, err
	}
	if err := s.applyEnv(env, fsys); err != nil {
		return Settings{}, err
	}
	if err := s.applyOverrides(ov); err != nil {
		return Settings{}, err
	}
	if err := s.validate(); err != nil {
		return Settings{}, err
	}
	return s, nil
}

func (s *Settings) applyEnv(env map[string]string, fsys fs.FS) error {
	if v := env["VIGIE_INTERVAL"]; v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%w VIGIE_INTERVAL %q: use a duration such as 5s", ErrInvalid, v)
		}
		s.Interval = d
	}
	if env["NO_COLOR"] != "" {
		s.Color = false
	}
	if v := env["GITLAB_HOST"]; v != "" {
		u, err := parseBaseURL(v)
		if err != nil {
			return fmt.Errorf("%w GITLAB_HOST %q: %w", ErrInvalid, v, err)
		}
		s.FallbackHost = u
	}
	return s.loadCertFile(env, fsys)
}

func (s *Settings) applyOverrides(ov Overrides) error {
	s.Watch = ov.Watch
	if ov.Interval != nil {
		s.Interval = *ov.Interval
	}
	if ov.Dir != "" {
		s.Dir = ov.Dir
	}
	if ov.Remote != "" {
		s.Remote = ov.Remote
	}
	s.Project = ov.Project
	if ov.Host != "" {
		u, err := parseBaseURL(ov.Host)
		if err != nil {
			return fmt.Errorf("%w --host %q: %w", ErrInvalid, ov.Host, err)
		}
		s.Host = u
	}
	if ov.Fields != "" {
		f, err := status.ParseFields(ov.Fields)
		if err != nil {
			return fmt.Errorf("%w --fields: %w", ErrInvalid, err)
		}
		s.Fields = f
	}
	if ov.Output != "" {
		s.Output = ov.Output
	}
	if ov.NoColor {
		s.Color = false
	}
	if ov.NoEmoji {
		s.Emoji = false
	}
	if ov.Timeout != nil {
		s.Timeout = *ov.Timeout
	}
	if ov.Depth != nil {
		s.Depth = *ov.Depth
	}
	switch {
	case ov.Verbose:
		s.LogLevel = slog.LevelDebug
	case ov.Quiet:
		s.LogLevel = slog.LevelError
	}
	return nil
}

func (s *Settings) validate() error {
	switch {
	case s.Interval < MinInterval:
		return fmt.Errorf("%w --interval %s: minimum is %s", ErrInvalid, s.Interval, MinInterval)
	case s.Timeout <= 0:
		return fmt.Errorf("%w --timeout %s: use a positive duration such as 10s", ErrInvalid, s.Timeout)
	case s.Output != OutputText && s.Output != OutputJSON:
		return fmt.Errorf("%w --output %q: use text or json", ErrInvalid, s.Output)
	case s.Project != "" && !validProject(s.Project):
		return fmt.Errorf("%w --project %q: use group/name or a numeric ID", ErrInvalid, s.Project)
	case s.Depth < 1:
		return fmt.Errorf("%w --depth %d: minimum is 1", ErrInvalid, s.Depth)
	case s.FallbackHost != nil && hostKey(s.FallbackHost.Host) == gitHubCom:
		// GITLAB_HOST carries GITLAB_TOKEN: it must never reach github.com (FR-003).
		return invalidf("GITLAB_HOST %s is a GitHub host: unset it, and pass --host https://github.com outside a clone", s.FallbackHost)
	}
	return nil
}

func validProject(p string) bool {
	if strings.Trim(p, "0123456789") == "" {
		return true
	}
	segs := strings.Split(p, "/")
	if len(segs) < 2 {
		return false
	}
	for _, seg := range segs {
		if seg == "" {
			return false
		}
	}
	return true
}

var errBaseURL = errors.New("use a URL such as https://gitlab.example.com")

// parseBaseURL accepts an absolute http or https URL with a host; the error is a hint.
func parseBaseURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errBaseURL
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u, nil
}
