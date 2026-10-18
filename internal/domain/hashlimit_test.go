package domain

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

// holdHashSlot fills one of limiter's slots until the returned func is called.
func holdHashSlot(t *testing.T, limiter *hashLimiter) (release func()) {
	t.Helper()
	held, freed := make(chan struct{}), make(chan struct{})
	go func() {
		_ = limiter.run(context.Background(), func() {
			close(held)
			<-freed
		})
	}()
	<-held
	return func() { close(freed) }
}

// With every slot held, a hash waits for one to be freed, then runs.
func TestAHashWaitsForAFreeSlot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newHashLimiter(1)
		release := holdHashSlot(t, limiter)

		ran := make(chan struct{})
		done := make(chan error)
		go func() { done <- limiter.run(context.Background(), func() { close(ran) }) }()
		synctest.Wait()
		select {
		case <-ran:
			t.Fatal("the hash ran while the only slot was held")
		default:
		}

		release()
		if err := <-done; err != nil {
			t.Fatalf("run once the slot was freed: %v", err)
		}
		select {
		case <-ran:
		default:
			t.Error("run returned without running the hash")
		}
	})
}

// A caller whose context ends while it waits gives up with the context's error
// and never hashes.
func TestAHashWaitingWhenItsContextEndsNeverRuns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newHashLimiter(1)
		release := holdHashSlot(t, limiter)
		defer release()

		ctx, cancel := context.WithCancel(context.Background())
		ran := false
		done := make(chan error)
		go func() { done <- limiter.run(ctx, func() { ran = true }) }()
		synctest.Wait()

		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("run cancelled while waiting: %v, want context.Canceled", err)
		}
		if ran {
			t.Error("the hash ran though its context ended while it waited")
		}
	})
}
