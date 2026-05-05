# ASH Sample Enrichment Design

**Date:** 2026-05-05
**Author:** Brian Dillmann
**Status:** Design proposal

## Summary

Replace the per-attribute wire fields and lookup machinery currently used to attribute statement-shaped work in ASH (`WorkloadID`, `AppNameID`, `WorkloadType`) with a single hash-based identifier (`enrichment_id`) that resolves through a new system table (`system.execution_attributes`). The release scope is two attributes — `stmt_fingerprint_id` and `app_name` — with the design extensible to additional execution attributes (plan_gist, database, user, etc.) by simple column adds, and with a clear pattern for future attribution shapes (jobs, replication, KV operations) to follow.

## Goals

1. Replace the existing `WorkloadID + AppNameID` plumbing for statement-shaped attribution with one identifier on the wire.
2. Resolve the identifier to its underlying attributes with high availability — target 99.9% successful resolution, comfortably better than today's `appNameMap` with its in-memory + RPC fallback.
3. Eliminate the per-attribute threading burden that has historically blocked adding new attributes (plan_gist, user, database) to ASH.
4. Establish a pattern that future attribution shapes (job attribution with richer context, KV-layer attributes, replication attributes) can mirror without inventing their own conventions.

## Non-goals

1. **Job attribution.** `WorkloadTypeJob` continues to use the existing bare `workload_id` (job_id). Future work can introduce `JobAttributesID` mirroring this design.
2. **System task attribution.** `WorkloadTypeSystem` continues to use bare constant IDs. Same future-work caveat.
3. **Intent-resolution attribution with parent links.** A many-parent shape would be a different table structure; out of scope.
4. **KV-operation attribution** (engine type, scan direction, etc.). Same pattern applies but is a separate consumer.
5. **A generic attribution framework or registry.** Each shape gets its own typed ID and table; the pattern is small enough to copy.
6. **Solving the underlying high-cardinality problem.** Some clusters (see [cardinality investigation](./2026-05-05-ash-enrichment-cardinality-investigation.md)) have unbounded cardinality from app-names embedding tenant identifiers. We bound the cache and degrade gracefully; we don't try to fix that.

## Background

Today, `kvpb.RequestHeader` carries three fields used for attribution:

```proto
uint64 workload_id   = N;  // for statements: stmt_fingerprint_id; for jobs: job_id; for system: constant
uint64 app_name_id   = M;  // hash of app_name; resolved via per-process appNameMap
WorkloadType workload_type = O;  // discriminator: Statement | Job | System
```

For statement-shaped work, the gateway sets all three. The ASH sampler at any node reads them off `WorkState`s (populated from the request headers) and writes them into samples in the in-memory ring buffer.

Two systemic problems:

1. **Adding a new attribute (plan_gist, user, database) requires plumbing it through the same path:** new field on `RequestHeader`, new threading through `kv.Txn` and contexts, new threading into `WorkState`, new column on `ASHSample`. Every attribute is an end-to-end project. The team has explicitly noted in code (`pkg/obs/ash/types.go:55`) that this should be replaced by a general enrichment ID.

2. **`AppNameID` resolution depends on per-process state with no durable backing.** When a sampler at node N1 sees an `AppNameID` minted by gateway G1, it consults a local `appNameMap`. On miss, it RPCs to the originating gateway. If G1 has restarted, the mapping is lost and the sample's `app_name` is unresolvable. This is the same failure mode that contention-event resolution suffers from, and one we explicitly want to leave behind.

The Google Doc *Execution Attributes: A solution for the attribution problem in observability systems* lays out the high-level direction: a hash-derived identifier for each unique attribute set, backed by a cache that any observability system can consult. This design narrows that vision to one shape (execution attribution) for the release, while preserving the architectural rails for additional shapes.

## Architecture

A new package `pkg/obs/executionattributes` introduces:

- **`ExecutionAttributesID`** (Go type): a typed `uint64` wrapper. Computed at the gateway as `xxhash64(canonical_encoding(stmt_fingerprint_id, app_name))`. Same inputs → same ID, anywhere in the cluster.

- **`system.execution_attributes`** (system table): durable mapping `ExecutionAttributesID → (stmt_fingerprint_id, app_name)`. Per-tenant. Rows are written on first sighting from each gateway and never deleted in this release.

- **Per-node caches:** an `EnrichmentID → row` cache on each node. Used by gateways (to avoid re-writing the system table on each occurrence) and by samplers (to denormalize at sample-write time).

- **Wire field:** a new `enrichment_id uint64` field on `kvpb.RequestHeader`, added alongside the existing fields rather than replacing them. For statement-shaped work, the gateway populates `enrichment_id` and stops populating `workload_id` and `app_name_id` once the cluster version finalizes.

Three things flow:

1. **Computation and stamping** at the gateway: compute the ID once per (statement, session), cache the value alongside `instrumentationHelper.fingerprintId`, fire-and-forget the system table write, stamp the ID on every BatchRequest the statement issues.
2. **Propagation** through the existing `kv.Txn` and context plumbing: same channels as today's `WorkloadID` propagation, just with one field instead of two-plus-discriminator.
3. **Resolution** at the sampler: on each sample, look up the ID in the local cache, denormalize the (stmt_fingerprint_id, app_name) into the sample's columns, and write the sample to the ring buffer. Cache miss falls back to a synchronous KV read with bounded budget; on exhaustion of that budget, the sample's denormalized fields are `<unknown>` and a metric increments.

## Components

### System table: `system.execution_attributes`

```sql
CREATE TABLE system.execution_attributes (
    id                   INT8       NOT NULL PRIMARY KEY,
    stmt_fingerprint_id  BYTES      NOT NULL,
    app_name             STRING     NOT NULL,
    created_at           TIMESTAMP  NOT NULL DEFAULT now()
)
```

- **`id`**: typed in Go as `ExecutionAttributesID = uint64`, stored as `INT8`. The hash of the canonical encoding of `(stmt_fingerprint_id, app_name)`.
- **`stmt_fingerprint_id`**: `BYTES`, matching the column type used in `system.statement_statistics`.
- **`app_name`**: `STRING`, stored verbatim.
- **`created_at`**: bookkeeping. No TTL applied initially — the table grows with bounded cardinality (see [investigation](./2026-05-05-ash-enrichment-cardinality-investigation.md)). A TTL job can be added later if needed.

**No FK to `system.statement_statistics`.** The `stmt_fingerprint_id` value is stored verbatim. Resolution `id → (stmt_fingerprint_id, app_name)` always works as long as the `system.execution_attributes` row exists; the further lookup `stmt_fingerprint_id → SQL text` continues to depend on `system.statement_statistics` rotation behavior, which is the existing behavior (no better, no worse).

**Tenancy.** Per-tenant table. Both inputs (fingerprint_id, app_name) are tenant-scoped; tenants do not need (and should not have) cross-tenant access to enrichment data. Matches the layout of `system.statement_statistics`.

**Hash collisions.** A 64-bit xxhash collision on `(stmt_fingerprint_id, app_name)` is rare — birthday-bound around 2^32 distinct combos, while observed cardinality is in the thousands per cluster. On INSERT we use `ON CONFLICT (id) DO NOTHING` and then SELECT to verify the existing row matches what we tried to insert. If they differ (genuine collision), the gateway:
- Increments `obs.execution_attributes.collisions` counter.
- Logs a WARNING with both colliding tuples.
- Falls back to stamping `enrichment_id = 0` (sentinel for "unattributed") for that statement. ASH samples for that statement show as `<unknown>`.

This is a degraded-but-correct path; the metric exists so we'll know if cardinality assumptions ever break.

**Migration.** New system table, follows the standard system-table-change checklist:
- Schema definition in `pkg/sql/catalog/systemschema/`.
- Migration in `pkg/upgrade/upgrades/`.
- Cluster version gate `V25_x_AddExecutionAttributesTable` (exact version per release planning).
- Bootstrap test hash updates and golden file refreshes.

### Wire format

**`kvpb.RequestHeader` changes:**

```proto
// New field, additive.
uint64 enrichment_id = P;

// Existing fields kept; semantics unchanged.
uint64 workload_id   = N;
WorkloadType workload_type = O;

// Removed during the migration cycle (see Migration plan below).
// uint64 app_name_id = M;
```

**Discriminator semantics.** `workload_type` continues to discriminate the shape. For shapes with an enrichment table (currently only `WorkloadTypeStatement`), the wire ID is read from `enrichment_id`. For shapes without (`WorkloadTypeJob`, `WorkloadTypeSystem`), the wire ID is read from `workload_id` as today. The two ID fields are mutually exclusive given the discriminator; only one is meaningful per request.

As future shapes acquire enrichment tables, their work migrates from `workload_id` to `enrichment_id` (each shape's enrichment ID lives in the same wire field, distinguished by `workload_type`). Eventually `workload_id` can be retired entirely.

### Gateway: computation, caching, write path

Lives in `connExecutor` / `instrumentationHelper`, alongside the existing `fingerprintId` cache. Lifecycle of one statement:

1. Statement parsing and fingerprinting compute `stmt_fingerprint_id` (existing). Session has `app_name` (existing).
2. After the fingerprint is known, compute `enrichment_id = xxhash64(canonical_encoding(stmt_fingerprint_id, app_name))`. Stash on `instrumentationHelper`. Cost: one hash call (~100 ns) per statement.
3. Look up `enrichment_id` in the per-node gateway cache.
   - **Cache hit:** done. Stamp on every BatchRequest the statement issues.
   - **Cache miss:** insert the entry into the local cache immediately. Enqueue the `(id, stmt_fingerprint_id, app_name)` tuple onto the per-node bounded write queue. Stamp `enrichment_id` on the BatchRequest. The statement does not wait for the write.

A single background goroutine drains the write queue:

- Pulls entries, runs `INSERT INTO system.execution_attributes (...) VALUES (...) ON CONFLICT (id) DO NOTHING` via the internal executor.
- Verifies via SELECT that the row at `id` matches what we wrote (collision detection, see above).
- Retries with backoff on transient KV errors (3 attempts).
- On queue overflow (writes can't keep up — should be rare): drops the new entry being enqueued (rather than evicting an in-flight write), increments `obs.execution_attributes.discarded`. The cache entry is still present locally, so the same gateway can still resolve it for its own samples; only the durable write is missed, which means *other* nodes will see resolution misses for IDs from this gateway.

**Cache structure.**
- Per-node singleton, lives on the `Server` struct.
- Bounded LRU. Default 16,384 entries (~1.5 MB). Cluster setting `obs.execution_attributes.cache_size`. Default sized for >99% of the fleet (see [investigation](./2026-05-05-ash-enrichment-cardinality-investigation.md)).
- Entries store `(enrichment_id, stmt_fingerprint_id, app_name, write_status)`. `write_status` tracks whether the durable write has committed; used only for the discarded metric.

**Source-level caching of the ID.** The hash itself is computed once per (statement, session) pair and cached on `instrumentationHelper`. Subsequent BatchRequests issued by the same statement reuse the cached ID via pointer dereference — no per-request hashing.

### Sampler: cache, miss handling, denormalization

Lives in `pkg/obs/ash`. Extends `Sampler` and `WorkState`.

The sampler's existing plumbing changes only at the receiving end of the wire change: `WorkState.WorkloadID` and `WorkState.AppNameID` are replaced by `WorkState.EnrichmentID`, populated from the BatchRequest header through the existing `kv.Txn` → `ash.SetWorkState` path.

Lifecycle of one sample at a sampler tick:

1. The sampler tick fires (default 1Hz). For each registered work state, take a shallow copy.
2. Look up `EnrichmentID` in the per-node *resolution cache* (separate from the gateway-side cache; same underlying type).
   - **Cache hit:** denormalize into the expanded columns. Write the full sample to the ring buffer. Done.
   - **Cache miss:** see miss policy below.

**Miss policy:** the sampler does *not* block on resolution.

- Issue a synchronous KV point-read with a short timeout (e.g., 50 ms).
- Track a per-tick miss-resolution budget (e.g., 200 ms total per tick).
- If the read succeeds: cache the result, denormalize, write the sample.
- If the read times out, fails, or the per-tick budget is exhausted: store the sample with NULL `stmt_fingerprint_id` and empty `app_name`. The `enrichment_id` itself is preserved on the sample so it remains joinable. Increment `obs.execution_attributes.unresolved_samples`.

**Cache structure:**
- Per-node singleton, separate from the gateway-side cache (the same node may host both, and the two have different write semantics).
- Bounded LRU, same `obs.execution_attributes.cache_size` default and cluster setting as the gateway-side cache.
- No pre-warming at node startup. Functional warmup (cache populated with the combos accounting for ~99% of execution traffic) takes seconds to a minute under typical workloads.

### Sample structure

The ASH sample (in-memory `ASHSample` and the virtual table column set) gains:

- `enrichment_id uint64` — new column, populated for statement-shaped samples.
- Existing `stmt_fingerprint_id` and `app_name` columns are kept and now populated from cache resolution (eager denormalization at sample-write time).
- Existing `workload_id` and `workload_type` columns are kept; populated only for non-statement work (jobs, system tasks).

The virtual table presents both the raw `enrichment_id` (for joinability and downstream consumers) and the denormalized columns (for human readability). Operators query the same columns they query today; no JOIN required.

## Migration plan

Standard cluster-version-gated pattern, two phases.

### Phase 1: this release (`V25_x_AddExecutionAttributesID`)

- Add `enrichment_id` to `kvpb.RequestHeader`.
- Add `system.execution_attributes` system table.
- Old binaries: don't know about the field; ignore on receive; never set. Continue to populate `workload_id` and `app_name_id` for statements as today.
- New binaries:
  - Below the version gate: behave as old binaries.
  - At/above the version gate: for statements, compute and stamp `enrichment_id`; *also continue to stamp `workload_id` and `app_name_id`* for the duration of this release so old binaries on the receive side still produce ASH samples with the existing fields.
- ASH sampler: always reads both. If `enrichment_id != 0` and the local cache resolves it, use the resolved `(stmt_fingerprint_id, app_name)`. Otherwise fall back to the legacy fields.

### Phase 2: next release

- Cluster version is finalized.
- Gateways stop setting `workload_id` and `app_name_id` for statements. Both are written through `enrichment_id` only.
- `app_name_id` field is removed from the proto and the codebase.
- `appNameMap` and `Sampler.resolveRemoteAppNames()` are deleted.
- ASH sampler reads only `enrichment_id` for statement-shaped samples.

## Observability

Metrics:

| Metric | Type | Meaning |
|---|---|---|
| `obs.execution_attributes.cache_size` | Gauge | Current entries in the per-node cache |
| `obs.execution_attributes.evictions` | Counter | Cumulative cache evictions |
| `obs.execution_attributes.discarded` | Counter | Cumulative cache entries evicted before durable write committed; or write-queue overflows. Per-event semantics, matching `sql.stats.discarded.current`. |
| `obs.execution_attributes.collisions` | Counter | Cumulative hash collisions detected |
| `obs.execution_attributes.unresolved_samples` | Counter | ASH samples written with `<unknown>` denormalized columns due to cache+KV resolution failure |
| `obs.execution_attributes.writes` | Counter | Cumulative system table writes attempted |
| `obs.execution_attributes.write_errors` | Counter | Cumulative system table write failures (after retries) |

Cluster settings:

| Setting | Type | Default | Purpose |
|---|---|---|---|
| `obs.execution_attributes.cache_size` | Int | 16384 | Max entries in the per-node LRU |
| `obs.execution_attributes.write_queue_size` | Int | 1024 | Max pending durable writes per node |
| `obs.execution_attributes.miss_read_timeout` | Duration | 50ms | Per-miss KV read timeout at the sampler |
| `obs.execution_attributes.miss_tick_budget` | Duration | 200ms | Per-tick total budget for miss-resolution KV reads |

Logs: when `discarded` increments, a rate-limited WARNING (at most once per minute per node) is emitted. The log line names the cluster setting (`obs.execution_attributes.cache_size` and `obs.execution_attributes.write_queue_size`) and points at the metric, so operators investigating ASH gaps have a breadcrumb.

## Testing strategy

**Unit tests:**
- ID computation determinism: same `(stmt_fingerprint_id, app_name)` → same `enrichment_id` regardless of node, time, ordering.
- Hash collision handling: forced collision via mock; verify counter increments, sample shows `<unknown>`, no incorrect resolution.
- Cache eviction: fill cache past capacity, verify oldest entries evicted, `evictions` counter increments.
- Write queue overflow: stuff queue past capacity, verify `discarded` counter increments and the new-enqueued entry is dropped (in-flight writes proceed unimpeded).

**Integration tests:**
- End-to-end: gateway computes ID, BatchRequest reaches receiver, sampler resolves via local cache, ring buffer contains denormalized sample.
- Cross-node resolution: gateway G1 mints a new combo; sampler on N2 (which hasn't seen it) hits cache miss → KV read → resolution succeeds.
- Resolution timeout: simulate slow KV; verify sampler stays within tick budget and degrades to `<unknown>`.
- System table TTL behavior: confirm rows persist across node restarts; cache reconstitutes via lazy lookups.

**Mixed-version tests:**
- Old binary receives BatchRequest with `enrichment_id` set: ignores it, uses legacy fields, sampler emits sample with legacy attribution.
- New binary receives BatchRequest with only legacy fields set (from old binary): uses legacy fields, sampler emits sample with legacy attribution.
- Mixed cluster: rolling upgrade across the version gate; ASH samples remain attributed throughout (some via new path, some via legacy).
- Phase 2 transition: confirm legacy field removal does not break upgrade from Phase 1 binaries.

**Load / cardinality tests:**
- Synthetic high-cardinality workload (10K combos) on a small cluster: verify cache size, eviction, and discarded behavior. Confirm sampling stays within tick budget.
- Pathological long-tail workload (combo emerges every minute): confirm functional warmup behavior matches the assumed Zipfian model.

## Future shapes

The pattern (hash-based ID + system-table backing + per-node cache + eager sample-time resolution) generalizes to any attribution shape that meets two criteria:

1. **Attributes are produced at one point in the request's lifetime and consumed at another** (so they need to flow on the wire).
2. **Many work items share the same attribute set** (so cache hit rate is high and the system table stays bounded).

Likely future instances:

| Shape | Producer | Consumer | Notes |
|---|---|---|---|
| **JobAttributes** | Job adoption (`pkg/jobs/adopt.go`) | ASH samples on KV nodes | Replaces `workload_id = job_id`. Adds richer per-job context (job_type, owner, parent execution). |
| **ReplicationAttributes** | LDR/PCR consumer | Downstream samplers | For attributing replication-stream-driven work. |
| **KVAttributes** | KV layer at request evaluation | Raft / replica samplers | For attributing the KV execution engine, scan direction, or other KV-layer attributes. |

Shapes that **do not** fit this pattern and need different mechanisms:

- **Resolver batches** (intent resolution, async cleanup): one record represents work done on behalf of many parents. Schema needs a list, not a flat row.
- **Per-request KV operations** within a batch: cardinality is per-individual-request, exploding the cache.

When a future shape is added: define `<Shape>AttributesID` as its own typed `uint64`, create `system.<shape>_attributes`, mirror the cache/write/resolution code from `pkg/obs/executionattributes`. The wire field `enrichment_id` is reused; `workload_type` (or its successor discriminator) tells the consumer which table to resolve against.

No framework abstraction is built here. The pattern is small enough to copy (~200 lines per shape), and the divergent-shape cases (resolver batches, per-request) need different mechanisms regardless.

## References

- [Cardinality investigation](./2026-05-05-ash-enrichment-cardinality-investigation.md) — empirical data backing cache sizing, no-pre-warm, and discarded-metric decisions.
- Google Doc *Execution Attributes: A solution for the attribution problem in observability systems* — high-level vision this design narrows to one shape.
- `pkg/obs/ash/types.go:55` — existing TODO comment foreshadowing this work.
- `pkg/obs/workloadid/workloadid.go:131` — existing `WorkloadType` discriminator.
- `pkg/sql/conn_executor_exec.go:662` — existing `WorkloadID` / `AppNameID` stamping site.
- `pkg/obs/ash/sampler.go:472` — existing sampler-side resolution path with `appNameMap` fallback.
