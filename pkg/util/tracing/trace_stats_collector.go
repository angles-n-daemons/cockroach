// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package tracing

import "github.com/cockroachdb/cockroach/pkg/util/syncutil"

// AggregateTraceStats holds running totals and counts for computing averages.
type AggregateTraceStats struct {
	// TraceCount is the number of traces collected.
	TraceCount int64

	// Running totals (divide by TraceCount for averages).
	TotalSpans                   int64
	TotalStructuredRecords       int64
	TotalStructuredRecordsBytes  int64
	TotalComponentStatsBytes     int64
	TotalContentionEventBytes    int64
	TotalAdmissionQueueStatsBytes int64
	TotalOtherStructuredBytes    int64
	TotalLogRecords              int64
	TotalLogRecordsBytes         int64
	TotalTagGroups               int64
	TotalTags                    int64
	TotalTagsBytes               int64
	TotalChildrenMetadataEntries int64
	TotalChildrenMetadataBytes   int64
	TotalSpanOverheadBytes       int64
	TotalEstimatedBytes          int64
}

// TraceStatsCollector aggregates trace statistics across executions.
type TraceStatsCollector struct {
	mu    syncutil.Mutex
	stats AggregateTraceStats
}

// NewTraceStatsCollector creates a new TraceStatsCollector.
func NewTraceStatsCollector() *TraceStatsCollector {
	return &TraceStatsCollector{}
}

// RecordTrace records statistics from a single trace.
func (c *TraceStatsCollector) RecordTrace(stats TraceStats) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.TraceCount++
	c.stats.TotalSpans += stats.NumSpans
	c.stats.TotalStructuredRecords += stats.NumStructuredRecords
	c.stats.TotalStructuredRecordsBytes += stats.StructuredRecordsBytes
	c.stats.TotalComponentStatsBytes += stats.ComponentStatsBytes
	c.stats.TotalContentionEventBytes += stats.ContentionEventBytes
	c.stats.TotalAdmissionQueueStatsBytes += stats.AdmissionQueueStatsBytes
	c.stats.TotalOtherStructuredBytes += stats.OtherStructuredBytes
	c.stats.TotalLogRecords += stats.NumLogRecords
	c.stats.TotalLogRecordsBytes += stats.LogRecordsBytes
	c.stats.TotalTagGroups += stats.NumTagGroups
	c.stats.TotalTags += stats.NumTags
	c.stats.TotalTagsBytes += stats.TagsBytes
	c.stats.TotalChildrenMetadataEntries += stats.NumChildrenMetadataEntries
	c.stats.TotalChildrenMetadataBytes += stats.ChildrenMetadataBytes
	c.stats.TotalSpanOverheadBytes += stats.SpanOverheadBytes
	c.stats.TotalEstimatedBytes += stats.TotalEstimatedBytes
}

// GetStats returns a copy of the current aggregate statistics.
func (c *TraceStatsCollector) GetStats() AggregateTraceStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// Reset resets the collector to its initial state.
func (c *TraceStatsCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = AggregateTraceStats{}
}
