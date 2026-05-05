// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"
	"testing"
	"time"
)

func TestGatewayResolverEnqueuesOnce(t *testing.T) {
	exec := &fakeExecutor{}
	metrics := NewMetrics()
	cache := NewCache(8)
	writer := NewWriter(exec, &metrics, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go writer.Run(ctx)

	g := NewGatewayResolver(cache, writer)
	stmtFP := []byte("fp1")

	id1 := g.Resolve(stmtFP, "myapp")
	id2 := g.Resolve(stmtFP, "myapp") // cache hit; no new write
	if id1 != id2 {
		t.Fatalf("expected same ID, got %d and %d", id1, id2)
	}

	// Wait briefly for the writer to drain.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if exec.writeCount() == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected exactly 1 write, got %d", exec.writeCount())
}
