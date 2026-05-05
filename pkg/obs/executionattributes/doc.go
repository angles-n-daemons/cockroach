// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

// Package executionattributes implements ASH sample enrichment for
// statement-shaped work. It introduces a typed identifier
// (ExecutionAttributesID) derived from xxhash64 of (stmt_fingerprint_id,
// app_name), backed by the system.execution_attributes table.
//
// The package provides three components that cooperate at runtime:
//
//   - A per-node Cache (ID -> Entry) used both at the gateway (to avoid
//     redundant durable writes) and at the sampler (to denormalize at
//     sample-write time).
//   - A background Writer that drains a bounded queue of pending writes
//     onto system.execution_attributes via an internal executor.
//   - A SamplerResolver that resolves IDs at sample time, falling back to
//     a bounded synchronous KV read on cache miss.
//
// See the design document at:
//
//	docs/superpowers/specs/2026-05-05-ash-enrichment-design.md
package executionattributes
