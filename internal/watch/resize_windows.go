//go:build windows

package watch

import "context"

// ResizeSignal never fires on Windows, which has no SIGWINCH; the display re-reads the
// width at each draw instead.
func ResizeSignal(_ context.Context) <-chan struct{} { return make(chan struct{}) }
