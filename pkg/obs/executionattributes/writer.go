// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"
	"time"

	"github.com/cockroachdb/cockroach/pkg/util/log"
)

// writeRequest is one pending durable write to system.execution_attributes.
type writeRequest struct {
	ID    ID
	Entry Entry
}

// executor is the minimal interface the Writer needs from the system-table
// backend. The real implementation uses an InternalExecutor; tests use a
// fake.
type executor interface {
	// insertExecutionAttributes inserts the row idempotently. If a row
	// already exists at the same ID with a different (fp, app_name)
	// tuple, returns the existing row with conflict=true.
	insertExecutionAttributes(ctx context.Context, req writeRequest) (existing Entry, conflict bool, err error)
}

// Writer drains pending writes onto the system.execution_attributes table.
//
// The queue is bounded; on overflow, the new entry is dropped and the
// Discarded metric increments. In-flight writes proceed unimpeded so a
// burst doesn't cascade into starvation.
type Writer struct {
	exec    executor
	metrics *Metrics
	queue   chan writeRequest
}

// NewWriter creates a Writer with a bounded queue.
func NewWriter(exec executor, metrics *Metrics, queueSize int) *Writer {
	return &Writer{
		exec:    exec,
		metrics: metrics,
		queue:   make(chan writeRequest, queueSize),
	}
}

// Enqueue adds a write request, dropping it if the queue is full.
func (w *Writer) Enqueue(req writeRequest) {
	select {
	case w.queue <- req:
	default:
		w.metrics.Discarded.Inc(1)
	}
}

// Run drains the queue until ctx is cancelled.
func (w *Writer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-w.queue:
			w.process(ctx, req)
		}
	}
}

func (w *Writer) process(ctx context.Context, req writeRequest) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		existing, conflict, err := w.exec.insertExecutionAttributes(ctx, req)
		if err == nil {
			if conflict {
				w.metrics.Collisions.Inc(1)
				log.Dev.Warningf(ctx,
					"execution_attributes hash collision: id=%d existing=%+v new=%+v",
					req.ID, existing, req.Entry)
			}
			return
		}
		lastErr = err
		time.Sleep(time.Duration(50*(1<<attempt)) * time.Millisecond)
	}
	log.Dev.Warningf(ctx,
		"execution_attributes write failed after retries: id=%d err=%v",
		req.ID, lastErr)
}
