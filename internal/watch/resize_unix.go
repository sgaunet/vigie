//go:build unix

package watch

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// ResizeSignal returns a channel that receives a value when the terminal is resized
// (SIGWINCH), until ctx is done.
func ResizeSignal(ctx context.Context) <-chan struct{} {
	sig := make(chan os.Signal, 1)
	out := make(chan struct{}, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	go func() {
		defer signal.Stop(sig)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sig:
				select {
				case out <- struct{}{}:
				default: // a redraw is already pending
				}
			}
		}
	}()
	return out
}
