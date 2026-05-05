// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"time"

	"github.com/cockroachdb/cockroach/pkg/settings"
)

// CacheSize bounds the per-node Cache. Default sized for >99% of the
// cloud fleet per the cardinality investigation. Operators on
// high-cardinality clusters can tune up.
var CacheSize = settings.RegisterIntSetting(
	settings.ApplicationLevel,
	"obs.execution_attributes.cache_size",
	"maximum number of entries in the per-node execution-attributes cache",
	16384,
	settings.PositiveInt,
)

// WriteQueueSize bounds the per-node durable write queue. Overflow drops
// the new entry being enqueued and increments obs.execution_attributes.discarded.
var WriteQueueSize = settings.RegisterIntSetting(
	settings.ApplicationLevel,
	"obs.execution_attributes.write_queue_size",
	"maximum pending durable writes per node",
	1024,
	settings.PositiveInt,
)

// MissReadTimeout is the per-miss KV read timeout at the sampler.
var MissReadTimeout = settings.RegisterDurationSetting(
	settings.ApplicationLevel,
	"obs.execution_attributes.miss_read_timeout",
	"per-miss KV read timeout for sampler-side resolution",
	50*time.Millisecond,
	settings.PositiveDuration,
)

// MissTickBudget is the per-tick total budget for miss-resolution KV reads.
var MissTickBudget = settings.RegisterDurationSetting(
	settings.ApplicationLevel,
	"obs.execution_attributes.miss_tick_budget",
	"per-tick budget for miss-resolution KV reads at the sampler",
	200*time.Millisecond,
	settings.PositiveDuration,
)
