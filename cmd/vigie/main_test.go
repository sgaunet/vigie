package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binary string

// TestMain builds the vigie binary once; the end-to-end tests run it as a user would.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "vigie-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "vigie")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// runVigie runs the binary in dir with a clean environment plus env.
func runVigie(t *testing.T, dir string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run vigie: %v", err)
		}
		code = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func TestE2EVersion(t *testing.T) {
	t.Parallel()
	out, errOut, code := runVigie(t, t.TempDir(), nil, "version")
	if code != 0 || errOut != "" || !strings.HasPrefix(out, "vigie ") || strings.Count(out, "\n") != 1 {
		t.Fatalf("version: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	out, _, code = runVigie(t, t.TempDir(), nil, "version", "-o", "json")
	var v map[string]string
	if err := json.Unmarshal([]byte(out), &v); err != nil || code != 0 {
		t.Fatalf("version json: %v code=%d %q", err, code, out)
	}
	for _, k := range []string{"version", "commit", "date"} {
		if v[k] == "" {
			t.Errorf("version json lacks %q: %q", k, out)
		}
	}
}

func TestE2EHelp(t *testing.T) {
	t.Parallel()
	out, errOut, code := runVigie(t, t.TempDir(), nil, "--help")
	if code != 0 || errOut != "" {
		t.Fatalf("help: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"  0  ", "  1  ", "  2  ", "  3  ", "  4  ", "  5  ", "Settings: flags > environment", "GITLAB_HOST (fallback only)"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestE2EUsageError(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--bogus"}, {"-wv"}} {
		out, errOut, code := runVigie(t, t.TempDir(), nil, args...)
		if code != 2 || out != "" {
			t.Fatalf("%v: code=%d stdout=%q", args, code, out)
		}
		lines := strings.Split(strings.TrimSuffix(errOut, "\n"), "\n")
		if len(lines) != 2 || lines[1] != "Run 'vigie --help' for usage." {
			t.Fatalf("%v: stderr=%q", args, errOut)
		}
	}
}
