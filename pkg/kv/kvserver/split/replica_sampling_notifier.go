package split

import (
	"context"
	"sync"
	"time"

	"github.com/cockroachdb/cockroach/pkg/util/log"
)

// ReplicaSamplingNotifier is a small utility embedded in each of the replica
// deciders which allows subscribers to know when sampling kicks on in the
// system.
type ReplicaSamplingNotifier struct {
	mu    sync.Mutex
	count int
	on    chan struct{}
}

// NewReplicaSamplingNotifier creates a new ReplicaSamplingNotifier with a
// channel for notifications.
func NewReplicaSamplingNotifier() *ReplicaSamplingNotifier {
	// You might want a buffered channel if you don’t want Inc/Dec
	// to block when there is no active reader. e.g., make(chan bool, 1)
	return &ReplicaSamplingNotifier{
		on: make(chan struct{}),
	}
}

// Inc increments the count. If we transition from 0 to 1, send `true`.
func (n *ReplicaSamplingNotifier) Inc(ctx context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// If count was 0, we are transitioning to a nonzero state.
	if n.count == 0 {
		n.publish(ctx)
	}
	n.count++
}

// Dec decrements the count. If we transition from 1 to 0, send `false`.
func (n *ReplicaSamplingNotifier) Dec(ctx context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.count == 0 {
		log.Telemetry.Warning(ctx, "tried to decrement ReplicaSamplingNotifier notifier when at 0")
		return
	}
	n.count--
}

// publish sends a message on the channel if there is a receiver, or drops it
// otherwise.
func (n *ReplicaSamplingNotifier) publish(ctx context.Context) {
	select {
	case n.on <- struct{}{}:
		return
	case <-time.After(time.Millisecond):
		log.Telemetry.Info(ctx, "unable to send replica sampling notification")
		return
	}
}

// Subscribe returns the underlying channel so callers can receive notifications.
func (n *ReplicaSamplingNotifier) Subscribe() <-chan struct{} {
	return n.on
}
