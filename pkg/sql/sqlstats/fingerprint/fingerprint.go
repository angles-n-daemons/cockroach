// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package fingerprint

import (
	"context"
	"encoding/binary"

	"github.com/cockroachdb/cockroach/pkg/sql/appstatspb"
	"github.com/cockroachdb/cockroach/pkg/sql/isql"
	"github.com/cockroachdb/cockroach/pkg/util/syncutil"
)

// Maximum possible number of cached fingerprints is 1 million.
const MAX_CACHED_FINGERPRINTS = 1000000

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
	return s
}

type Store struct {
	seen syncutil.Map[appstatspb.StmtFingerprintID, struct{}]
	db   isql.DB
}

// Get both returns the fingerprint of the incoming fields, and persists
// it to the underlying system table if it's unsen.
func (s *Store) Get(
	ctx context.Context, dbName string, query string, implicitTxn bool,
) (appstatspb.StmtFingerprintID, error) {
	fp := appstatspb.ConstructStatementFingerprintID(query, implicitTxn, dbName)
	cacheFull := s.seen.Len() > MAX_CACHED_FINGERPRINTS
	if _, ok := s.seen.Load(fp); cacheFull || ok {
		return fp, nil
	}

	s.seen.Store(fp, &struct{}{})
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(fp))
	err := s.db.Txn(ctx, func(ctx context.Context, txn isql.Txn) error {
		_, err := txn.Exec(ctx, "fingerprint-upsert", txn.KV(), `
				INSERT INTO system.statement_fingerprints
			        (row_id, fingerprint, database, query, implicit_txn, summary)
				VALUES (nextval('system.statement_fingerprint_id_seq'), $1, $2, $3, $4, '')
			  ON CONFLICT(fingerprint) DO NOTHING
			`, b, dbName, query, implicitTxn)
		return err
	})
	return fp, err
}
