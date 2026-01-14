// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package perftrace

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/cockroachdb/cockroach/pkg/util/hlc"
	"github.com/cockroachdb/cockroach/pkg/util/timeutil"
)

// noOpHandle is a singleton handle returned when spans are not sampled.
// This avoids allocating a new SpanHandle for every non-sampled span.
var noOpHandle = &SpanHandle{shouldCapture: false}

// Collector manages work span capture for a node.
type Collector struct {
	sampler         *RateSampler
	nodeIDContainer *base.NodeIDContainer
	clock           *hlc.Clock
	settings        *cluster.Settings
	// nextSpanID is an atomic counter for generating unique span IDs within this node.
	nextSpanID atomic.Int64
}

// NewCollector creates a new Collector.
func NewCollector(
	nodeIDContainer *base.NodeIDContainer, clock *hlc.Clock, settings *cluster.Settings,
) *Collector {
	c := &Collector{
		sampler:         NewRateSampler(settings),
		nodeIDContainer: nodeIDContainer,
		clock:           clock,
		settings:        settings,
	}
	// Seed the span ID counter with a pseudo-random value based on the current
	// timestamp to avoid collisions across node restarts.
	seed := timeutil.Now().UnixNano()
	c.nextSpanID.Store(seed)
	return c
}

// StartSpan begins tracking a work span if sampled.
// Returns a SpanHandle that must be finished when the span completes.
// If the span is not sampled, returns a no-op handle.
// The component parameter identifies the type of work (e.g., "gateway", "sql.TableReader", "kv.batch").
// The context is used to capture query tags propagated from the gateway.
func (c *Collector) StartSpan(
	ctx context.Context,
	component string,
	stmtFingerprintID uint64,
	parentID int64,
) *SpanHandle {
	if !Enabled.Get(&c.settings.SV) {
		return noOpHandle
	}

	if !c.sampler.MaybeSample() {
		return noOpHandle
	}

	// Generate a unique span ID
	spanID := c.nextSpanID.Add(1)

	// Capture query tags from context
	var queryTags []QueryTag
	if tags := GetQueryTagsFromContext(ctx); tags != nil {
		queryTags = tags
	}

	return &SpanHandle{
		collector:         c,
		shouldCapture:     true,
		id:                spanID,
		parentID:          parentID,
		stmtFingerprintID: stmtFingerprintID,
		component:         component,
		startTime:         timeutil.Now(),
		stopWatch:         timeutil.NewStopWatchWithCPU(),
		queryTags:         queryTags,
	}
}

// Sampler returns the underlying sampler (for flushing).
func (c *Collector) Sampler() *RateSampler {
	return c.sampler
}

// NodeID returns the node ID of this collector.
// This is read dynamically from the container since the node ID may not be
// assigned at collector creation time.
func (c *Collector) NodeID() int32 {
	return int32(c.nodeIDContainer.Get())
}

// SpanHandle tracks an in-progress span.
type SpanHandle struct {
	collector         *Collector
	shouldCapture     bool
	id                int64
	parentID          int64
	stmtFingerprintID uint64
	component         string
	startTime         time.Time
	stopWatch         *timeutil.StopWatch

	// mu protects the maps below which may be accessed from multiple goroutines.
	mu                  sync.Mutex
	componentMetrics    map[string]int64
	componentAttributes map[string]string
	queryTags           []QueryTag
}

// ID returns the unique ID of this span.
// Returns 0 if the span is not being captured.
func (h *SpanHandle) ID() int64 {
	if h == nil || !h.shouldCapture {
		return 0
	}
	return h.id
}

// Start begins timing the span. Must be called after StartSpan.
func (h *SpanHandle) Start() {
	if h == nil || !h.shouldCapture {
		return
	}
	h.stopWatch.Start()
}

// SetContentionTime sets the contention time (lock + latch waits) for this span.
// This is stored as a component metric named "contention_time".
// Respects the component_metrics.enabled cluster setting.
func (h *SpanHandle) SetContentionTime(d time.Duration) {
	if h == nil || !h.shouldCapture {
		return
	}
	if !ComponentMetricsEnabled.Get(&h.collector.settings.SV) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.componentMetrics == nil {
		h.componentMetrics = make(map[string]int64)
	}
	h.componentMetrics["contention_time"] = d.Nanoseconds()
}

// AddContentionTime adds to the contention time for this span.
// This is stored as a component metric named "contention_time".
// Respects the component_metrics.enabled cluster setting.
func (h *SpanHandle) AddContentionTime(d time.Duration) {
	if h == nil || !h.shouldCapture {
		return
	}
	if !ComponentMetricsEnabled.Get(&h.collector.settings.SV) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.componentMetrics == nil {
		h.componentMetrics = make(map[string]int64)
	}
	h.componentMetrics["contention_time"] += d.Nanoseconds()
}

// IncrementComponentMetric increments a named numeric metric for this span.
// The metric value is added to any existing value.
// This method is thread-safe and checks the component_metrics.enabled setting.
func (h *SpanHandle) IncrementComponentMetric(name string, value int64) {
	if h == nil || !h.shouldCapture {
		return
	}
	if !ComponentMetricsEnabled.Get(&h.collector.settings.SV) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.componentMetrics == nil {
		h.componentMetrics = make(map[string]int64)
	}
	h.componentMetrics[name] += value
}

// SetComponentAttribute sets a named string attribute for this span.
// This method is thread-safe and checks the component_attributes.enabled setting.
func (h *SpanHandle) SetComponentAttribute(name, value string) {
	if h == nil || !h.shouldCapture {
		return
	}
	if !ComponentAttributesEnabled.Get(&h.collector.settings.SV) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.componentAttributes == nil {
		h.componentAttributes = make(map[string]string)
	}
	h.componentAttributes[name] = value
}

// Finish completes the span and records it to the reservoir if sampled.
func (h *SpanHandle) Finish() {
	if h == nil || !h.shouldCapture {
		return
	}

	h.stopWatch.Stop()

	// Copy fields under lock to build the span.
	h.mu.Lock()
	componentMetrics := h.componentMetrics
	componentAttributes := h.componentAttributes
	queryTags := h.queryTags
	h.mu.Unlock()

	span := WorkSpan{
		ID:                     h.id,
		ParentID:               h.parentID,
		NodeID:                 h.collector.NodeID(),
		StatementFingerprintID: h.stmtFingerprintID,
		Timestamp:              h.startTime,
		Duration:               h.stopWatch.Elapsed(),
		CPUTime:                h.stopWatch.ElapsedCPU(),
		Component:              h.component,
		ComponentMetrics:       componentMetrics,
		ComponentAttributes:    componentAttributes,
		QueryTags:              queryTags,
	}

	h.collector.sampler.Record(span)
}
