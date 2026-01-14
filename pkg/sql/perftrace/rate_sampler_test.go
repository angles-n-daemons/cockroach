// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package perftrace

import (
	"context"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/stretchr/testify/require"
)

func TestRateSampler_BasicSampling(t *testing.T) {
	ctx := context.Background()
	settings := cluster.MakeTestingClusterSettings()

	// Set 100% sample rate to ensure all samples are captured
	SampleRate.Override(ctx, &settings.SV, 1.0)
	MaxSamplesPerInterval.Override(ctx, &settings.SV, 1000)

	s := NewRateSampler(settings)

	// With 100% sample rate, all samples should be captured
	sampled := 0
	for i := 0; i < 100; i++ {
		if s.MaybeSample() {
			s.Record(WorkSpan{ID: int64(i)})
			sampled++
		}
	}

	require.Equal(t, 100, sampled, "with 100%% sample rate, all spans should be sampled")
	require.Equal(t, 100, s.Len())

	// Drain and verify
	spans := s.Drain()
	require.Len(t, spans, 100)
	require.Equal(t, 0, s.Len(), "sampler should be empty after drain")
}

func TestRateSampler_MaxCapacity(t *testing.T) {
	ctx := context.Background()
	settings := cluster.MakeTestingClusterSettings()

	// Set 100% sample rate but low capacity
	SampleRate.Override(ctx, &settings.SV, 1.0)
	MaxSamplesPerInterval.Override(ctx, &settings.SV, 50)

	s := NewRateSampler(settings)

	// Try to sample more than capacity
	sampled := 0
	for i := 0; i < 100; i++ {
		if s.MaybeSample() {
			s.Record(WorkSpan{ID: int64(i)})
			sampled++
		}
	}

	// Should be capped at max capacity
	require.LessOrEqual(t, s.Len(), 50, "sampler should respect max capacity")
}

func TestRateSampler_ZeroRate(t *testing.T) {
	ctx := context.Background()
	settings := cluster.MakeTestingClusterSettings()

	// Set 0% sample rate - nothing should be sampled
	SampleRate.Override(ctx, &settings.SV, 0.0)
	MaxSamplesPerInterval.Override(ctx, &settings.SV, 1000)

	s := NewRateSampler(settings)

	sampled := 0
	for i := 0; i < 1000; i++ {
		if s.MaybeSample() {
			s.Record(WorkSpan{ID: int64(i)})
			sampled++
		}
	}

	require.Equal(t, 0, sampled, "with 0%% sample rate, no spans should be sampled")
}

func TestRateSampler_ProportionalSampling(t *testing.T) {
	ctx := context.Background()
	settings := cluster.MakeTestingClusterSettings()

	// Set 10% sample rate
	SampleRate.Override(ctx, &settings.SV, 0.1)
	MaxSamplesPerInterval.Override(ctx, &settings.SV, 10000)

	s := NewRateSampler(settings)

	// Sample many spans
	sampled := 0
	total := 10000
	for i := 0; i < total; i++ {
		if s.MaybeSample() {
			s.Record(WorkSpan{ID: int64(i)})
			sampled++
		}
	}

	// With 10% rate over 10000 samples, we expect ~1000 samples
	// Allow for statistical variance (should be within 800-1200 with high probability)
	require.Greater(t, sampled, 500, "sample count should be roughly 10%% of total")
	require.Less(t, sampled, 1500, "sample count should be roughly 10%% of total")
}

// TestRateSampler_PreservesProportionalDistribution verifies the key behavior:
// nodes with higher workload produce proportionally more samples.
// This is the main improvement over reservoir sampling.
func TestRateSampler_PreservesProportionalDistribution(t *testing.T) {
	ctx := context.Background()

	// Use the same settings for both "nodes" (simulated by separate samplers)
	settings := cluster.MakeTestingClusterSettings()
	SampleRate.Override(ctx, &settings.SV, 0.01) // 1% sample rate
	MaxSamplesPerInterval.Override(ctx, &settings.SV, 10000)

	// Simulate two nodes with different workloads
	// Node 1: high traffic (10,000 spans)
	// Node 2: low traffic (1,000 spans)
	node1Sampler := NewRateSampler(settings)
	node2Sampler := NewRateSampler(settings)

	node1Workload := 10000
	node2Workload := 1000

	// Process workload on each "node"
	for i := 0; i < node1Workload; i++ {
		if node1Sampler.MaybeSample() {
			node1Sampler.Record(WorkSpan{ID: int64(i), NodeID: 1})
		}
	}
	for i := 0; i < node2Workload; i++ {
		if node2Sampler.MaybeSample() {
			node2Sampler.Record(WorkSpan{ID: int64(i), NodeID: 2})
		}
	}

	node1Samples := node1Sampler.Len()
	node2Samples := node2Sampler.Len()

	t.Logf("Node 1: %d workload -> %d samples (%.2f%%)",
		node1Workload, node1Samples, float64(node1Samples)/float64(node1Workload)*100)
	t.Logf("Node 2: %d workload -> %d samples (%.2f%%)",
		node2Workload, node2Samples, float64(node2Samples)/float64(node2Workload)*100)

	// Key assertion: the ratio of samples should approximately match the ratio of workloads
	// Node 1 has 10x the workload, so should have roughly 10x the samples
	// With 1% sampling: expect ~100 samples from node 1, ~10 from node 2
	//
	// We allow for statistical variance, but the ratio should be preserved.
	// The important thing is that node 1 contributes MORE samples than node 2,
	// proportional to its workload.

	require.Greater(t, node1Samples, 0, "node 1 should have some samples")
	require.Greater(t, node2Samples, 0, "node 2 should have some samples")

	// Check that node 1 has significantly more samples (at least 3x, expected ~10x)
	ratio := float64(node1Samples) / float64(node2Samples)
	t.Logf("Sample ratio (node1/node2): %.2f (expected ~%.2f)",
		ratio, float64(node1Workload)/float64(node2Workload))

	require.Greater(t, ratio, 3.0,
		"node with 10x workload should have at least 3x more samples (got %.2f)", ratio)
	require.Less(t, ratio, 30.0,
		"sample ratio should be reasonable (got %.2f)", ratio)

	// Verify that total samples reflects total work
	// With 1% sampling of 11,000 total spans, expect ~110 total samples
	totalSamples := node1Samples + node2Samples
	expectedTotal := float64(node1Workload+node2Workload) * 0.01
	require.Greater(t, totalSamples, int(expectedTotal*0.5),
		"total samples should be roughly 1%% of total workload")
	require.Less(t, totalSamples, int(expectedTotal*2.0),
		"total samples should be roughly 1%% of total workload")
}
