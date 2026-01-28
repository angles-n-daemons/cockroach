// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package tracing

import (
	"strings"

	"github.com/cockroachdb/cockroach/pkg/util/tracing/tracingpb"
)

// TraceStats holds size statistics for a single trace.
type TraceStats struct {
	// NumSpans is the number of spans in the trace.
	NumSpans int64

	// NumStructuredRecords is the number of structured records across all spans.
	NumStructuredRecords int64
	// StructuredRecordsBytes is the total memory size of structured records.
	StructuredRecordsBytes int64

	// Breakdown of structured records by type.
	ComponentStatsBytes      int64 // execinfrapb.ComponentStats
	ContentionEventBytes     int64 // kvpb.ContentionEvent
	AdmissionQueueStatsBytes int64 // admissionpb.AdmissionWorkQueueStats
	OtherStructuredBytes     int64 // all other types

	// NumLogRecords is the number of log records across all spans.
	NumLogRecords int64
	// LogRecordsBytes is the total memory size of log records.
	LogRecordsBytes int64

	// NumTagGroups is the number of tag groups across all spans.
	NumTagGroups int64
	// NumTags is the total number of tags across all spans.
	NumTags int64
	// TagsBytes is the estimated memory size of all tags.
	TagsBytes int64

	// NumChildrenMetadataEntries is the number of children metadata entries
	// across all spans.
	NumChildrenMetadataEntries int64
	// ChildrenMetadataBytes is the estimated memory size of children metadata.
	ChildrenMetadataBytes int64

	// SpanOverheadBytes is the estimated per-span fixed overhead (IDs,
	// timestamps, operation names, goroutine IDs).
	SpanOverheadBytes int64

	// TotalEstimatedBytes is the total estimated memory usage of the trace.
	TotalEstimatedBytes int64
}

// ComputeTraceStats computes statistics for a trace.
func ComputeTraceStats(t Trace) TraceStats {
	var stats TraceStats
	computeTraceStatsRecursive(&t, &stats)
	stats.TotalEstimatedBytes = stats.StructuredRecordsBytes +
		stats.LogRecordsBytes + stats.TagsBytes +
		stats.ChildrenMetadataBytes + stats.SpanOverheadBytes
	return stats
}

// ComputeRecordingStats computes statistics for a recording (flat slice of spans).
func ComputeRecordingStats(rec tracingpb.Recording) TraceStats {
	var stats TraceStats
	for i := range rec {
		computeSpanStats(&rec[i], &stats)
	}
	stats.TotalEstimatedBytes = stats.StructuredRecordsBytes +
		stats.LogRecordsBytes + stats.TagsBytes +
		stats.ChildrenMetadataBytes + stats.SpanOverheadBytes
	return stats
}

func computeTraceStatsRecursive(t *Trace, stats *TraceStats) {
	computeSpanStats(&t.Root, stats)
	for i := range t.Children {
		computeTraceStatsRecursive(&t.Children[i], stats)
	}
}

func computeSpanStats(span *tracingpb.RecordedSpan, stats *TraceStats) {
	stats.NumSpans++

	// Span overhead: IDs (24 bytes for TraceID + SpanID + ParentSpanID) +
	// timestamps (32 bytes for StartTime + Duration) + operation name + goroutine ID (8).
	stats.SpanOverheadBytes += 24 + 32 + int64(len(span.Operation)) + 8

	// Structured records.
	for i := range span.StructuredRecords {
		stats.NumStructuredRecords++
		size := int64(span.StructuredRecords[i].MemorySize())
		stats.StructuredRecordsBytes += size

		// Categorize by type using the TypeUrl.
		typeUrl := ""
		if span.StructuredRecords[i].Payload != nil {
			typeUrl = span.StructuredRecords[i].Payload.TypeUrl
		}
		switch {
		case strings.HasSuffix(typeUrl, "ComponentStats"):
			stats.ComponentStatsBytes += size
		case strings.HasSuffix(typeUrl, "ContentionEvent"):
			stats.ContentionEventBytes += size
		case strings.HasSuffix(typeUrl, "AdmissionWorkQueueStats"):
			stats.AdmissionQueueStatsBytes += size
		default:
			stats.OtherStructuredBytes += size
		}
	}

	// Log records.
	for i := range span.Logs {
		stats.NumLogRecords++
		stats.LogRecordsBytes += int64(span.Logs[i].MemorySize())
	}

	// Tags.
	for i := range span.TagGroups {
		stats.NumTagGroups++
		for j := range span.TagGroups[i].Tags {
			stats.NumTags++
			tag := &span.TagGroups[i].Tags[j]
			// Estimate: key + value + 32 bytes overhead for Tag struct.
			stats.TagsBytes += int64(len(tag.Key) + len(tag.Value) + 32)
		}
	}

	// Children metadata.
	for op := range span.ChildrenMetadata {
		stats.NumChildrenMetadataEntries++
		// Estimate: operation name + 24 bytes for OperationMetadata struct
		// (duration 8 + count 8 + containsUnfinished 1, rounded up).
		stats.ChildrenMetadataBytes += int64(len(op) + 24)
	}
}
