// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package perftrace

import (
	"time"

	"github.com/cockroachdb/cockroach/pkg/settings"
)

// Enabled controls whether work span capture is enabled.
var Enabled = settings.RegisterBoolSetting(
	settings.ApplicationLevel,
	"sql.work_span_capture.enabled",
	"enables work span capture for query observability (experimental)",
	true, // enabled by default for POC
)

// SampleRate controls the probability of sampling each work span.
var SampleRate = settings.RegisterFloatSetting(
	settings.ApplicationLevel,
	"sql.work_span_capture.sample_rate",
	"probability of sampling each work span (0.0 to 1.0)",
	0.005, // 0.1% default
	settings.FloatInRange(0.0, 1.0),
)

// MaxSamplesPerInterval controls the maximum samples to collect per flush interval.
var MaxSamplesPerInterval = settings.RegisterIntSetting(
	settings.ApplicationLevel,
	"sql.work_span_capture.max_samples_per_interval",
	"maximum samples to collect per flush interval (safety cap)",
	1000,
	settings.IntInRange(10, 100000),
)

// FlushInterval controls the interval between flushes to the system table.
var FlushInterval = settings.RegisterDurationSetting(
	settings.ApplicationLevel,
	"sql.work_span_capture.flush_interval",
	"interval between work span flushes to the system table",
	time.Second*10,
	settings.DurationInRange(5*time.Second, 10*time.Minute),
)

// DeleteRatio controls the fraction of existing spans to delete during flush.
var DeleteRatio = settings.RegisterFloatSetting(
	settings.ApplicationLevel,
	"sql.work_span_capture.delete_ratio",
	"fraction of existing spans to randomly delete during each flush (0.0 to 1.0)",
	0.01, // 1% by default
	settings.FloatInRange(0.0, 1.0),
)

// ComponentMetricsEnabled controls whether component-specific numeric metrics are captured.
var ComponentMetricsEnabled = settings.RegisterBoolSetting(
	settings.ApplicationLevel,
	"sql.work_span_capture.component_metrics.enabled",
	"enables capture of component-specific numeric metrics in work spans (num_rows, memory_used, write_io, contention_time)",
	false,
)

// ComponentAttributesEnabled controls whether component-specific string attributes are captured.
var ComponentAttributesEnabled = settings.RegisterBoolSetting(
	settings.ApplicationLevel,
	"sql.work_span_capture.component_attributes.enabled",
	"enables capture of component-specific string attributes in work spans (app_name, start_key, range_id)",
	false,
)
