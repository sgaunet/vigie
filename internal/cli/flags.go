package cli

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/sgaunet/vigie/internal/config"
)

//go:embed help.txt
var helpText string

// Help returns the text printed by --help.
func Help() string { return helpText }

// Command is the action selected on the command line.
type Command int

// Commands.
const (
	CommandStatus  Command = iota // default: print the status line
	CommandVersion                // vigie version
)

// Parse parses the arguments (without the program name). It returns flag.ErrHelp for
// -h/--help, and an error wrapping ErrUsage for anything it cannot accept.
func Parse(args []string) (Command, config.Overrides, error) {
	var (
		ov                config.Overrides
		interval, timeout time.Duration
		output            string
		depth             int
	)
	fs := newFlagSet("vigie")
	boolPair(fs, &ov.Watch, "watch", "w")
	durationPair(fs, &interval, "interval", "i")
	stringPair(fs, &ov.Dir, "dir", "C")
	stringPair(fs, &ov.Remote, "remote", "r")
	stringPair(fs, &ov.Project, "project", "p")
	stringPair(fs, &ov.Host, "host", "")
	stringPair(fs, &ov.Fields, "fields", "f")
	stringPair(fs, &output, "output", "o")
	boolPair(fs, &ov.NoColor, "no-color", "")
	boolPair(fs, &ov.NoEmoji, "no-emoji", "")
	durationPair(fs, &timeout, "timeout", "")
	fs.IntVar(&depth, "depth", 0, "")
	boolPair(fs, &ov.Verbose, "verbose", "v")
	boolPair(fs, &ov.Quiet, "quiet", "q")

	if err := parse(fs, args); err != nil {
		return 0, config.Overrides{}, err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["interval"] || set["i"] {
		ov.Interval = &interval
	}
	if set["timeout"] {
		ov.Timeout = &timeout
	}
	if set["depth"] {
		ov.Depth = &depth
	}
	out, err := parseOutput(output)
	if err != nil {
		return 0, config.Overrides{}, err
	}
	ov.Output = out
	if ov.Verbose && ov.Quiet {
		return 0, config.Overrides{}, fmt.Errorf("%w: --verbose and --quiet are exclusive", ErrUsage)
	}

	rest := fs.Args()
	if len(rest) == 0 {
		return CommandStatus, ov, nil
	}
	if rest[0] != "version" {
		return 0, config.Overrides{}, fmt.Errorf("%w: unexpected argument %q", ErrUsage, rest[0])
	}
	return parseVersion(rest[1:])
}

func parseVersion(args []string) (Command, config.Overrides, error) {
	var output string
	fs := newFlagSet("vigie version")
	stringPair(fs, &output, "output", "o")
	if err := parse(fs, args); err != nil {
		return 0, config.Overrides{}, err
	}
	if fs.NArg() > 0 {
		return 0, config.Overrides{}, fmt.Errorf("%w: unexpected argument %q", ErrUsage, fs.Arg(0))
	}
	out, err := parseOutput(output)
	if err != nil {
		return 0, config.Overrides{}, err
	}
	return CommandVersion, config.Overrides{Output: out}, nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	err := fs.Parse(args)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		return flag.ErrHelp
	default:
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
}

func parseOutput(s string) (config.Output, error) {
	switch o := config.Output(s); o {
	case "", config.OutputText, config.OutputJSON:
		return o, nil
	default:
		return "", fmt.Errorf("%w: --output %q: use text or json", ErrUsage, s)
	}
}

// The helpers below bind one variable to a long and an optional short name; the usage
// strings are unused because --help prints the embedded text.

func boolPair(fs *flag.FlagSet, p *bool, long, short string) {
	fs.BoolVar(p, long, false, "")
	if short != "" {
		fs.BoolVar(p, short, false, "")
	}
}

func stringPair(fs *flag.FlagSet, p *string, long, short string) {
	fs.StringVar(p, long, "", "")
	if short != "" {
		fs.StringVar(p, short, "", "")
	}
}

func durationPair(fs *flag.FlagSet, p *time.Duration, long, short string) {
	fs.DurationVar(p, long, 0, "")
	if short != "" {
		fs.DurationVar(p, short, 0, "")
	}
}
