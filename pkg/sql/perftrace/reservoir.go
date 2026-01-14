// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package perftrace

import (
	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/cockroachdb/cockroach/pkg/util/randutil"
	"github.com/cockroachdb/cockroach/pkg/util/syncutil"
)

// RateSampler implements fixed-rate probabilistic sampling for work spans.
// Each span is independently sampled with probability p (the SampleRate setting).
// This preserves proportional distribution across nodes: if node 1 does 10x more
// work than node 2, it contributes ~10x more samples.
//
// A per-node cap (MaxSamplesPerInterval) acts as a safety valve to prevent OOM.
type RateSampler struct {
	mu struct {
		syncutil.Mutex
		spans []WorkSpan
	}
	settings *cluster.Settings
}

// NewRateSampler creates a new RateSampler.
func NewRateSampler(settings *cluster.Settings) *RateSampler {
	maxSamples := int(MaxSamplesPerInterval.Get(&settings.SV))
	s := &RateSampler{
		settings: settings,
	}
	s.mu.spans = make([]WorkSpan, 0, maxSamples)
	return s
}

// MaybeSample decides whether to sample this span based on the sample rate.
// Returns true if the span should be captured, false otherwise.
// This is a fast-path check that avoids allocations for non-sampled spans.
func (s *RateSampler) MaybeSample() bool {
	// Fast path: check if at capacity
	maxSamples := int(MaxSamplesPerInterval.Get(&s.settings.SV))
	atCapacity := s.isAtCapacity(maxSamples)
	if atCapacity {
		return false
	}

	// Roll against sample rate using fast random
	rate := SampleRate.Get(&s.settings.SV)
	// Convert rate to a threshold in the uint32 range for comparison
	threshold := uint32(rate * float64(^uint32(0)))
	return randutil.FastUint32() < threshold
}

// isAtCapacity checks if the sampler has reached capacity.
func (s *RateSampler) isAtCapacity(maxSamples int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mu.spans) >= maxSamples
}

// Record adds a completed span to the sampler.
// Should only be called if MaybeSample() returned true.
func (s *RateSampler) Record(span WorkSpan) {
	maxSamples := int(MaxSamplesPerInterval.Get(&s.settings.SV))
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.mu.spans) < maxSamples {
		s.mu.spans = append(s.mu.spans, span)
	}
}

// Drain removes and returns all spans from the sampler.
func (s *RateSampler) Drain() []WorkSpan {
	maxSamples := int(MaxSamplesPerInterval.Get(&s.settings.SV))
	s.mu.Lock()
	defer s.mu.Unlock()
	spans := s.mu.spans
	s.mu.spans = make([]WorkSpan, 0, maxSamples)
	return spans
}

// Len returns the current number of spans in the sampler.
func (s *RateSampler) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mu.spans)
}
