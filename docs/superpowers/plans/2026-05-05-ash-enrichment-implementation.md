# ASH Sample Enrichment Implementation Plan (POC)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a proof-of-concept implementation of ASH sample enrichment per the [design doc](../specs/2026-05-05-ash-enrichment-design.md) — a hash-derived `enrichment_id` on the wire, a system-table-backed cache, and eager sample-time denormalization for ASH samples on statement-shaped work.

**Architecture:** New `pkg/obs/executionattributes` package owns ID computation, the per-node cache, the background system-table writer, and the sampler-side resolver. `kvpb.RequestHeader` gets a new `enrichment_id` field added alongside (not replacing) the existing `workload_id` / `app_name_id` fields. Cluster version `V26_3_AddExecutionAttributesTable` gates the new behavior.

**Tech Stack:** Go, CRDB internal frameworks (system tables, cluster versions, upgrade migrations, internal executor), protobuf, xxhash.

**POC framing:**
- This is a proof of concept, not a production-ready release. Each commit is structured *as if* it were a release-version increment so the migration story is clearly visible in the commit progression. Tests are kept minimal — one or two focused tests per task — to demonstrate the behavior, not to exhaustively cover edge cases.
- **Each task = one commit = one mergeable, mixed-version-safe step in the migration path.** Within a task, multiple TDD steps build up the change; the final step commits.
- Phase 2 of the migration (removing the legacy `app_name_id` field, deleting `appNameMap`) lives in a future release and is **out of scope** for this plan. Only Phase 1 is built.

---

## File structure

**New package: `pkg/obs/executionattributes/`**

| File | Responsibility |
|---|---|
| `doc.go` | Package documentation, brief design summary |
| `id.go` | `ExecutionAttributesID` type, canonical encoding, hash function |
| `cache.go` | LRU cache wrapper, eviction tracking |
| `writer.go` | Background goroutine that drains the write queue and inserts to `system.execution_attributes` |
| `resolver.go` | Sampler-side resolution path (cache lookup + bounded sync KV read) |
| `metrics.go` | Metric definitions (cache_size, evictions, discarded, unresolved_samples, collisions, write_errors) |
| `cluster_settings.go` | Cluster setting registrations |
| `executionattributes.go` | Public API, factory functions, lifecycle wiring |
| `id_test.go`, `cache_test.go`, `writer_test.go`, `resolver_test.go` | Unit tests |
| `BUILD.bazel` | Bazel build file |

**Modified files (with brief role):**

| File | What changes |
|---|---|
| `pkg/clusterversion/cockroach_versions.go` | Add `V26_3_AddExecutionAttributesTable` version gate |
| `pkg/sql/catalog/systemschema/system.go` | Add `system.execution_attributes` table descriptor |
| `pkg/upgrade/upgrades/v26_3_add_execution_attributes_table.go` (new) | Bootstrap migration creating the table |
| `pkg/kv/kvpb/api.proto` | Add `uint64 enrichment_id` field to `RequestHeader` |
| `pkg/sql/conn_executor_exec.go` | Compute and stamp `enrichment_id` (gated by cluster version) |
| `pkg/sql/instrumentation.go` | Cache `enrichment_id` on `instrumentationHelper` |
| `pkg/obs/ash/types.go` | Add `EnrichmentID` field to `WorkState` and `ASHSample` |
| `pkg/obs/ash/work_state.go` | Propagate `EnrichmentID` through `SetWorkState` |
| `pkg/obs/ash/sampler.go` | Resolve `EnrichmentID` at sample-write time via the resolver |
| `pkg/sql/crdb_internal.go` | Add `enrichment_id` column to `cluster_active_session_history` virtual table |
| `pkg/server/serverpb/status.proto` | Add `enrichment_id` field to `ASHSample` proto |
| `pkg/server/server_sql.go` | Wire the executionattributes singleton into the SQL server |
| `pkg/kv/txn.go` | Propagate `enrichment_id` from `Txn` → `BatchRequest.Header` |

---

## Tasks

### Task 1: Add cluster version gate

**Files:**
- Modify: `pkg/clusterversion/cockroach_versions.go`

The version gate is the foundation; no other commit can predicate behavior on it without it existing first.

- [ ] **Step 1: Add the version constant**

In `pkg/clusterversion/cockroach_versions.go`, in the `const (...)` block where existing `V26_3_*` versions are declared, add:

```go
// V26_3_AddExecutionAttributesTable adds the system.execution_attributes
// table that stores the mapping from enrichment_id to its underlying
// (stmt_fingerprint_id, app_name) tuple. See pkg/obs/executionattributes.
V26_3_AddExecutionAttributesTable
```

In the `versionsSingleton` map below, add (use the next available `Internal` number for the V26_3 cycle — likely `Internal: 8` if `V26_3_AddAdvisoryLocksTable` is at `Internal: 6`, but verify by reading the file first):

```go
V26_3_AddExecutionAttributesTable: {Major: 26, Minor: 2, Internal: 8},
```

- [ ] **Step 2: Build to confirm the constant is recognized**

Run: `./dev build pkg/clusterversion`
Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add pkg/clusterversion/cockroach_versions.go
git commit -m "$(cat <<'EOF'
clusterversion: add V26_3_AddExecutionAttributesTable

This version gate will guard the introduction of the
system.execution_attributes table and the new enrichment_id
field on BatchRequest.Header. No behavior change in this commit;
the gate is added in isolation so subsequent commits can predicate
on it.

Release note: None
EOF
)"
```

---

### Task 2: Add `system.execution_attributes` table

**Files:**
- Modify: `pkg/sql/catalog/systemschema/system.go`
- Create: `pkg/upgrade/upgrades/v26_3_add_execution_attributes_table.go`
- Create: `pkg/upgrade/upgrades/v26_3_add_execution_attributes_table_test.go`

This commit introduces the durable storage. Nothing reads or writes it yet — it's a no-op from a runtime perspective. Mergeable in isolation.

- [ ] **Step 1: Add the table descriptor in systemschema**

In `pkg/sql/catalog/systemschema/system.go`, add (placing alongside other table SQL definitions like `SystemStatementsTableSchema`):

```go
ExecutionAttributesTableSchema = `
CREATE TABLE system.execution_attributes (
    id                  INT8       NOT NULL,
    stmt_fingerprint_id BYTES      NOT NULL,
    app_name            STRING     NOT NULL,
    created_at          TIMESTAMP  NOT NULL DEFAULT now(),
    CONSTRAINT "primary" PRIMARY KEY (id),
    FAMILY "primary" (id, stmt_fingerprint_id, app_name, created_at)
)`
```

Then add a `MakeExecutionAttributesTable()` helper following the pattern of the existing `MakeSystemStatementsTable()` (search for the latter to copy the structure). Register the table in the `MakeMetadataSchema` function (or whatever the equivalent registration point is — follow the `system.statements` precedent from V26_2).

- [ ] **Step 2: Add the table ID constant**

In `pkg/keys/constants.go` (or `pkg/keys/keys.go` — find the `SystemStatementsTableID` definition; add the new ID right after it):

```go
ExecutionAttributesTableID = <next-unused-id>
```

Find the next unused ID by reading the file (`grep -n "TableID = " pkg/keys/constants.go | tail -5` shows the current top of the namespace).

- [ ] **Step 3: Create the migration file**

Create `pkg/upgrade/upgrades/v26_3_add_execution_attributes_table.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package upgrades

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/clusterversion"
	"github.com/cockroachdb/cockroach/pkg/keys"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/systemschema"
	"github.com/cockroachdb/cockroach/pkg/upgrade"
)

func addExecutionAttributesTableMigration(
	ctx context.Context, version clusterversion.ClusterVersion, deps upgrade.TenantDeps,
) error {
	return createSystemTable(
		ctx, deps.DB, deps.Settings, deps.Codec,
		systemschema.ExecutionAttributesTable,
		tree.LocalityLevelTable,
	)
}
```

(`createSystemTable` may have a slightly different signature in the current codebase — copy the pattern from a recent `_add_*_table.go` migration like `v26_2_add_system_statements_table.go`.)

- [ ] **Step 4: Register the migration**

In `pkg/upgrade/upgrades/upgrades.go` (or the registration file — find where existing migrations like `addSystemStatementsTableMigration` are registered in the `registry` map). Add an entry:

```go
clusterversion.V26_3_AddExecutionAttributesTable: upgrade.NewTenantUpgrade(
    "add system.execution_attributes table",
    toCV(clusterversion.V26_3_AddExecutionAttributesTable),
    upgrade.NoPrecondition,
    addExecutionAttributesTableMigration,
    upgrade.RestoreActionNotRequired("new system table; restored data does not reference it"),
),
```

- [ ] **Step 5: Update bootstrap test data**

CRDB has golden files for the bootstrap state. After adding a new system table, the bootstrap hash and golden test data must be regenerated. Run:

```bash
./dev test pkg/sql/catalog/bootstrap -- --test_arg=-rewrite
./dev test pkg/sql/catalog/systemschema -- --test_arg=-rewrite
```

Verify that the resulting diffs only include the new table's bytes and metadata.

- [ ] **Step 6: Write a basic migration test**

Create `pkg/upgrade/upgrades/v26_3_add_execution_attributes_table_test.go`. Copy the structure of `v26_2_add_system_statements_table_test.go`. Minimum viable test: bring up a test cluster at the previous cluster version, run the upgrade to `V26_3_AddExecutionAttributesTable`, verify the table now exists and accepts an INSERT.

```go
package upgrades_test

import (
	"context"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/clusterversion"
	"github.com/cockroachdb/cockroach/pkg/server"
	"github.com/cockroachdb/cockroach/pkg/testutils/serverutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/sqlutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/testcluster"
	"github.com/cockroachdb/cockroach/pkg/upgrade/upgrades"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
)

func TestExecutionAttributesTableMigration(t *testing.T) {
	defer leaktest.AfterTest(t)()

	ctx := context.Background()
	clusterArgs := base.TestClusterArgs{
		ServerArgs: base.TestServerArgs{
			Knobs: base.TestingKnobs{
				Server: &server.TestingKnobs{
					ClusterVersionOverride:         clusterversion.MinSupported.Version(),
					BootstrapVersionKeyOverride:    clusterversion.MinSupported,
					DisableAutomaticVersionUpgrade: make(chan struct{}),
				},
			},
		},
	}

	tc := testcluster.StartTestCluster(t, 1, clusterArgs)
	defer tc.Stopper().Stop(ctx)

	db := tc.ServerConn(0)
	tdb := sqlutils.MakeSQLRunner(db)

	// Verify table doesn't exist pre-migration.
	tdb.ExpectErr(t, "does not exist", "SELECT * FROM system.execution_attributes")

	// Trigger the upgrade.
	upgrades.Upgrade(t, db, clusterversion.V26_3_AddExecutionAttributesTable, nil, false)

	// Verify table exists and accepts inserts.
	tdb.Exec(t, "INSERT INTO system.execution_attributes (id, stmt_fingerprint_id, app_name) VALUES (1, '\x00\x00\x00\x00\x00\x00\x00\x01', 'testapp')")
	row := tdb.QueryRow(t, "SELECT app_name FROM system.execution_attributes WHERE id = 1")
	var got string
	row.Scan(&got)
	if got != "testapp" {
		t.Fatalf("expected testapp, got %s", got)
	}
}
```

- [ ] **Step 7: Run the migration test**

Run: `./dev test pkg/upgrade/upgrades -f TestExecutionAttributesTableMigration -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/sql/catalog/systemschema/system.go pkg/keys/ pkg/upgrade/upgrades/v26_3_add_execution_attributes_table.go pkg/upgrade/upgrades/v26_3_add_execution_attributes_table_test.go pkg/upgrade/upgrades/upgrades.go
# Also include any regenerated bootstrap golden files
git add pkg/sql/catalog/bootstrap/
git commit -m "$(cat <<'EOF'
sql: add system.execution_attributes table

Adds the system table that backs ash sample enrichment. Stores
the mapping (id, stmt_fingerprint_id, app_name, created_at) where
id is a 64-bit hash of the (stmt_fingerprint_id, app_name) tuple.

The table is bootstrapped via the V26_3_AddExecutionAttributesTable
cluster upgrade. No code reads or writes the table yet; this commit
introduces storage in isolation.

Release note: None
EOF
)"
```

---

### Task 3: Create `executionattributes` package skeleton with ID type

**Files:**
- Create: `pkg/obs/executionattributes/doc.go`
- Create: `pkg/obs/executionattributes/id.go`
- Create: `pkg/obs/executionattributes/id_test.go`
- Create: `pkg/obs/executionattributes/BUILD.bazel`

This commit introduces the typed identifier and its hash function. Pure library; no system integration. Mergeable in isolation.

- [ ] **Step 1: Write the failing test**

Create `pkg/obs/executionattributes/id_test.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"encoding/binary"
	"testing"
)

func TestComputeIDIsDeterministic(t *testing.T) {
	stmtFP := uint64ToBytes(0xDEADBEEF)
	appName := "myapp"

	id1 := ComputeID(stmtFP, appName)
	id2 := ComputeID(stmtFP, appName)

	if id1 != id2 {
		t.Fatalf("expected deterministic IDs, got %d and %d", id1, id2)
	}
	if id1 == 0 {
		t.Fatalf("expected nonzero ID, got 0 (reserved sentinel)")
	}
}

func TestComputeIDDistinguishesInputs(t *testing.T) {
	stmtFP := uint64ToBytes(0xDEADBEEF)
	id1 := ComputeID(stmtFP, "app1")
	id2 := ComputeID(stmtFP, "app2")
	id3 := ComputeID(uint64ToBytes(0xCAFEBABE), "app1")

	if id1 == id2 {
		t.Fatalf("expected app variation to produce different IDs")
	}
	if id1 == id3 {
		t.Fatalf("expected fingerprint variation to produce different IDs")
	}
}

func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}
```

- [ ] **Step 2: Verify the test fails**

Run: `./dev test pkg/obs/executionattributes -f TestComputeID -v`
Expected: FAIL with "package not found" or "ComputeID not defined".

- [ ] **Step 3: Implement the ID type**

Create `pkg/obs/executionattributes/doc.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

// Package executionattributes implements ash sample enrichment for
// statement-shaped work. It introduces a typed ID derived from the
// hash of (stmt_fingerprint_id, app_name), backed by the
// system.execution_attributes table, and provides per-node caches
// and a sampler-side resolver. See:
//
//   docs/superpowers/specs/2026-05-05-ash-enrichment-design.md
package executionattributes
```

Create `pkg/obs/executionattributes/id.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"github.com/cespare/xxhash/v2"
)

// ID is the typed identifier for an execution-shaped attribution
// context. Computed as xxhash64 of the canonical encoding of
// (stmt_fingerprint_id, app_name). The zero value is reserved as a
// sentinel for "unattributed" or "resolution failed".
type ID uint64

// ComputeID returns the deterministic ID for the given attribute set.
// Same inputs always yield the same ID, regardless of node.
func ComputeID(stmtFingerprintID []byte, appName string) ID {
	h := xxhash.New()
	// Length prefix prevents (a, b) || (c) and (a) || (b, c) from colliding.
	var lenBuf [8]byte
	writeLenPrefixed(h, lenBuf[:], stmtFingerprintID)
	writeLenPrefixed(h, lenBuf[:], []byte(appName))
	id := ID(h.Sum64())
	if id == 0 {
		// Avoid the sentinel by perturbing; collision risk is negligible.
		id = ID(1)
	}
	return id
}

func writeLenPrefixed(h interface{ Write([]byte) (int, error) }, lenBuf, data []byte) {
	for i := 0; i < 8; i++ {
		lenBuf[i] = byte(len(data) >> (i * 8))
	}
	_, _ = h.Write(lenBuf)
	_, _ = h.Write(data)
}
```

Create `pkg/obs/executionattributes/BUILD.bazel`:

```python
load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "executionattributes",
    srcs = [
        "doc.go",
        "id.go",
    ],
    importpath = "github.com/cockroachdb/cockroach/pkg/obs/executionattributes",
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_cespare_xxhash_v2//:xxhash",
    ],
)

go_test(
    name = "executionattributes_test",
    srcs = [
        "id_test.go",
    ],
    embed = [":executionattributes"],
)
```

- [ ] **Step 4: Run the tests**

Run: `./dev generate bazel` (refresh BUILD.bazel after new files)
Then: `./dev test pkg/obs/executionattributes -v`
Expected: PASS for both tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/obs/executionattributes/
git commit -m "$(cat <<'EOF'
obs/executionattributes: introduce ID type and hash function

Defines the typed identifier ExecutionAttributesID = uint64 and
the deterministic hash function ComputeID(stmt_fingerprint_id,
app_name) -> ID. The zero value is reserved as a sentinel for
unattributed work.

This is a pure library introduction; no system integration yet.

Release note: None
EOF
)"
```

---

### Task 4: Add per-node cache and cluster settings

**Files:**
- Create: `pkg/obs/executionattributes/cache.go`
- Create: `pkg/obs/executionattributes/cache_test.go`
- Create: `pkg/obs/executionattributes/cluster_settings.go`
- Modify: `pkg/obs/executionattributes/BUILD.bazel`

This commit adds the in-memory cache. No system integration; just the data structure and its API. Mergeable in isolation.

- [ ] **Step 1: Write the failing tests**

Create `pkg/obs/executionattributes/cache_test.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"testing"
)

func TestCachePutGet(t *testing.T) {
	c := NewCache(8)
	id := ComputeID([]byte("fp1"), "app1")
	entry := Entry{StmtFingerprintID: []byte("fp1"), AppName: "app1"}

	c.Put(id, entry)
	got, ok := c.Get(id)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if got.AppName != "app1" {
		t.Fatalf("expected app1, got %s", got.AppName)
	}
}

func TestCacheEvictsLRU(t *testing.T) {
	c := NewCache(2)
	id1 := ID(1)
	id2 := ID(2)
	id3 := ID(3)
	c.Put(id1, Entry{AppName: "a"})
	c.Put(id2, Entry{AppName: "b"})
	c.Put(id3, Entry{AppName: "c"}) // evicts id1

	if _, ok := c.Get(id1); ok {
		t.Fatalf("expected id1 to be evicted")
	}
	if _, ok := c.Get(id2); !ok {
		t.Fatalf("expected id2 to remain")
	}
	if _, ok := c.Get(id3); !ok {
		t.Fatalf("expected id3 to remain")
	}
}
```

- [ ] **Step 2: Verify tests fail**

Run: `./dev test pkg/obs/executionattributes -f TestCache -v`
Expected: FAIL.

- [ ] **Step 3: Implement the cache**

Create `pkg/obs/executionattributes/cache.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"container/list"
	"sync"
)

// Entry is the resolved attribute set for an ID.
type Entry struct {
	StmtFingerprintID []byte
	AppName           string
}

// Cache is a thread-safe LRU cache from ID to Entry.
type Cache struct {
	mu       sync.Mutex
	maxSize  int
	entries  map[ID]*list.Element
	lru      *list.List // front = most recent
}

type cacheItem struct {
	id    ID
	entry Entry
}

// NewCache creates an LRU cache with the given max entry count.
func NewCache(maxSize int) *Cache {
	return &Cache{
		maxSize: maxSize,
		entries: make(map[ID]*list.Element, maxSize),
		lru:     list.New(),
	}
}

// Get returns the entry for id and marks it most recently used.
func (c *Cache) Get(id ID) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[id]
	if !ok {
		return Entry{}, false
	}
	c.lru.MoveToFront(el)
	return el.Value.(*cacheItem).entry, true
}

// Put inserts or updates the entry for id, evicting the LRU entry
// if the cache is over capacity.
func (c *Cache) Put(id ID, entry Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[id]; ok {
		el.Value.(*cacheItem).entry = entry
		c.lru.MoveToFront(el)
		return
	}
	el := c.lru.PushFront(&cacheItem{id: id, entry: entry})
	c.entries[id] = el
	for c.lru.Len() > c.maxSize {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		delete(c.entries, oldest.Value.(*cacheItem).id)
		c.lru.Remove(oldest)
	}
}

// Size returns the current number of entries.
func (c *Cache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}
```

- [ ] **Step 4: Add cluster settings**

Create `pkg/obs/executionattributes/cluster_settings.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"time"

	"github.com/cockroachdb/cockroach/pkg/settings"
)

// CacheSize is the maximum number of entries in the per-node cache.
var CacheSize = settings.RegisterIntSetting(
	settings.ApplicationLevel,
	"obs.execution_attributes.cache_size",
	"maximum number of entries in the per-node execution-attributes cache",
	16384,
	settings.PositiveInt,
)

// WriteQueueSize is the bound on the per-node durable write queue.
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
```

- [ ] **Step 5: Update BUILD.bazel**

Run: `./dev generate bazel` to regenerate.

- [ ] **Step 6: Run the tests**

Run: `./dev test pkg/obs/executionattributes -v`
Expected: PASS for all four tests (id and cache).

- [ ] **Step 7: Commit**

```bash
git add pkg/obs/executionattributes/
git commit -m "$(cat <<'EOF'
obs/executionattributes: add LRU cache and cluster settings

Adds the per-node Cache type (thread-safe LRU keyed by ID, returning
Entry { StmtFingerprintID, AppName }) and the cluster settings that
govern its sizing and resolution timeouts.

Cache size defaults to 16384 entries (~1.5 MB), sized for >99% of
the cloud fleet per the cardinality investigation. Operators on
high-cardinality clusters can tune via cluster settings.

Release note: None
EOF
)"
```

---

### Task 5: Add background writer for system table

**Files:**
- Create: `pkg/obs/executionattributes/writer.go`
- Create: `pkg/obs/executionattributes/writer_test.go`
- Create: `pkg/obs/executionattributes/metrics.go`
- Modify: `pkg/obs/executionattributes/BUILD.bazel`

This commit adds the goroutine that drains pending writes to `system.execution_attributes`. Tests use a mock executor; no real KV interaction yet.

- [ ] **Step 1: Define the metrics**

Create `pkg/obs/executionattributes/metrics.go`:

```go
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
		Measurement: "entries",
		Unit:        metric.Unit_COUNT,
	}
	metaEvictions = metric.Metadata{
		Name:        "obs.execution_attributes.evictions",
		Help:        "Cumulative cache evictions",
		Measurement: "evictions",
		Unit:        metric.Unit_COUNT,
	}
	metaDiscarded = metric.Metadata{
		Name:        "obs.execution_attributes.discarded",
		Help:        "Cache entries evicted before durable write completed; or write-queue overflows",
		Measurement: "discards",
		Unit:        metric.Unit_COUNT,
	}
	metaCollisions = metric.Metadata{
		Name:        "obs.execution_attributes.collisions",
		Help:        "Hash collisions detected",
		Measurement: "collisions",
		Unit:        metric.Unit_COUNT,
	}
	metaUnresolvedSamples = metric.Metadata{
		Name:        "obs.execution_attributes.unresolved_samples",
		Help:        "ASH samples written with NULL denormalized columns due to resolution failure",
		Measurement: "samples",
		Unit:        metric.Unit_COUNT,
	}
)

// Metrics is the per-node metric struct for execution attributes.
type Metrics struct {
	CacheSize         *metric.Gauge
	Evictions         *metric.Counter
	Discarded         *metric.Counter
	Collisions        *metric.Counter
	UnresolvedSamples *metric.Counter
}

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
```

- [ ] **Step 2: Write the failing writer test**

Create `pkg/obs/executionattributes/writer_test.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeExecutor struct {
	mu    sync.Mutex
	wrote []writeRequest
}

func (f *fakeExecutor) insertExecutionAttributes(ctx context.Context, req writeRequest) (existing Entry, conflict bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wrote = append(f.wrote, req)
	return Entry{}, false, nil
}

func (f *fakeExecutor) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.wrote)
}

func TestWriterDrainsQueue(t *testing.T) {
	exec := &fakeExecutor{}
	metrics := NewMetrics()
	w := NewWriter(exec, &metrics, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Enqueue(writeRequest{ID: 1, Entry: Entry{StmtFingerprintID: []byte("fp"), AppName: "a"}})
	w.Enqueue(writeRequest{ID: 2, Entry: Entry{StmtFingerprintID: []byte("fp2"), AppName: "b"}})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if exec.writeCount() == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected 2 writes, got %d", exec.writeCount())
}

func TestWriterOverflowDropsAndCounts(t *testing.T) {
	exec := &fakeExecutor{}
	metrics := NewMetrics()
	w := NewWriter(exec, &metrics, 1)

	// Don't start the writer goroutine — so the queue can't drain.
	w.Enqueue(writeRequest{ID: 1, Entry: Entry{}})
	w.Enqueue(writeRequest{ID: 2, Entry: Entry{}}) // should be dropped

	if got := metrics.Discarded.Count(); got != 1 {
		t.Fatalf("expected 1 discard, got %d", got)
	}
}
```

- [ ] **Step 3: Verify tests fail**

Run: `./dev test pkg/obs/executionattributes -f TestWriter -v`
Expected: FAIL.

- [ ] **Step 4: Implement the writer**

Create `pkg/obs/executionattributes/writer.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"
	"time"

	"github.com/cockroachdb/cockroach/pkg/util/log"
)

// writeRequest is one pending write to system.execution_attributes.
type writeRequest struct {
	ID    ID
	Entry Entry
}

// executor is the minimal interface the writer needs from the
// system-table writer (real impl uses an InternalExecutor; tests use a fake).
type executor interface {
	// insertExecutionAttributes inserts the row idempotently. If a row
	// already exists at the same ID with a different (fp, app_name)
	// tuple, it returns the existing row with conflict=true.
	insertExecutionAttributes(ctx context.Context, req writeRequest) (existing Entry, conflict bool, err error)
}

// Writer drains pending writes onto the system.execution_attributes table.
type Writer struct {
	exec    executor
	metrics *Metrics
	queue   chan writeRequest
}

// NewWriter creates a Writer with a bounded queue.
func NewWriter(exec executor, metrics *Metrics, queueSize int) *Writer {
	return &Writer{
		exec:    exec,
		metrics: metrics,
		queue:   make(chan writeRequest, queueSize),
	}
}

// Enqueue adds a write request, dropping if the queue is full.
func (w *Writer) Enqueue(req writeRequest) {
	select {
	case w.queue <- req:
	default:
		w.metrics.Discarded.Inc(1)
	}
}

// Run drains the queue until ctx is cancelled.
func (w *Writer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-w.queue:
			w.process(ctx, req)
		}
	}
}

func (w *Writer) process(ctx context.Context, req writeRequest) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		existing, conflict, err := w.exec.insertExecutionAttributes(ctx, req)
		if err == nil {
			if conflict {
				w.metrics.Collisions.Inc(1)
				log.Warningf(ctx,
					"execution_attributes hash collision: id=%d existing=%+v new=%+v",
					req.ID, existing, req.Entry)
			}
			return
		}
		lastErr = err
		time.Sleep(time.Duration(50*(1<<attempt)) * time.Millisecond)
	}
	log.Warningf(ctx, "execution_attributes write failed after retries: id=%d err=%v", req.ID, lastErr)
}
```

- [ ] **Step 5: Update BUILD.bazel**

Run: `./dev generate bazel`

- [ ] **Step 6: Run the tests**

Run: `./dev test pkg/obs/executionattributes -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/obs/executionattributes/
git commit -m "$(cat <<'EOF'
obs/executionattributes: add background writer and metrics

Introduces Writer (drains a bounded queue of writeRequests onto
system.execution_attributes via an injected executor interface) and
Metrics (cache_size, evictions, discarded, collisions,
unresolved_samples).

Writes are idempotent: insert with ON CONFLICT DO NOTHING + verify
the existing row matches. Mismatch implies a hash collision; the
collisions counter increments and a WARNING is logged.

Queue overflow drops the new entry (in-flight writes proceed) and
increments the discarded counter.

Release note: None
EOF
)"
```

---

### Task 6: Add `enrichment_id` field to `BatchRequest.Header`

**Files:**
- Modify: `pkg/kv/kvpb/api.proto`
- Modify: `pkg/kv/kvpb/api.pb.go` (regenerated)

This commit adds the new wire field. No reader or writer in this commit; old binaries ignore the unknown field. Mergeable in isolation.

- [ ] **Step 1: Find the next available proto field tag**

Read `pkg/kv/kvpb/api.proto`. Locate the `RequestHeader` message and find the `WorkloadID`, `AppNameID`, `WorkloadType` fields. Note the highest existing tag number on that message; the new field gets the next.

- [ ] **Step 2: Add the field**

In `pkg/kv/kvpb/api.proto`, in the `RequestHeader` message, add (assume the next tag is `42` — verify and adjust):

```proto
// EnrichmentID is the ash sample enrichment identifier for
// statement-shaped work. When set, the receiver should resolve
// it via system.execution_attributes to retrieve the underlying
// (stmt_fingerprint_id, app_name) tuple. See
// pkg/obs/executionattributes.
//
// Mutually exclusive (by workload_type) with workload_id:
// statement-shaped work uses enrichment_id; job- and system-shaped
// work continues to use workload_id.
uint64 enrichment_id = 42 [(gogoproto.casttype) = "github.com/cockroachdb/cockroach/pkg/obs/executionattributes.ID"];
```

- [ ] **Step 3: Regenerate the proto**

Run: `./dev generate protobuf`
Verify: `pkg/kv/kvpb/api.pb.go` now contains an `EnrichmentID` field on `RequestHeader`.

- [ ] **Step 4: Build**

Run: `./dev build pkg/kv/kvpb`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add pkg/kv/kvpb/api.proto pkg/kv/kvpb/api.pb.go
git commit -m "$(cat <<'EOF'
kvpb: add enrichment_id field to RequestHeader

Adds the new wire field that will eventually replace the
WorkloadID + AppNameID pair for statement-shaped attribution.
This commit is the proto change in isolation; no code reads or
writes the field. Old binaries ignore the unknown field.

The field is typed as executionattributes.ID via the gogoproto
casttype option.

Release note: None
EOF
)"
```

---

### Task 7: Gateway-side stamping (gated by cluster version)

**Files:**
- Create: `pkg/obs/executionattributes/gateway.go`
- Create: `pkg/obs/executionattributes/gateway_test.go`
- Modify: `pkg/sql/instrumentation.go`
- Modify: `pkg/sql/conn_executor_exec.go`
- Modify: `pkg/kv/txn.go`

This commit makes the gateway compute, cache, and stamp `enrichment_id` on outgoing BatchRequests. Behavior is gated on `V26_3_AddExecutionAttributesTable` so it's a no-op in mixed-version clusters until the version finalizes. Legacy fields (`WorkloadID`, `AppNameID`) continue to be set in parallel.

- [ ] **Step 1: Implement the gateway resolver**

Create `pkg/obs/executionattributes/gateway.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

// GatewayResolver computes IDs and triggers durable writes for the
// statement-shaped attribution at the gateway.
type GatewayResolver struct {
	cache  *Cache
	writer *Writer
}

// NewGatewayResolver constructs the resolver bound to a per-node cache
// and writer.
func NewGatewayResolver(cache *Cache, writer *Writer) *GatewayResolver {
	return &GatewayResolver{cache: cache, writer: writer}
}

// Resolve returns the ID for the given attributes, ensuring it is
// cached locally and enqueued for durable write. The returned ID can
// be stamped immediately; the durable write is best-effort.
func (g *GatewayResolver) Resolve(stmtFingerprintID []byte, appName string) ID {
	id := ComputeID(stmtFingerprintID, appName)
	if _, ok := g.cache.Get(id); ok {
		return id
	}
	entry := Entry{StmtFingerprintID: append([]byte(nil), stmtFingerprintID...), AppName: appName}
	g.cache.Put(id, entry)
	g.writer.Enqueue(writeRequest{ID: id, Entry: entry})
	return id
}
```

- [ ] **Step 2: Write a gateway resolver test**

Create `pkg/obs/executionattributes/gateway_test.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"
	"testing"
	"time"
)

func TestGatewayResolverEnqueuesOnce(t *testing.T) {
	exec := &fakeExecutor{}
	metrics := NewMetrics()
	cache := NewCache(8)
	writer := NewWriter(exec, &metrics, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go writer.Run(ctx)

	g := NewGatewayResolver(cache, writer)
	stmtFP := []byte("fp1")

	id1 := g.Resolve(stmtFP, "myapp")
	id2 := g.Resolve(stmtFP, "myapp") // cache hit; no new write
	if id1 != id2 {
		t.Fatalf("expected same ID, got %d and %d", id1, id2)
	}

	// Wait briefly for the writer to drain.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if exec.writeCount() == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected exactly 1 write, got %d", exec.writeCount())
}
```

- [ ] **Step 3: Run the test (should fail then pass)**

Run: `./dev test pkg/obs/executionattributes -f TestGateway -v`
Expected: PASS after Step 1's implementation lands.

- [ ] **Step 4: Cache the ID on `instrumentationHelper`**

In `pkg/sql/instrumentation.go`, find the `instrumentationHelper` struct definition. Add a field alongside `fingerprintId`:

```go
// enrichmentID is the cached ExecutionAttributesID for this
// statement, computed from (fingerprintId, app_name) at first use.
// Zero means "not yet computed" or "version gate not passed".
enrichmentID executionattributes.ID
```

Add the import: `"github.com/cockroachdb/cockroach/pkg/obs/executionattributes"`.

- [ ] **Step 5: Wire the gateway resolver into the connExecutor flow**

In `pkg/sql/conn_executor_exec.go`, find the existing block that calls `ash.GetOrStoreAppNameID(p.SessionData().ApplicationName)` and `p.txn.SetWorkloadInfo(...)` (around line 662–672 per the original survey). Wrap the new logic in a cluster-version check:

```go
appNameID := ash.GetOrStoreAppNameID(p.SessionData().ApplicationName)
p.txn.SetWorkloadInfo(uint64(ih.fingerprintId), appNameID, workloadid.WorkloadTypeStatement)

// New: also stamp enrichment_id when the version gate is open.
if p.execCfg.Settings.Version.IsActive(ctx, clusterversion.V26_3_AddExecutionAttributesTable) {
    if ih.enrichmentID == 0 {
        // Look up the per-node gateway resolver from execCfg (added in Task 9).
        ih.enrichmentID = p.execCfg.ExecutionAttributesResolver.Resolve(
            ih.fingerprintId.Bytes(),
            p.SessionData().ApplicationName,
        )
    }
    p.txn.SetEnrichmentID(ih.enrichmentID)
}
```

(The exact API for fingerprint bytes — `Bytes()` or similar — should be verified against the current `appstatspb.StmtFingerprintID` type.)

- [ ] **Step 6: Add `SetEnrichmentID` on `kv.Txn`**

In `pkg/kv/txn.go`, find `SetWorkloadInfo`. Add a sibling:

```go
// SetEnrichmentID stamps the execution attributes ID that should be
// propagated on every BatchRequest this txn issues.
func (txn *Txn) SetEnrichmentID(id executionattributes.ID) {
    txn.mu.Lock()
    defer txn.mu.Unlock()
    txn.mu.enrichmentID = id
}
```

Add a corresponding `enrichmentID executionattributes.ID` field on the `txn.mu` struct. In the `Send` path (where `BatchRequest.Header` is populated), copy `txn.mu.enrichmentID` into `ba.Header.EnrichmentID` if nonzero.

- [ ] **Step 7: Build to verify everything compiles**

Run: `./dev build pkg/sql pkg/kv`
Expected: clean build. (At this point the `ExecutionAttributesResolver` field on `execCfg` doesn't exist; that's Task 9. For now, you may need to inline the construction or skip Step 5's stamping until Task 9 — see the next note.)

**Note on commit ordering:** Step 5 references `p.execCfg.ExecutionAttributesResolver`, which is wired in Task 9. To keep this commit self-contained, **either** (a) move the connExecutor wiring into Task 9 and have this commit only add the gateway resolver type + the txn.SetEnrichmentID method, OR (b) include the execCfg wiring in this commit. Choose (a) for clean separation; the commit message below assumes option (a).

- [ ] **Step 8: Commit (option (a) — gateway type + txn API only)**

```bash
git add pkg/obs/executionattributes/ pkg/sql/instrumentation.go pkg/kv/txn.go
git commit -m "$(cat <<'EOF'
kv,obs/executionattributes: add gateway resolver and txn API

Introduces GatewayResolver (compute ID, cache, enqueue durable write)
and Txn.SetEnrichmentID (propagates the ID onto every BatchRequest's
RequestHeader.EnrichmentID).

instrumentationHelper gains an enrichmentID field cached alongside
fingerprintId so the hash is computed at most once per statement.

No connExecutor wiring yet; that lands with the executionattributes
service in the SQL server (next commit).

Release note: None
EOF
)"
```

---

### Task 8: Wire `executionattributes` service into the SQL server + activate gateway stamping

**Files:**
- Create: `pkg/obs/executionattributes/server.go`
- Modify: `pkg/server/server_sql.go` (or the equivalent SQL server construction site)
- Modify: `pkg/sql/exec_util.go` (the `ExecutorConfig` definition)
- Modify: `pkg/sql/conn_executor_exec.go` (activate stamping)

This commit constructs the per-node singleton (cache + writer + gateway resolver) and threads it into `ExecutorConfig`, then activates the gateway stamping conditional on the cluster version.

- [ ] **Step 1: Build the server-facing wrapper**

Create `pkg/obs/executionattributes/server.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/cockroachdb/cockroach/pkg/sql/isql"
	"github.com/cockroachdb/cockroach/pkg/util/stop"
)

// Service is the per-node singleton that owns the cache, writer,
// gateway resolver, and (later) sampler-side resolver. Exposed via
// ExecutorConfig.
type Service struct {
	Cache    *Cache
	Writer   *Writer
	Gateway  *GatewayResolver
	Metrics  Metrics
	settings *cluster.Settings
}

// NewService constructs a Service. The caller is responsible for
// calling Start to launch the writer goroutine.
func NewService(settings *cluster.Settings, db isql.DB) *Service {
	metrics := NewMetrics()
	cache := NewCache(int(CacheSize.Get(&settings.SV)))
	exec := newSystemTableExecutor(db)
	writer := NewWriter(exec, &metrics, int(WriteQueueSize.Get(&settings.SV)))
	gateway := NewGatewayResolver(cache, writer)
	return &Service{
		Cache:    cache,
		Writer:   writer,
		Gateway:  gateway,
		Metrics:  metrics,
		settings: settings,
	}
}

// Start launches the writer goroutine under the given stopper.
func (s *Service) Start(ctx context.Context, stopper *stop.Stopper) error {
	return stopper.RunAsyncTask(ctx, "execution-attributes-writer", s.Writer.Run)
}
```

Add the system-table executor implementation in the same file (or a new `pkg/obs/executionattributes/system_table.go`):

```go
// systemTableExecutor wraps an isql.DB to implement the executor
// interface used by Writer.
type systemTableExecutor struct {
	db isql.DB
}

func newSystemTableExecutor(db isql.DB) *systemTableExecutor {
	return &systemTableExecutor{db: db}
}

func (e *systemTableExecutor) insertExecutionAttributes(
	ctx context.Context, req writeRequest,
) (existing Entry, conflict bool, err error) {
	const insertStmt = `
INSERT INTO system.execution_attributes (id, stmt_fingerprint_id, app_name)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO NOTHING
`
	const selectStmt = `
SELECT stmt_fingerprint_id, app_name
FROM system.execution_attributes WHERE id = $1
`
	err = e.db.Txn(ctx, func(ctx context.Context, txn isql.Txn) error {
		if _, err := txn.ExecEx(ctx, "exec-attrs-insert", txn.KV(),
			sessiondata.NodeUserSessionDataOverride,
			insertStmt, int64(req.ID), req.Entry.StmtFingerprintID, req.Entry.AppName); err != nil {
			return err
		}
		row, err := txn.QueryRowEx(ctx, "exec-attrs-verify", txn.KV(),
			sessiondata.NodeUserSessionDataOverride,
			selectStmt, int64(req.ID))
		if err != nil || row == nil {
			return err
		}
		existing.StmtFingerprintID = []byte(*row[0].(*tree.DBytes))
		existing.AppName = string(tree.MustBeDString(row[1]))
		return nil
	})
	if err != nil {
		return Entry{}, false, err
	}
	conflict = !bytes.Equal(existing.StmtFingerprintID, req.Entry.StmtFingerprintID) ||
		existing.AppName != req.Entry.AppName
	return existing, conflict, nil
}
```

(Imports: `"bytes"`, `"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"`, `"github.com/cockroachdb/cockroach/pkg/sql/sessiondata"`. Verify `sessiondata.NodeUserSessionDataOverride` is the correct constant — it might be `sessiondata.RootUserSessionDataOverride` depending on the version.)

- [ ] **Step 2: Add the field to ExecutorConfig**

In `pkg/sql/exec_util.go`, find the `ExecutorConfig` struct. Add:

```go
// ExecutionAttributesService is the per-node service for ash sample
// enrichment. nil before V26_3_AddExecutionAttributesTable.
ExecutionAttributesService *executionattributes.Service
```

Add the import.

- [ ] **Step 3: Construct and start the service in the SQL server**

In `pkg/server/server_sql.go` (or the equivalent — search for where `ExecutorConfig` is populated), construct the service and start it:

```go
execAttrsService := executionattributes.NewService(cfg.Settings, cfg.InternalDB)
if err := execAttrsService.Start(ctx, cfg.Stopper); err != nil {
    return nil, err
}
execCfg.ExecutionAttributesService = execAttrsService
```

Register the metrics:

```go
cfg.registry.AddMetricStruct(execAttrsService.Metrics)
```

- [ ] **Step 4: Activate gateway stamping in connExecutor**

In `pkg/sql/conn_executor_exec.go`, in the same location updated in Task 7, replace the deferred section:

```go
if p.execCfg.Settings.Version.IsActive(ctx, clusterversion.V26_3_AddExecutionAttributesTable) &&
    p.execCfg.ExecutionAttributesService != nil {
    if ih.enrichmentID == 0 {
        ih.enrichmentID = p.execCfg.ExecutionAttributesService.Gateway.Resolve(
            ih.fingerprintId.Bytes(),
            p.SessionData().ApplicationName,
        )
    }
    p.txn.SetEnrichmentID(ih.enrichmentID)
}
```

- [ ] **Step 5: Build and run a smoke test**

Run: `./dev build short`
Then start a single-node cluster and verify a few statements: `./dev start-single-node` (or equivalent), connect via `./dev sql`, run `SELECT 1; SELECT * FROM system.execution_attributes;`. Expect the second query to show one or more rows.

- [ ] **Step 6: Commit**

```bash
git add pkg/obs/executionattributes/ pkg/server/server_sql.go pkg/sql/exec_util.go pkg/sql/conn_executor_exec.go
git commit -m "$(cat <<'EOF'
sql,server: wire executionattributes service and activate gateway stamping

Constructs the per-node executionattributes.Service (cache + writer +
gateway resolver + metrics) at SQL server startup. The Writer goroutine
runs under the SQL stopper.

Gateway stamping in connExecutor is activated under cluster version
V26_3_AddExecutionAttributesTable: every statement computes (or reads
from cache) its enrichment_id and stamps it via Txn.SetEnrichmentID,
which propagates to RequestHeader.EnrichmentID on every BatchRequest.

Legacy WorkloadID + AppNameID stamping continues unchanged for the
duration of this release.

Release note: None
EOF
)"
```

---

### Task 9: Sampler-side resolution and ASHSample columns

**Files:**
- Create: `pkg/obs/executionattributes/resolver.go`
- Create: `pkg/obs/executionattributes/resolver_test.go`
- Modify: `pkg/obs/ash/types.go`
- Modify: `pkg/obs/ash/work_state.go`
- Modify: `pkg/obs/ash/sampler.go`
- Modify: `pkg/server/serverpb/status.proto`
- Modify: `pkg/sql/crdb_internal.go`

This commit makes ASH samples carry and resolve `enrichment_id`. The sampler eagerly denormalizes at sample-write time. Cache misses fall back to a bounded synchronous KV read; on budget exhaustion the sample's denormalized columns are NULL and the ID itself remains as a join key.

- [ ] **Step 1: Implement the SamplerResolver**

Create `pkg/obs/executionattributes/resolver.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"
	"time"
)

// reader is the minimal interface for the sampler to fetch rows from
// system.execution_attributes (real impl uses an isql.DB; tests use a fake).
type reader interface {
	readExecutionAttributes(ctx context.Context, id ID) (Entry, bool, error)
}

// SamplerResolver resolves IDs to Entries at sample-write time.
type SamplerResolver struct {
	cache         *Cache
	reader        reader
	metrics       *Metrics
	readTimeout   func() time.Duration
	tickBudget    func() time.Duration
	tickStartedAt time.Time
	tickSpent     time.Duration
}

// NewSamplerResolver constructs a resolver backed by the given cache and reader.
func NewSamplerResolver(
	cache *Cache, reader reader, metrics *Metrics,
	readTimeout func() time.Duration,
	tickBudget func() time.Duration,
) *SamplerResolver {
	return &SamplerResolver{
		cache:       cache,
		reader:      reader,
		metrics:     metrics,
		readTimeout: readTimeout,
		tickBudget:  tickBudget,
	}
}

// BeginTick resets the per-tick budget tracking.
func (r *SamplerResolver) BeginTick() {
	r.tickStartedAt = time.Now()
	r.tickSpent = 0
}

// Resolve returns the Entry for id, or (Entry{}, false) if not resolvable
// within the tick budget.
func (r *SamplerResolver) Resolve(ctx context.Context, id ID) (Entry, bool) {
	if e, ok := r.cache.Get(id); ok {
		return e, true
	}
	if r.tickSpent >= r.tickBudget() {
		r.metrics.UnresolvedSamples.Inc(1)
		return Entry{}, false
	}
	timeout := r.readTimeout()
	deadline := time.Now().Add(timeout)
	cctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	start := time.Now()
	entry, ok, err := r.reader.readExecutionAttributes(cctx, id)
	r.tickSpent += time.Since(start)
	if err != nil || !ok {
		r.metrics.UnresolvedSamples.Inc(1)
		return Entry{}, false
	}
	r.cache.Put(id, entry)
	return entry, true
}
```

- [ ] **Step 2: Write resolver tests**

Create `pkg/obs/executionattributes/resolver_test.go`:

```go
// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"context"
	"testing"
	"time"
)

type fakeReader struct {
	entries map[ID]Entry
	calls   int
}

func (f *fakeReader) readExecutionAttributes(ctx context.Context, id ID) (Entry, bool, error) {
	f.calls++
	if e, ok := f.entries[id]; ok {
		return e, true, nil
	}
	return Entry{}, false, nil
}

func TestResolverUsesCacheWhenWarm(t *testing.T) {
	cache := NewCache(8)
	cache.Put(42, Entry{AppName: "warm"})
	reader := &fakeReader{}
	metrics := NewMetrics()
	r := NewSamplerResolver(cache, reader, &metrics,
		func() time.Duration { return 50 * time.Millisecond },
		func() time.Duration { return 200 * time.Millisecond })
	r.BeginTick()

	got, ok := r.Resolve(context.Background(), 42)
	if !ok || got.AppName != "warm" {
		t.Fatalf("expected warm, got %+v ok=%v", got, ok)
	}
	if reader.calls != 0 {
		t.Fatalf("expected 0 reader calls on cache hit, got %d", reader.calls)
	}
}

func TestResolverFetchesOnMiss(t *testing.T) {
	cache := NewCache(8)
	reader := &fakeReader{entries: map[ID]Entry{99: {AppName: "fetched"}}}
	metrics := NewMetrics()
	r := NewSamplerResolver(cache, reader, &metrics,
		func() time.Duration { return 50 * time.Millisecond },
		func() time.Duration { return 200 * time.Millisecond })
	r.BeginTick()

	got, ok := r.Resolve(context.Background(), 99)
	if !ok || got.AppName != "fetched" {
		t.Fatalf("expected fetched, got %+v ok=%v", got, ok)
	}
	// Second call hits cache.
	r.Resolve(context.Background(), 99)
	if reader.calls != 1 {
		t.Fatalf("expected 1 reader call across two resolves, got %d", reader.calls)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `./dev test pkg/obs/executionattributes -f TestResolver -v`
Expected: PASS.

- [ ] **Step 4: Add `EnrichmentID` to `WorkState` and `ASHSample`**

In `pkg/obs/ash/types.go`, find the `WorkState` and `ASHSample` struct definitions. Add the new field to each:

```go
// EnrichmentID is the id from RequestHeader.EnrichmentID, set when
// the work was attributed via system.execution_attributes.
EnrichmentID executionattributes.ID
```

Add the import.

- [ ] **Step 5: Propagate `EnrichmentID` in `SetWorkState`**

In `pkg/obs/ash/work_state.go`, find `SetWorkState`. Where it currently extracts `WorkloadID` and `AppNameID` from the batch header (or `WorkloadInfo`), also extract `EnrichmentID`. Pass it through into the `WorkState` it constructs.

- [ ] **Step 6: Wire the SamplerResolver into the Sampler**

In `pkg/obs/ash/sampler.go`, find the per-tick sampling loop (`takeSample` or wherever each `WorkState` is converted into an `ASHSample`). Where the existing code resolves `AppNameID → app_name` (the `appNameMap` lookup or RPC fallback), add the new resolution path:

```go
// At the start of each sampler tick:
s.execAttrsResolver.BeginTick()

// For each sample being constructed:
if ws.EnrichmentID != 0 {
    if entry, ok := s.execAttrsResolver.Resolve(ctx, ws.EnrichmentID); ok {
        sample.StmtFingerprintID = entry.StmtFingerprintID
        sample.AppName = entry.AppName
    } else {
        // Leave StmtFingerprintID nil and AppName empty;
        // unresolved_samples counter already incremented inside Resolve.
    }
    sample.EnrichmentID = ws.EnrichmentID
} else {
    // Legacy path: fall back to existing WorkloadID/AppNameID resolution.
}
```

The resolver and its reader implementation are constructed in the SQL server (Task 8 wiring expanded here):

```go
samplerReader := executionattributes.NewSamplerReader(cfg.InternalDB)
samplerResolver := executionattributes.NewSamplerResolver(
    execAttrsService.Cache, samplerReader, &execAttrsService.Metrics,
    func() time.Duration { return executionattributes.MissReadTimeout.Get(&cfg.Settings.SV) },
    func() time.Duration { return executionattributes.MissTickBudget.Get(&cfg.Settings.SV) },
)
// pass samplerResolver into ash.NewSampler(...)
```

Implement `NewSamplerReader` in `pkg/obs/executionattributes/server.go`:

```go
type samplerReader struct {
	db isql.DB
}

// NewSamplerReader constructs a reader that fetches rows from
// system.execution_attributes via an isql.DB.
func NewSamplerReader(db isql.DB) *samplerReader {
	return &samplerReader{db: db}
}

func (r *samplerReader) readExecutionAttributes(ctx context.Context, id ID) (Entry, bool, error) {
	const stmt = `SELECT stmt_fingerprint_id, app_name FROM system.execution_attributes WHERE id = $1`
	var entry Entry
	var found bool
	err := r.db.Txn(ctx, func(ctx context.Context, txn isql.Txn) error {
		row, err := txn.QueryRowEx(ctx, "exec-attrs-read", txn.KV(),
			sessiondata.NodeUserSessionDataOverride, stmt, int64(id))
		if err != nil || row == nil {
			return err
		}
		entry.StmtFingerprintID = []byte(*row[0].(*tree.DBytes))
		entry.AppName = string(tree.MustBeDString(row[1]))
		found = true
		return nil
	})
	return entry, found, err
}
```

- [ ] **Step 7: Add `enrichment_id` column to the ASH virtual table**

In `pkg/sql/crdb_internal.go`, find the schema for `cluster_active_session_history` (search for `node_active_session_history` or `ASHSample`). Add a column:

```sql
enrichment_id INT8 NULL,
```

Wire it into the row-population code so the column reflects the sample's `EnrichmentID` (or NULL if zero).

- [ ] **Step 8: Add `enrichment_id` to the proto**

In `pkg/server/serverpb/status.proto`, find the `ASHSample` message. Add the field with the next available tag:

```proto
uint64 enrichment_id = N [(gogoproto.casttype) = "github.com/cockroachdb/cockroach/pkg/obs/executionattributes.ID"];
```

Regenerate: `./dev generate protobuf`.

- [ ] **Step 9: Build and smoke test**

Run: `./dev build short`
Start a single-node cluster, run several SQL statements, then:

```sql
SELECT enrichment_id, stmt_fingerprint_id, app_name
FROM crdb_internal.node_active_session_history
WHERE enrichment_id IS NOT NULL
LIMIT 10;
```

Expect rows with non-null enrichment_ids and corresponding fingerprint/app_name values matching `system.execution_attributes`.

- [ ] **Step 10: Commit**

```bash
git add pkg/obs/executionattributes/ pkg/obs/ash/ pkg/sql/crdb_internal.go pkg/server/serverpb/status.proto pkg/server/server_sql.go
git commit -m "$(cat <<'EOF'
obs/ash,obs/executionattributes: resolve enrichment_id at sample time

Threads enrichment_id from the BatchRequest header into WorkState and
ASHSample. Adds SamplerResolver, which resolves IDs at sample-write
time via the per-node cache, falling back to a bounded synchronous KV
read on miss. On budget exhaustion or persistent miss, the sample's
denormalized columns are NULL and the unresolved_samples counter
increments; the enrichment_id itself is preserved on the sample so
joins to system.execution_attributes still work.

The crdb_internal.node_active_session_history virtual table gains
an enrichment_id column.

Release note (sql change): The cluster_active_session_history virtual
table now exposes an enrichment_id column for joining against
system.execution_attributes.
EOF
)"
```

---

### Task 10: Sustained-discard log line

**Files:**
- Modify: `pkg/obs/executionattributes/writer.go`
- Modify: `pkg/obs/executionattributes/cache.go`

Final commit: rate-limited operator-facing logging when discards happen.

- [ ] **Step 1: Add a rate-limited logger to Writer**

In `pkg/obs/executionattributes/writer.go`, replace the `Enqueue` method:

```go
import (
    "github.com/cockroachdb/cockroach/pkg/util/log"
)

var discardLogEvery = log.Every(time.Minute)

// Enqueue adds a write request, dropping if the queue is full.
func (w *Writer) Enqueue(req writeRequest) {
    select {
    case w.queue <- req:
    default:
        w.metrics.Discarded.Inc(1)
        if discardLogEvery.ShouldLog() {
            log.Warningf(context.Background(),
                "execution_attributes write queue full; dropping entry id=%d. "+
                "Consider raising obs.execution_attributes.write_queue_size.",
                req.ID)
        }
    }
}
```

- [ ] **Step 2: Build and smoke test**

Run: `./dev build pkg/obs/executionattributes`
Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add pkg/obs/executionattributes/
git commit -m "$(cat <<'EOF'
obs/executionattributes: log on sustained write-queue discards

Adds a rate-limited (once per minute per node) WARNING when the
durable write queue overflows. The log line names the cluster
setting operators can tune (obs.execution_attributes.write_queue_size).

Release note: None
EOF
)"
```

---

## Out of scope for this POC plan

- **Phase 2 of the migration** (removing `app_name_id` field, deleting `appNameMap` and `Sampler.resolveRemoteAppNames`): lives in a future release after `V26_3_AddExecutionAttributesTable` is finalized.
- **Comprehensive mixed-version tests**: only basic happy-path validation in this POC.
- **Cross-tenant, multi-region considerations**: deferred.
- **TTL on `system.execution_attributes` rows**: not needed at observed cardinality; can be added later if a cluster's cache_size needs raising past a threshold.
- **Tracing integration** (joining ash samples to spans via enrichment_id): out of scope.
- **sqlstats integration** (using the same enrichment_id from sqlstats): a separate downstream project.

## References

- [Design doc](../specs/2026-05-05-ash-enrichment-design.md)
- [Cardinality investigation](../specs/2026-05-05-ash-enrichment-cardinality-investigation.md)
