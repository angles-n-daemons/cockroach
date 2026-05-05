// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import "testing"

func TestCachePutGet(t *testing.T) {
	c := NewCache(8)
	id := ComputeID([]byte("fp1"), "app1")
	entry := Entry{StmtFingerprintID: []byte("fp1"), AppName: "app1"}

	c.Put(id, entry)
	got, ok := c.Get(id)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if got.AppName != "app1" {
		t.Fatalf("expected app1, got %s", got.AppName)
	}
}

func TestCacheEvictsLRU(t *testing.T) {
	c := NewCache(2)
	c.Put(ID(1), Entry{AppName: "a"})
	c.Put(ID(2), Entry{AppName: "b"})
	c.Put(ID(3), Entry{AppName: "c"}) // evicts ID 1

	if _, ok := c.Get(ID(1)); ok {
		t.Fatalf("expected ID 1 to be evicted")
	}
	if _, ok := c.Get(ID(2)); !ok {
		t.Fatalf("expected ID 2 to remain")
	}
	if _, ok := c.Get(ID(3)); !ok {
		t.Fatalf("expected ID 3 to remain")
	}
}

func TestCacheUpdatesRecencyOnGet(t *testing.T) {
	c := NewCache(2)
	c.Put(ID(1), Entry{AppName: "a"})
	c.Put(ID(2), Entry{AppName: "b"})
	// Touching ID 1 makes ID 2 the LRU.
	if _, ok := c.Get(ID(1)); !ok {
		t.Fatalf("expected hit on ID 1")
	}
	c.Put(ID(3), Entry{AppName: "c"}) // should evict ID 2 now

	if _, ok := c.Get(ID(1)); !ok {
		t.Fatalf("expected ID 1 to remain (recently used)")
	}
	if _, ok := c.Get(ID(2)); ok {
		t.Fatalf("expected ID 2 to be evicted")
	}
}
