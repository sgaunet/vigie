//go:build unix

package main_test

import (
	"bufio"
	"bytes"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sgaunet/vigie/internal/gitlab/gitlabtest"
)

// watcher is a running `vigie --watch` whose stdout lines arrive on lines.
type watcher struct {
	cmd    *exec.Cmd
	lines  chan string
	done   chan struct{}
	mu     sync.Mutex
	stdout strings.Builder
	stderr bytes.Buffer
}

func startWatch(t *testing.T, e *e2e, args ...string) *watcher {
	t.Helper()
	return startWatchIn(t, e.repo, e.env, args...)
}

// startWatchIn runs `vigie -w -i 2s` in dir with a clean environment plus env.
func startWatchIn(t *testing.T, dir string, env []string, args ...string) *watcher {
	t.Helper()
	w := &watcher{lines: make(chan string, 100), done: make(chan struct{})}
	w.cmd = exec.Command(binary, append([]string{"-w", "-i", "2s"}, args...)...)
	w.cmd.Dir = dir
	w.cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}, env...)
	w.cmd.Stderr = &lockedWriter{mu: &w.mu, w: &w.stderr}
	out, err := w.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(w.done)
		r := bufio.NewReader(out)
		for {
			line, err := r.ReadString('\n')
			w.mu.Lock()
			w.stdout.WriteString(line)
			w.mu.Unlock()
			if err != nil {
				return
			}
			w.lines <- strings.TrimSuffix(line, "\n")
		}
	}()
	t.Cleanup(func() { _ = w.cmd.Process.Kill(); _ = w.cmd.Wait() })
	return w
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (w *watcher) stderrString() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stderr.String()
}

// next waits for the next stdout line.
func (w *watcher) next(t *testing.T, within time.Duration) string {
	t.Helper()
	select {
	case l := <-w.lines:
		if strings.Contains(l, e2eToken) || strings.Contains(l, ghToken) {
			t.Fatalf("token leaked: %q", l)
		}
		return l
	case <-time.After(within):
		t.Fatalf("no line within %v (stderr %q)", within, w.stderrString())
	}
	return ""
}

// quiet asserts no line arrives for d.
func (w *watcher) quiet(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case l := <-w.lines:
		t.Fatalf("unexpected line %q", l)
	case <-time.After(d):
	}
}

// wait waits for the process to exit and returns its code.
func (w *watcher) wait(t *testing.T, within time.Duration) int {
	t.Helper()
	exited := make(chan error, 1)
	go func() { <-w.done; exited <- w.cmd.Wait() }()
	select {
	case err := <-exited:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	case <-time.After(within):
		t.Fatalf("still running after %v", within)
	}
	return -1
}

func TestE2EWatchPrintsOnChange(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	w := startWatch(t, e)
	if l := w.next(t, 5*time.Second); !strings.HasSuffix(l, "mr 5 is 17") {
		t.Fatalf("first line = %q", l)
	}
	w.quiet(t, 4500*time.Millisecond) // two refreshes, same values

	p := gitlabtest.Fixture(time.Now())
	p.OpenMRs = 6
	e.srv.SetProject(p)
	if l := w.next(t, 5*time.Second); !strings.Contains(l, "mr 6") {
		t.Fatalf("changed line = %q", l)
	}

	_ = w.cmd.Process.Signal(syscall.SIGINT)
	if code := w.wait(t, 3*time.Second); code != 0 {
		t.Fatalf("exit after SIGINT = %d, stderr=%q", code, w.stderrString())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if out := w.stdout.String(); !strings.HasSuffix(out, "\n") || strings.Contains(out, "\x1b") {
		t.Fatalf("stdout = %q", out)
	}
	if strings.Contains(w.stderr.String(), e2eToken) {
		t.Fatal("token leaked on stderr")
	}
}

func TestE2EWatchJSONStale(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	w := startWatch(t, e, "-o", "json")
	if l := w.next(t, 5*time.Second); !strings.Contains(l, `"stale":false`) {
		t.Fatalf("first record = %q", l)
	}
	e.srv.SetAll(gitlabtest.Behavior{Status: http.StatusBadGateway})
	l := w.next(t, 5*time.Second)
	if !strings.Contains(l, `"stale":true`) || !strings.Contains(l, `"stale_since"`) ||
		!strings.Contains(l, `"field":"refresh"`) || !strings.Contains(l, `"opened":5`) {
		t.Fatalf("stale record = %q", l)
	}
	w.quiet(t, 4500*time.Millisecond) // further failures write nothing
	e.srv.SetAll(gitlabtest.Behavior{})
	if l := w.next(t, 12*time.Second); !strings.Contains(l, `"stale":false`) || strings.Contains(l, "stale_since") {
		t.Fatalf("recovered record = %q", l)
	}
	_ = w.cmd.Process.Signal(syscall.SIGTERM)
	if code := w.wait(t, 3*time.Second); code != 0 {
		t.Fatalf("exit after SIGTERM = %d", code)
	}
}

func TestE2EWatchStopsOnUnauthorized(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	w := startWatch(t, e)
	w.next(t, 5*time.Second)
	e.srv.SetAll(gitlabtest.Behavior{Status: http.StatusUnauthorized})
	if code := w.wait(t, 4*time.Second); code != 3 {
		t.Fatalf("exit = %d, want 3", code)
	}
	if got := w.stderrString(); !strings.HasPrefix(got, "token rejected by ") {
		t.Fatalf("stderr = %q", got)
	}
}
