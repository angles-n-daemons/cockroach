// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package fingerprint

type Fingerprint struct {
	// id       uint
	digest   []byte
	db       string
	implicit bool
	query    string
	summary  string
}

// Store encapsulates the both the caching and persistence of Fingerprints. It
// theoretically maintains a cache of digests, and if an unseen digest is
// encountered, it attempts an upsert against the normalized fingerprints
// table.
//
// As the primary usage of normalized fingerprints is to join them against
// other record types in sql queries, there isn't a need to retrieve records
// from the store.
type Store interface {
	Put(fp Fingerprint) error
}
