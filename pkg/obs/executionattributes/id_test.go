// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"encoding/binary"
	"testing"
)

func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

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

func TestComputeIDLengthPrefixedPreventsCollision(t *testing.T) {
	// (a, b) and (a||b, "") should NOT produce the same ID; without length
	// prefixing they would (concatenation is ambiguous).
	id1 := ComputeID([]byte("foo"), "bar")
	id2 := ComputeID([]byte("foobar"), "")
	if id1 == id2 {
		t.Fatalf("length-prefixing failed: (foo, bar) collided with (foobar, '')")
	}
}
