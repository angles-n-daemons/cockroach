# ASH Enrichment Cache: Cardinality Investigation

**Date:** 2026-05-05
**Author:** Brian Dillmann (with research support)
**Status:** Background research for the ASH sample enrichment design

## Purpose

The ASH sample enrichment project introduces a per-node in-memory cache that maps an `EnrichmentID` (hash of `(stmt_fingerprint_id, app_name)`) to its denormalized values. The cache is backed by a new system table `system.execution_attributes`. This investigation grounds three design decisions in production data:

1. **Default cache size** for the per-node `EnrichmentID → row` cache.
2. **Whether to pre-warm** the cache at node startup.
3. **Whether to expose a "discarded" metric** for operator visibility.

## Method

Two telemetry sources, queried for the 7-day window 2026-04-28 through 2026-05-04:

- **Snowflake** (`DATAMART_PROD.TELEMETRY.STMTSTATS_CLOUD`): per-cluster aggregates of `system.statement_statistics` rows, used to compute distinct `(QUERY, APP)` cardinality per cluster.
- **Datadog** (`cockroachdb.sql.stats.discarded.current`): per-cluster timeseries of statements discarded from the in-memory `system.statement_statistics` cache because the per-node `uniqueStmtCount` limit (default 100,000) was reached.

## Findings

### Cardinality distribution (2,845 cloud clusters, last 7 days)

Distinct `(stmt_fingerprint, app_name)` combos per cluster:

| Percentile | Combos |
|---|---|
| p50 | **39** |
| p75 | 115 |
| p90 | 287 |
| p99 | 2,311 |
| max observed | 91,347 |

Bucket distribution:

| Bucket | Clusters | Share |
|---|---|---|
| < 100 combos | 2,051 | 72% |
| 100–500 | 639 | 22% |
| 500–1K | 81 | 3% |
| 1K–5K | 62 | 2% |
| 5K–10K | 3 | 0.1% |
| 10K–50K | 8 | 0.3% |
| 50K–100K | 1 | 0.04% |

**Top 5 highest-cardinality clusters:**

| Cluster | Distinct fingerprints | Distinct apps | Combos |
|---|---|---|---|
| #1 | 1,830 | 2,501 | 91,347 |
| #2 | 67 | 11,092 | 48,079 |
| #3 | 48 | 14,823 | 47,642 |
| #4 | 709 | 760 | 22,140 |
| #5 | 1,265 | 743 | 20,263 |

The top three high-cardinality clusters are dominated by *app-name* cardinality, not fingerprint cardinality — typical of multi-tenant SaaS deployments where each customer or worker process gets a unique `application_name`.

### Discarded statements (top of fleet, last 7 days)

| Rank | Cluster | 7-day discards |
|---|---|---|
| 1 | crl-prod-rgd | 2,321,248,322 (2.32 **billion**) |
| 2 | crl-prod-sd7 | 1,050,685,161 (1.05 **billion**) |
| 3 | crl-prod-rqv | 924,757,014 |
| 4 | crl-prod-hzt | 223,085,885 |
| 5 | crl-prod-v3k | 108,447,236 |
| 6–11 | crl-prod-{v2f, r2v, 6cx, t2n, vt5, kd4} | 31M–41M each |
| 12–13 | crl-prod-{qqw, v28} | 7M–12M |
| 14–15 | crl-prod-{tmg, j77} | 820K–900K |
| 16+ | rest | <300K |

### Discard counts overstate true cardinality

Both of the following are true and mean the discard counts inflate dramatically relative to our cache's actual sizing problem:

**1. `system.statement_statistics` includes `transaction_fingerprint_id` in its primary key.**

```
PRIMARY KEY (aggregated_ts, fingerprint_id, transaction_fingerprint_id, plan_hash, app_name, node_id)
```
Source: `pkg/sql/catalog/systemschema/system.go:728`

A single `(stmt_fp, app_name)` combo can produce many stmt_stats rows depending on how many transaction shapes the statement appears in. Our enrichment cache does not include `transaction_fingerprint_id`, so we have no equivalent multiplier.

**2. The `sql.stats.discarded.current` metric is a per-event counter.**

```go
err := appStats.RecordStatement(ctx, stmt)
if err != nil { discardedCount++ }
...
s.discardedStatsCount.Inc(discardedCount)
```
Source: `pkg/sql/sqlstats/sslocal/sql_stats.go:216`

Same combo discarded a million times → counter goes up a million. The metric measures churn/cycle rate, not unique cardinality. A workload with 200K active fingerprints thrashing in a 100K cache can produce billions of discards without those fingerprints being unique.

**Implication:** the billions of discards observed for top clusters are heavily inflated by both effects. The telemetry-derived combo counts (max 91K observed) are likely a much better upper bound on our cache cardinality — though even those are clipped by the in-memory `uniqueStmtCount` limit, so true `(fingerprint, app_name)` cardinality on the worst-case clusters could be in the 100K–1M range. Still, our cache is structurally safer than the existing stmt_stats cache:

- No transaction_fingerprint_id multiplier
- Deduplication by combo (one cache entry per unique combo regardless of how many times it's seen)

### Warmup behavior (p99 cluster, ~2,311 combos)

SQL workloads are heavily Zipfian — a small number of combos account for the bulk of executions. Two definitions of "warm":

- **Functional warmup** (cache holds the combos that account for ~99% of execution traffic): typically the top ~200 combos. For an active cluster doing 100+ statements/sec, this is reached in **seconds to a minute**.
- **Full warmup** (every combo seen at least once): bounded by the rarest combo's inter-arrival time. May take hours; some combos may never reappear.

After functional warmup, sampler-side cache miss rate should be ~1% — matching the share of execution traffic that hits the rare long tail. With ~1000 samples per sampler tick:

- ~10 cache-miss reads per tick
- ~1ms per KV read
- ~10ms of sampler work in a 1-second tick budget

This fits comfortably; even an order-of-magnitude spike during cold start or workload change stays within budget.

## Decisions informed by this investigation

1. **Default cache size: 16,384 entries (~1.5 MB).** Covers >99% of clusters comfortably with headroom. Configurable via cluster setting (`obs.execution_attributes.cache_size`) for the high-cardinality tail.
2. **No pre-warming at node startup.** Functional warmup is fast enough (seconds to minutes) that the complexity of a startup scan of `system.execution_attributes` is not justified. Lazy population on cache miss handles it.
3. **Expose discard observability:**
   - Metric: `obs.execution_attributes.discarded` (counter, per-event semantics matching `sql.stats.discarded.current`) — increments when an entry is evicted before it's been written through to the system table, indicating cache pressure.
   - Log line at sustained discard rates so operators investigating ASH gaps have a breadcrumb pointing at the cache_size setting.
4. **High-cardinality clusters inherit the existing observability gap.** Our enrichment system does not create new cardinality problems for clusters already hitting `uniqueStmtCount` limits in stmt_stats. It will degrade similarly (cache misses → `<unknown>` columns on samples) and operators on those clusters can either bump the cache size cluster setting or accept the gap.

## Followups (out of scope for this release)

- Underlying high-cardinality root cause (apps with embedded customer IDs, fingerprints not aggregating identifier patterns) is a SQL Observability concern — not addressed by this project.
- Future shapes (job attribution, replication attribution, etc.) will face similar cardinality questions; this investigation establishes the pattern but each new shape should re-validate against its own data.
