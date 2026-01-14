// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package perftrace

import (
	"context"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
)

// BenchmarkMaybeSampleNotSampled measures the overhead of MaybeSample
// when the sampler is at capacity and most samples are rejected.
// This is the common case in production.
func BenchmarkMaybeSampleNotSampled(b *testing.B) {
	ctx := context.Background()
	settings := cluster.MakeTestingClusterSettings()
	// Set a high sample rate for the warmup phase
	SampleRate.Override(ctx, &settings.SV, 1.0)
	MaxSamplesPerInterval.Override(ctx, &settings.SV, 100)

	s := NewRateSampler(settings)
	// Warm up to capacity so most samples are rejected
	for i := 0; i < 100; i++ {
		if s.MaybeSample() {
			s.Record(WorkSpan{})
		}
	}
	// Set a low sample rate for the benchmark (to match production behavior)
	SampleRate.Override(ctx, &settings.SV, 0.001)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if s.MaybeSample() {
				s.Record(WorkSpan{})
			}
		}
	})
}

// BenchmarkMaybeSampleNotSampledSerial measures single-threaded performance.
func BenchmarkMaybeSampleNotSampledSerial(b *testing.B) {
	ctx := context.Background()
	settings := cluster.MakeTestingClusterSettings()
	// Set a high sample rate for the warmup phase
	SampleRate.Override(ctx, &settings.SV, 1.0)
	MaxSamplesPerInterval.Override(ctx, &settings.SV, 100)

	s := NewRateSampler(settings)
	// Warm up to capacity
	for i := 0; i < 100; i++ {
		if s.MaybeSample() {
			s.Record(WorkSpan{})
		}
	}
	// Set a low sample rate for the benchmark (to match production behavior)
	SampleRate.Override(ctx, &settings.SV, 0.001)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if s.MaybeSample() {
			s.Record(WorkSpan{})
		}
	}
}
