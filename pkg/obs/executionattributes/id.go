// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"encoding/binary"

	"github.com/cespare/xxhash/v2"
)

// ID is the typed identifier for an execution-shaped attribution context.
// Computed as xxhash64 of the canonical encoding of
// (stmt_fingerprint_id, app_name). The zero value is reserved as a
// sentinel for "unattributed" or "resolution failed"; ComputeID never
// returns 0.
type ID uint64

// ComputeID returns the deterministic ID for the given attribute set.
// Same inputs always yield the same ID, regardless of node or time.
//
// The canonical encoding is length-prefixed concatenation:
//
//	uint64(len(stmtFingerprintID)) || stmtFingerprintID ||
//	uint64(len(appName))           || appName
//
// Length prefixes prevent (a, b) || (c) from colliding with (a) || (b, c).
func ComputeID(stmtFingerprintID []byte, appName string) ID {
	h := xxhash.New()
	var lenBuf [8]byte
	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(stmtFingerprintID)))
	_, _ = h.Write(lenBuf[:])
	_, _ = h.Write(stmtFingerprintID)
	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(appName)))
	_, _ = h.Write(lenBuf[:])
	_, _ = h.WriteString(appName)
	id := ID(h.Sum64())
	if id == 0 {
		// Avoid the sentinel by perturbing; collision risk is negligible.
		id = ID(1)
	}
	return id
}
