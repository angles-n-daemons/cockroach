// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import "github.com/cockroachdb/cockroach/pkg/util/metric"

var (
	metaCacheSize = metric.Metadata{
		Name:        "obs.execution_attributes.cache_size",
		Help:        "Current entries in the per-node execution-attributes cache",
		Measurement: "Entries",
		Unit:        metric.Unit_COUNT,
	}
	metaEvictions = metric.Metadata{
		Name:        "obs.execution_attributes.evictions",
		Help:        "Cumulative cache evictions",
		Measurement: "Evictions",
		Unit:        metric.Unit_COUNT,
	}
	metaDiscarded = metric.Metadata{
		Name:        "obs.execution_attributes.discarded",
		Help:        "Cache entries evicted before durable write completed; or write-queue overflows",
		Measurement: "Discards",
		Unit:        metric.Unit_COUNT,
	}
	metaCollisions = metric.Metadata{
		Name:        "obs.execution_attributes.collisions",
		Help:        "Hash collisions detected when writing to system.execution_attributes",
		Measurement: "Collisions",
		Unit:        metric.Unit_COUNT,
	}
	metaUnresolvedSamples = metric.Metadata{
		Name:        "obs.execution_attributes.unresolved_samples",
		Help:        "ASH samples written with NULL denormalized columns due to resolution failure",
		Measurement: "Samples",
		Unit:        metric.Unit_COUNT,
	}
)

// Metrics is the per-node metric struct for execution attributes. It is
// intended to be registered with the node's metric registry via
// AddMetricStruct.
type Metrics struct {
	CacheSize         *metric.Gauge
	Evictions         *metric.Counter
	Discarded         *metric.Counter
	Collisions        *metric.Counter
	UnresolvedSamples *metric.Counter
}

// MetricStruct implements metric.Struct.
func (Metrics) MetricStruct() {}

// NewMetrics creates a fresh Metrics struct.
func NewMetrics() Metrics {
	return Metrics{
		CacheSize:         metric.NewGauge(metaCacheSize),
		Evictions:         metric.NewCounter(metaEvictions),
		Discarded:         metric.NewCounter(metaDiscarded),
		Collisions:        metric.NewCounter(metaCollisions),
		UnresolvedSamples: metric.NewCounter(metaUnresolvedSamples),
	}
}
