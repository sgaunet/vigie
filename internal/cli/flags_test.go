package cli_test

import (
	"errors"
	"flag"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/cli"
	"github.com/sgaunet/vigie/internal/config"
)

func dur(d time.Duration) *time.Duration { return &d }

func TestParseLongAndShortPairs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		long, short []string
		want        config.Overrides
	}{
		{[]string{"--watch"}, []string{"-w"}, config.Overrides{Watch: true}},
		{[]string{"--interval", "3s"}, []string{"-i", "3s"}, config.Overrides{Interval: dur(3 * time.Second)}},
		{[]string{"--dir", "/src"}, []string{"-C", "/src"}, config.Overrides{Dir: "/src"}},
		{[]string{"--remote", "upstream"}, []string{"-r", "upstream"}, config.Overrides{Remote: "upstream"}},
		{[]string{"--project", "g/p"}, []string{"-p", "g/p"}, config.Overrides{Project: "g/p"}},
		{[]string{"--fields", "mrs"}, []string{"-f", "mrs"}, config.Overrides{Fields: "mrs"}},
		{[]string{"--output", "json"}, []string{"-o", "json"}, config.Overrides{Output: config.OutputJSON}},
		{[]string{"--verbose"}, []string{"-v"}, config.Overrides{Verbose: true}},
		{[]string{"--quiet"}, []string{"-q"}, config.Overrides{Quiet: true}},
	}
	for _, c := range cases {
		for _, args := range [][]string{c.long, c.short} {
			cmd, ov, err := cli.Parse(args)
			if err != nil {
				t.Errorf("Parse(%v): %v", args, err)
				continue
			}
			if cmd != cli.CommandStatus || !reflect.DeepEqual(ov, c.want) {
				t.Errorf("Parse(%v) = %v %+v, want %+v", args, cmd, ov, c.want)
			}
		}
	}
}

func TestParseLongOnly(t *testing.T) {
	t.Parallel()
	_, ov, err := cli.Parse([]string{"--host", "https://h", "--no-color", "--no-emoji", "--timeout", "4s"})
	if err != nil {
		t.Fatal(err)
	}
	want := config.Overrides{Host: "https://h", NoColor: true, NoEmoji: true, Timeout: dur(4 * time.Second)}
	if !reflect.DeepEqual(ov, want) {
		t.Fatalf("got %+v, want %+v", ov, want)
	}
}

func TestParseEqualsForm(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-i=3s"}, {"-i", "3s"}, {"--interval=3s"}} {
		_, ov, err := cli.Parse(args)
		if err != nil || ov.Interval == nil || *ov.Interval != 3*time.Second {
			t.Errorf("Parse(%v) = %+v, %v", args, ov, err)
		}
	}
}

func TestParseUsageErrors(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"-wv"},
		{"--bogus"},
		{"-i", "soon"},
		{"--timeout", "x"},
		{"-o", "yaml"},
		{"extra"},
		{"-v", "-q"},
		{"version", "extra"},
		{"version", "-o", "yaml"},
		{"version", "--watch"},
	} {
		_, _, err := cli.Parse(args)
		if !errors.Is(err, cli.ErrUsage) {
			t.Errorf("Parse(%v): got %v, want ErrUsage", args, err)
		}
	}
}

func TestParseHelp(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-h"}, {"--help"}, {"-w", "--help"}} {
		if _, _, err := cli.Parse(args); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("Parse(%v): got %v, want flag.ErrHelp", args, err)
		}
	}
}

func TestParseVersion(t *testing.T) {
	t.Parallel()
	cmd, ov, err := cli.Parse([]string{"version"})
	if err != nil || cmd != cli.CommandVersion || ov.Output != "" {
		t.Fatalf("version: %v %+v %v", cmd, ov, err)
	}
	for _, args := range [][]string{{"version", "-o", "json"}, {"version", "--output=json"}} {
		cmd, ov, err = cli.Parse(args)
		if err != nil || cmd != cli.CommandVersion || ov.Output != config.OutputJSON {
			t.Fatalf("%v: %v %+v %v", args, cmd, ov, err)
		}
	}
}

func TestHelpText(t *testing.T) {
	t.Parallel()
	h := cli.Help()
	for _, want := range []string{
		"Settings: flags > environment > config file > detected from the repository > defaults.",
		"Host:     --host > detected from the remote (via config \"hosts\") > GITLAB_HOST (fallback only).",
		"Token:    host entry token_env / token_command > GITLAB_TOKEN.",
		"GITLAB_TOKEN", "GITLAB_HOST", "VIGIE_INTERVAL", "VIGIE_CONFIG", "NO_COLOR", "SSL_CERT_FILE",
		"--watch", "--interval", "--dir", "--remote", "--project", "--host", "--fields", "--output",
		"--no-color", "--no-emoji", "--timeout", "--verbose", "--quiet", "--help",
		"  0  ", "  1  ", "  2  ", "  3  ", "  4  ", "  5  ",
	} {
		if !contains(h, want) {
			t.Errorf("help text lacks %q", want)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

