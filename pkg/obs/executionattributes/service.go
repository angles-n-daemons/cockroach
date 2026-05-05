// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"time"

	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
)

// Service is the per-node singleton that owns the cache, writer,
// gateway resolver, sampler resolver, and metrics. It is intended to
// be constructed once at SQL server startup and exposed via
// ExecutorConfig (or equivalent) to the connExecutor and the ASH
// sampler.
type Service struct {
	Settings *cluster.Settings
	Cache    *Cache
	Writer   *Writer
	Gateway  *GatewayResolver
	Sampler  *SamplerResolver
	Metrics  Metrics
}

// NewService constructs a Service with the given executor (used by the
// background writer to insert rows into system.execution_attributes)
// and reader (used by the sampler to resolve cache misses).
//
// The caller is responsible for launching the writer goroutine, e.g.:
//
//	stopper.RunAsyncTask(ctx, "execution-attributes-writer", svc.Writer.Run)
//
// (executionattributes does not import pkg/util/stop directly to avoid
// a dependency cycle through kvpb.)
func NewService(settings *cluster.Settings, exec executor, r reader) *Service {
	metrics := NewMetrics()
	cache := NewCache(int(CacheSize.Get(&settings.SV)))
	writer := NewWriter(exec, &metrics, int(WriteQueueSize.Get(&settings.SV)))
	gateway := NewGatewayResolver(cache, writer)
	sampler := NewSamplerResolver(
		cache, r, &metrics,
		func() time.Duration { return MissReadTimeout.Get(&settings.SV) },
		func() time.Duration { return MissTickBudget.Get(&settings.SV) },
	)
	return &Service{
		Settings: settings,
		Cache:    cache,
		Writer:   writer,
		Gateway:  gateway,
		Sampler:  sampler,
		Metrics:  metrics,
	}
}

