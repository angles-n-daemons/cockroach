// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package perftrace

import (
	"context"
	"time"
)

// QueryTag represents a sqlcommenter key-value tag attached to a query.
type QueryTag struct {
	Key   string
	Value string
}

// WorkSpan represents a captured execution span.
type WorkSpan struct {
	// ID is a unique identifier for this span, generated via unique_rowid().
	ID int64
	// ParentID is the ID of the parent span (0 if no parent).
	ParentID int64
	// NodeID is the SQL instance ID of the node where this span was captured.
	NodeID int32
	// StatementFingerprintID is the fingerprint ID of the statement.
	StatementFingerprintID uint64
	// Timestamp is when the span started.
	Timestamp time.Time
	// Duration is the wall-clock time of the span in nanoseconds.
	Duration time.Duration
	// CPUTime is the CPU time consumed by this span in nanoseconds.
	CPUTime time.Duration
	// Component identifies the type of work (e.g., "gateway", "sql.TableReader", "kv.batch", "raft.follower").
	Component string
	// ComponentMetrics contains numeric metrics for this span (e.g., num_rows, memory_used, contention_time).
	ComponentMetrics map[string]int64
	// ComponentAttributes contains string attributes for this span (e.g., app_name, start_key).
	ComponentAttributes map[string]string
	// QueryTags contains sqlcommenter query tags propagated from the gateway.
	QueryTags []QueryTag
}

// contextKey is used for storing work span context values.
type contextKey int

const (
	// parentSpanIDKey is the context key for the parent work span ID.
	parentSpanIDKey contextKey = iota
	// fingerprintIDKey is the context key for the statement fingerprint ID.
	fingerprintIDKey
	// currentSpanHandleKey is the context key for the current work span handle.
	currentSpanHandleKey
	// queryTagsKey is the context key for propagating query tags to child spans.
	queryTagsKey
)

// WithParentSpanID returns a new context with the parent work span ID set.
func WithParentSpanID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, parentSpanIDKey, id)
}

// GetParentSpanIDFromContext returns the parent work span ID from the context.
// Returns 0 if not set.
func GetParentSpanIDFromContext(ctx context.Context) int64 {
	if v := ctx.Value(parentSpanIDKey); v != nil {
		return v.(int64)
	}
	return 0
}

// WithFingerprintID returns a new context with the statement fingerprint ID set.
func WithFingerprintID(ctx context.Context, id uint64) context.Context {
	return context.WithValue(ctx, fingerprintIDKey, id)
}

// GetFingerprintFromContext returns the statement fingerprint ID from the context.
// Returns 0 if not set.
func GetFingerprintFromContext(ctx context.Context) uint64 {
	if v := ctx.Value(fingerprintIDKey); v != nil {
		return v.(uint64)
	}
	return 0
}

// collectorKey is the context key for the work span collector.
type collectorKeyType struct{}

var collectorKey = collectorKeyType{}

// WithCollector returns a new context with the work span collector set.
// This is used to make the collector available to the KV layer.
func WithCollector(ctx context.Context, collector *Collector) context.Context {
	return context.WithValue(ctx, collectorKey, collector)
}

// GetCollectorFromContext returns the work span collector from the context.
// Returns nil if not set.
func GetCollectorFromContext(ctx context.Context) *Collector {
	if v := ctx.Value(collectorKey); v != nil {
		return v.(*Collector)
	}
	return nil
}

// WithCurrentSpanHandle returns a new context with the current work span handle set.
// This is used to make the span handle available to low-level waiting code
// (latch manager, lock table waiter) so they can add contention time directly.
func WithCurrentSpanHandle(ctx context.Context, handle *SpanHandle) context.Context {
	return context.WithValue(ctx, currentSpanHandleKey, handle)
}

// GetCurrentSpanHandleFromContext returns the current work span handle from the context.
// Returns nil if not set.
func GetCurrentSpanHandleFromContext(ctx context.Context) *SpanHandle {
	if v := ctx.Value(currentSpanHandleKey); v != nil {
		return v.(*SpanHandle)
	}
	return nil
}

// AddContentionTimeFromContext adds the given contention duration to the current
// work span handle in the context. This is a convenience function for use by
// low-level waiting code that doesn't need to directly manipulate the handle.
// If there is no current span handle in the context, this is a no-op.
func AddContentionTimeFromContext(ctx context.Context, d time.Duration) {
	if h := GetCurrentSpanHandleFromContext(ctx); h != nil {
		h.AddContentionTime(d)
	}
}

// WithQueryTags returns a new context with the query tags set.
// This allows query tags to propagate from the gateway to all child spans.
func WithQueryTags(ctx context.Context, tags []QueryTag) context.Context {
	return context.WithValue(ctx, queryTagsKey, tags)
}

// GetQueryTagsFromContext returns the query tags from the context.
// Returns nil if not set.
func GetQueryTagsFromContext(ctx context.Context) []QueryTag {
	if v := ctx.Value(queryTagsKey); v != nil {
		return v.([]QueryTag)
	}
	return nil
}

// IncrementComponentMetricFromContext increments a component metric on the
// current work span handle in the context. This is a convenience function for
// use by code that doesn't need to directly manipulate the handle.
// If there is no current span handle in the context, this is a no-op.
func IncrementComponentMetricFromContext(ctx context.Context, name string, value int64) {
	if h := GetCurrentSpanHandleFromContext(ctx); h != nil {
		h.IncrementComponentMetric(name, value)
	}
}

// SetComponentAttributeFromContext sets a component attribute on the current
// work span handle in the context. This is a convenience function for use by
// code that doesn't need to directly manipulate the handle.
// If there is no current span handle in the context, this is a no-op.
func SetComponentAttributeFromContext(ctx context.Context, name, value string) {
	if h := GetCurrentSpanHandleFromContext(ctx); h != nil {
		h.SetComponentAttribute(name, value)
	}
}
