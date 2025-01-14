package split

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestNoBlockOnNoReceiver ensures that an Inc/Dec call does not block even if
// there is no one reading from the channel. With the 'select { case n.ch <- v: default: }'
// in publish, sends should be dropped if nobody is receiving.
func TestNoBlockOnNoReceiver(t *testing.T) {
	n := NewReplicaSamplingNotifier()
	ctx := context.Background()

	// We'll do a quick check that Inc does not block.
	doneCh := make(chan struct{})
	go func() {
		n.Inc(ctx) // if there's no receiver, publish should fallback to default case.
		close(doneCh)
	}()
	select {
	case <-doneCh:
		// Good: we did not block
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Inc blocked when there was no receiver (unexpected)")
	}
}

// TestTransitionNotification ensures that going from count=0→1 emits a value,
// signifying that sampling has turned on in the system.
func TestTransitionNotification(t *testing.T) {
	n := NewReplicaSamplingNotifier()
	ctx := context.Background()

	// We'll read from the channel in a goroutine so we don't block the main test.
	var (
		notifications []struct{}
		mu            sync.Mutex
	)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case val := <-n.Subscribe():
				mu.Lock()
				notifications = append(notifications, val)
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()

	// 0 -> 1 => should publish msg
	n.Inc(ctx)

	// 1 -> 2 => no message
	n.Inc(ctx)

	// 2 -> 1 => no message
	n.Dec(ctx)

	// 1 -> 2 => no message
	n.Inc(ctx)

	// 2 -> 1 => no message
	n.Dec(ctx)

	// 1 -> 0 => no message
	n.Dec(ctx)

	// 0 -> 1 => should publish msg
	n.Inc(ctx)

	// finish operations
	done <- struct{}{}

	mu.Lock()
	defer mu.Unlock()
	if len(notifications) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(notifications))
	}
}

// TestDecrementBelowZero verifies that the system doesn't panic or allow
// count to go below zero.
func TestDecrementBelowZero(t *testing.T) {
	n := NewReplicaSamplingNotifier()
	ctx := context.Background()

	// We'll read from the channel in a goroutine so we don't block the main test.
	var (
		notifications []struct{}
		mu            sync.Mutex
	)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case val := <-n.Subscribe():
				mu.Lock()
				notifications = append(notifications, val)
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()

	// 0 -> 0 => no message
	n.Dec(ctx)

	// 0 -> 1 => should publish message
	n.Inc(ctx)

	// finish operations
	done <- struct{}{}

	mu.Lock()
	defer mu.Unlock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notifications, got %d", len(notifications))
	}
}
