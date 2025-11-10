// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package fingerprint

import (
	"context"
	"sync"

	"github.com/cockroachdb/cockroach/pkg/sql/appstatspb"
	"github.com/cockroachdb/cockroach/pkg/sql/isql"
)

// Store encapsulates the both the caching and persistence of Fingerprints. It
// theoretically maintains a cache of digests, and if an unseen digest is
// encountered, it attempts an upsert against the normalized fingerprints
// table.
//
// As the primary usage of normalized fingerprints is to join them against
// other record types in sql queries, there isn't a need to retrieve records
// from the store.
func NewStore(db isql.DB) *Store {
	s := &Store{
		db: db,
	}
	s.mu.seen = make(map[appstatspb.StmtFingerprintID]struct{})
	return s
}

type Store struct {
	mu struct {
		sync.Mutex
		seen map[appstatspb.StmtFingerprintID]struct{}
	}
	db isql.DB
}

func (s *Store) Get(
	ctx context.Context, dbName string, query string, implicitTxn bool,
) (appstatspb.StmtFingerprintID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	digest := appstatspb.ConstructStatementFingerprintID(query, implicitTxn, dbName)
	return digest, nil
}
