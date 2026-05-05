// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeExecutor struct {
	mu    sync.Mutex
	wrote []writeRequest
}

func (f *fakeExecutor) insertExecutionAttributes(
	_ context.Context, req writeRequest,
) (existing Entry, conflict bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wrote = append(f.wrote, req)
	return Entry{}, false, nil
}

func (f *fakeExecutor) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.wrote)
}

func TestWriterDrainsQueue(t *testing.T) {
	exec := &fakeExecutor{}
	metrics := NewMetrics()
	w := NewWriter(exec, &metrics, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Enqueue(writeRequest{ID: 1, Entry: Entry{StmtFingerprintID: []byte("fp"), AppName: "a"}})
	w.Enqueue(writeRequest{ID: 2, Entry: Entry{StmtFingerprintID: []byte("fp2"), AppName: "b"}})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if exec.writeCount() == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected 2 writes, got %d", exec.writeCount())
}

func TestWriterOverflowDropsAndCounts(t *testing.T) {
	exec := &fakeExecutor{}
	metrics := NewMetrics()
	w := NewWriter(exec, &metrics, 1)

	// Don't start the writer goroutine -- the queue can't drain.
	w.Enqueue(writeRequest{ID: 1, Entry: Entry{}}) // fits
	w.Enqueue(writeRequest{ID: 2, Entry: Entry{}}) // dropped

	if got := metrics.Discarded.Count(); got != 1 {
		t.Fatalf("expected 1 discard, got %d", got)
	}
}
