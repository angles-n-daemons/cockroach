// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package vtable

// IndexUsageStatistics describes the schema of the internal index_usage_statistics table.
const CRDBIndexUsageStatistics = `
CREATE TABLE information_schema.crdb_index_usage_statistics (
  table_id        INT NOT NULL,
  index_id        INT NOT NULL,
  total_reads     INT NOT NULL,
  last_read       TIMESTAMPTZ
)`

// WorkSample describes the schema of the work_sample view which
// joins system.work_span with statement statistics to show query context.
// The view filters to show only data from between 20 and 10 seconds ago
// to ensure statement statistics are available and to limit data volume.
const WorkSample = `
CREATE VIEW information_schema.work_sample AS
SELECT
  ws.id,
  ws.parent_id,
  ws.node_id,
  ws.statement_fingerprint_id,
  lpad(to_hex(ws.statement_fingerprint_id), 16, '0') AS statement_id,
  ws.ts,
  ws.duration,
  ws.cpu_time,
  ws.component,
  ws.component_metrics,
  ws.component_attributes,
  ws.query_tags,
  ss.metadata->>'query' AS query
FROM
  system.work_span ws
LEFT JOIN
  crdb_internal.statement_statistics ss
ON
  encode(ss.fingerprint_id, 'hex') = lpad(to_hex(ws.statement_fingerprint_id), 16, '0')
WHERE
  ws.ts >= now() - INTERVAL '20 seconds'
  AND ws.ts < now() - INTERVAL '10 seconds'
`
