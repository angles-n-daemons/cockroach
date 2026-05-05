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

type fakeReader struct {
	entries map[ID]Entry
	calls   int
}

func (f *fakeReader) readExecutionAttributes(_ context.Context, id ID) (Entry, bool, error) {
	f.calls++
	if e, ok := f.entries[id]; ok {
		return e, true, nil
	}
	return Entry{}, false, nil
}

func TestResolverUsesCacheWhenWarm(t *testing.T) {
	cache := NewCache(8)
	cache.Put(42, Entry{AppName: "warm"})
	r := &fakeReader{}
	metrics := NewMetrics()
	res := NewSamplerResolver(cache, r, &metrics,
		func() time.Duration { return 50 * time.Millisecond },
		func() time.Duration { return 200 * time.Millisecond })
	res.BeginTick()

	got, ok := res.Resolve(context.Background(), 42)
	if !ok || got.AppName != "warm" {
		t.Fatalf("expected warm, got %+v ok=%v", got, ok)
	}
	if r.calls != 0 {
		t.Fatalf("expected 0 reader calls on cache hit, got %d", r.calls)
	}
}

func TestResolverFetchesOnMiss(t *testing.T) {
	cache := NewCache(8)
	r := &fakeReader{entries: map[ID]Entry{99: {AppName: "fetched"}}}
	metrics := NewMetrics()
	res := NewSamplerResolver(cache, r, &metrics,
		func() time.Duration { return 50 * time.Millisecond },
		func() time.Duration { return 200 * time.Millisecond })
	res.BeginTick()

	got, ok := res.Resolve(context.Background(), 99)
	if !ok || got.AppName != "fetched" {
		t.Fatalf("expected fetched, got %+v ok=%v", got, ok)
	}
	// Second call hits the now-warmed cache.
	res.Resolve(context.Background(), 99)
	if r.calls != 1 {
		t.Fatalf("expected 1 reader call across two resolves, got %d", r.calls)
	}
}

func TestResolverCountsUnresolvedOnGenuineMiss(t *testing.T) {
	cache := NewCache(8)
	r := &fakeReader{entries: map[ID]Entry{}}
	metrics := NewMetrics()
	res := NewSamplerResolver(cache, r, &metrics,
		func() time.Duration { return 50 * time.Millisecond },
		func() time.Duration { return 200 * time.Millisecond })
	res.BeginTick()

	_, ok := res.Resolve(context.Background(), 7)
	if ok {
		t.Fatalf("expected miss, got hit")
	}
	if got := metrics.UnresolvedSamples.Count(); got != 1 {
		t.Fatalf("expected unresolved_samples=1, got %d", got)
	}
}
