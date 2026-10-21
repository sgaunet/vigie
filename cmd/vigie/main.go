// Command vigie prints a one-line status of the GitLab project or GitHub repository behind a
// git clone.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/sgaunet/vigie/internal/cli"
	"github.com/sgaunet/vigie/internal/config"
)

// Set by -ldflags "-X main.version=… -X main.commit=… -X main.date=…".
var (
	version = ""
	commit  = ""
	date    = ""
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := exitFailure
	defer func() {
		// A panic has already unwound run, whose deferred calls restored the terminal.
		if r := recover(); r != nil {
			stop()
			fmt.Fprintf(os.Stderr, "vigie: internal error: %v\n", r)
			os.Exit(exitFailure)
		}
		stop()
		os.Exit(code)
	}()
	code = exitCode(run(ctx, os.Args[1:], os.Stdout, os.Stderr, envMap()))
}

// run executes one invocation and returns the error that decides the exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, env map[string]string) error {
	cmd, ov, err := cli.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		_, err := io.WriteString(stdout, cli.Help())
		return err
	case err != nil:
		fmt.Fprintf(stderr, "%v\nRun 'vigie --help' for usage.\n", err)
		return err
	case cmd == cli.CommandVersion:
		return printVersion(stdout, ov.Output)
	}
	err = cli.Run(ctx, ov, cli.IO{Stdout: stdout, Stderr: stderr, Env: env, FS: config.HostFS()})
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, err)
	}
	return err
}

func printVersion(w io.Writer, out config.Output) error {
	v, c, d := buildInfo()
	if out == config.OutputJSON {
		return json.NewEncoder(w).Encode(struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
			Date    string `json:"date"`
		}{v, c, d})
	}
	_, err := fmt.Fprintf(w, "vigie %s (commit %s, built %s)\n", v, c, d)
	return err
}

// buildInfo returns the ldflags values, falling back to the module build info that
// `go install` records.
func buildInfo() (string, string, string) {
	v, c, d := version, commit, date
	if info, ok := debug.ReadBuildInfo(); ok {
		if v == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		for _, s := range info.Settings {
			switch {
			case s.Key == "vcs.revision" && c == "":
				c = s.Value[:min(len(s.Value), 7)]
			case s.Key == "vcs.time" && d == "":
				d = s.Value
			}
		}
	}
	return orDefault(v, "dev"), orDefault(c, "none"), orDefault(d, "unknown")
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func envMap() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}
