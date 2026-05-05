// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"
	"time"
)

// reader is the minimal interface for the sampler to fetch rows from
// system.execution_attributes. Real implementations use isql.DB; tests
// use a fake.
type reader interface {
	readExecutionAttributes(ctx context.Context, id ID) (Entry, bool, error)
}

// SamplerResolver resolves IDs to Entries at sample-write time.
//
// The resolver is reused across sampler ticks. Call BeginTick at the
// start of each sampler tick to reset the per-tick miss budget; call
// Resolve once per sample being written.
//
// Cache hits are essentially free. Cache misses fall back to a bounded
// synchronous KV read; on per-miss timeout or per-tick budget
// exhaustion, the resolver returns (Entry{}, false) and increments the
// UnresolvedSamples counter, leaving it to the caller to record the
// sample with NULL denormalized columns.
type SamplerResolver struct {
	cache       *Cache
	reader      reader
	metrics     *Metrics
	readTimeout func() time.Duration
	tickBudget  func() time.Duration
	tickSpent   time.Duration
}

// NewSamplerResolver constructs a resolver bound to the per-node cache
// and a system-table reader. The timeout and budget settings are
// supplied as closures so cluster-setting changes take effect without
// re-constructing the resolver.
func NewSamplerResolver(
	cache *Cache,
	r reader,
	metrics *Metrics,
	readTimeout func() time.Duration,
	tickBudget func() time.Duration,
) *SamplerResolver {
	return &SamplerResolver{
		cache:       cache,
		reader:      r,
		metrics:     metrics,
		readTimeout: readTimeout,
		tickBudget:  tickBudget,
	}
}

// BeginTick resets the per-tick miss-resolution budget. Call once at
// the start of each sampler tick before Resolve calls.
func (s *SamplerResolver) BeginTick() {
	s.tickSpent = 0
}

// Resolve returns the Entry for id. On cache miss, attempts a bounded
// synchronous KV read. If the per-miss timeout fires, the per-tick
// budget is exhausted, or the row is genuinely absent, returns
// (Entry{}, false) and increments UnresolvedSamples.
func (s *SamplerResolver) Resolve(ctx context.Context, id ID) (Entry, bool) {
	if e, ok := s.cache.Get(id); ok {
		return e, true
	}
	if s.tickSpent >= s.tickBudget() {
		s.metrics.UnresolvedSamples.Inc(1)
		return Entry{}, false
	}
	timeout := s.readTimeout()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	entry, ok, err := s.reader.readExecutionAttributes(cctx, id)
	s.tickSpent += time.Since(start)
	if err != nil || !ok {
		s.metrics.UnresolvedSamples.Inc(1)
		return Entry{}, false
	}
	s.cache.Put(id, entry)
	return entry, true
}
