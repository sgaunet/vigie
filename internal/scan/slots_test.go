package scan_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sgaunet/vigie/internal/scan"
)

func TestSlotsBound(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := scan.NewSlots(2)
		ctx := context.Background()
		for range 2 {
			if err := s.Acquire(ctx); err != nil {
				t.Fatal(err)
			}
		}
		var third atomic.Bool
		go func() {
			if s.Acquire(ctx) == nil {
				third.Store(true)
			}
		}()
		synctest.Wait()
		if third.Load() {
			t.Fatal("a third slot was granted")
		}
		s.Release()
		synctest.Wait()
		if !third.Load() {
			t.Fatal("Release did not wake the waiter")
		}
	})
}

func TestSlotsHalve(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := scan.NewSlots(4)
		s.Halve()
		if got := s.Limit(); got != 2 {
			t.Fatalf("after one halving: %d", got)
		}
		s.Halve()
		if got := s.Limit(); got != 2 {
			t.Fatalf("halved twice within a second: %d", got)
		}
		for range 2 {
			time.Sleep(time.Second)
			s.Halve()
			if got := s.Limit(); got != 1 {
				t.Fatalf("limit = %d, want 1 and never below", got)
			}
		}
	})
}

func TestSlotsHalveWhileInUse(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := scan.NewSlots(4)
		for range 4 {
			if err := s.Acquire(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		s.Halve()
		var got atomic.Bool
		go func() {
			if s.Acquire(context.Background()) == nil {
				got.Store(true)
			}
		}()
		for inUse := 3; inUse >= 2; inUse-- {
			s.Release()
			synctest.Wait()
			if got.Load() {
				t.Fatalf("slot granted with %d in use and a limit of 2", inUse)
			}
		}
		s.Release()
		synctest.Wait()
		if !got.Load() {
			t.Fatal("no slot granted with 1 in use")
		}
	})
}

func TestSlotsCanceled(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := scan.NewSlots(1)
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if err := s.Acquire(canceled); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled context: got %v", err)
		}
		if err := s.Acquire(context.Background()); err != nil {
			t.Fatal("the cancelled Acquire took the slot")
		}

		ctx, cancel := context.WithCancel(context.Background())
		errc := make(chan error, 1)
		go func() { errc <- s.Acquire(ctx) }()
		synctest.Wait()
		cancel()
		if err := <-errc; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled while waiting: got %v", err)
		}
		s.Release()
		if err := s.Acquire(context.Background()); err != nil {
			t.Fatal("the slot was not given back")
		}
	})
}
