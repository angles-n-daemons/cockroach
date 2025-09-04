// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package unsafesql_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/security/securityassets"
	"github.com/cockroachdb/cockroach/pkg/security/securitytest"
	"github.com/cockroachdb/cockroach/pkg/server"
	"github.com/cockroachdb/cockroach/pkg/sql/isql"
	"github.com/cockroachdb/cockroach/pkg/sql/sqlerrors"
	"github.com/cockroachdb/cockroach/pkg/testutils/serverutils"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/cockroachdb/cockroach/pkg/util/randutil"
	"github.com/cockroachdb/errors"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	securityassets.SetLoader(securitytest.EmbeddedAssets)
	randutil.SeedForTests()
	serverutils.InitTestServerFactory(server.TestServerFactory)
	os.Exit(m.Run())
}

func checkUnsafeErr(t *testing.T, err error) {
	var pqErr *pq.Error

	if errors.Is(err, sqlerrors.ErrUnsafeTableAccess) {
		return
	}

	if errors.As(err, &pqErr) &&
		pqErr.Code == "42501" &&
		strings.Contains(pqErr.Message, "Access to crdb_internal and system is restricted") {
		return
	}

	t.Fatal("expected unsafe access error, got", err)
}

func TestAccessCheckServer(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	ctx := context.Background()
	s := serverutils.StartServerOnly(t, base.TestServerArgs{
		DefaultTestTenant: base.TestControlsTenantsExplicitly,
	})
	defer s.Stopper().Stop(ctx)

	pool := s.SQLConn(t)
	defer pool.Close()

	_, err := pool.Exec("CREATE TABLE foo (id INT PRIMARY KEY)")
	require.NoError(t, err)

	sendQuery := func(allowUnsafe bool, internal bool, query string) error {
		if internal {
			idb := s.InternalDB().(isql.DB)
			err := idb.Txn(ctx, func(ctx context.Context, txn isql.Txn) error {
				txn.SessionData().LocalOnlySessionData.AllowUnsafeInternals = allowUnsafe

				_, err := txn.QueryBuffered(ctx, "internal-query", txn.KV(), query)
				return err
			})
			if err != nil {
				return err
			}
		} else {
			conn, err := pool.Conn(ctx)
			if err != nil {
				return err
			}
			_, err = conn.ExecContext(ctx, "SET allow_unsafe_internals = $1", allowUnsafe)
			if err != nil {
				return err
			}
			_, err = conn.QueryContext(ctx, query)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, test := range []struct {
		Query                string
		Internal             bool
		AllowUnsafeInternals bool
		Passes               bool
	}{
		// Regular tables aren't considered unsafe.
		{
			Query:  "SELECT * FROM foo",
			Passes: true,
		},
		// Tests on the system objects.
		{
			Query:                "SELECT * FROM system.namespace",
			Internal:             true,
			AllowUnsafeInternals: false,
			Passes:               true,
		},
		{
			Query:                "SELECT * FROM system.namespace",
			Internal:             true,
			AllowUnsafeInternals: true,
			Passes:               true,
		},
		{
			Query:                "SELECT * FROM system.namespace",
			Internal:             false,
			AllowUnsafeInternals: false,
			Passes:               false,
		},
		{
			Query:                "SELECT * FROM system.namespace",
			Internal:             false,
			AllowUnsafeInternals: true,
			Passes:               true,
		},
		// Tests on unsupported crdb_internal objects.
		{
			Query:                "SELECT * FROM crdb_internal.gossip_alerts",
			AllowUnsafeInternals: false,
			Passes:               false,
		},
		{
			Query:                "SELECT * FROM crdb_internal.gossip_alerts",
			AllowUnsafeInternals: true,
			Passes:               true,
		},
		// Tests on supported crdb_internal objects.
		{
			Query:                "SELECT * FROM crdb_internal.zones",
			AllowUnsafeInternals: false,
			Passes:               true,
		},
		{
			Query:                "SELECT * FROM crdb_internal.zones",
			AllowUnsafeInternals: true,
			Passes:               true,
		},
		// Non-crdb_internal functions pass
		{
			Query:  "SELECT * FROM generate_series(1, 5)",
			Passes: true,
		},
		// Crdb_internal functions require the override.
		{
			Query:  "SELECT * FROM crdb_internal.tenant_span_stats()",
			Passes: false,
		},
		{
			Query:                "SELECT * FROM crdb_internal.tenant_span_stats()",
			AllowUnsafeInternals: true,
			Passes:               true,
		},
		// Tests on delegate behavior.
		{
			Query:  "SHOW GRANTS",
			Passes: true,
		},
		{
			// this query is what show grants is using under the hood.
			Query:  "SELECT * FROM crdb_internal.privilege_name('SELECT')",
			Passes: false,
		},
		{
			Query:  "SHOW DATABASES",
			Passes: true,
		},
		{
			// this query is what show databases is using under the hood.
			Query:  "SELECT * FROM crdb_internal.databases",
			Passes: false,
		},
	} {
		t.Run(fmt.Sprintf("query=%s,internal=%t,allowUnsafe=%t", test.Query, test.Internal, test.AllowUnsafeInternals), func(t *testing.T) {
			err := sendQuery(test.AllowUnsafeInternals, test.Internal, test.Query)
			if test.Passes {
				require.NoError(t, err)
			} else {
				checkUnsafeErr(t, err)
			}
		})
	}
}

func TestAnnotationMismatchPanic(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	ctx := context.Background()
	s := serverutils.StartServerOnly(t, base.TestServerArgs{
		DefaultTestTenant: base.TestControlsTenantsExplicitly,
	})
	defer s.Stopper().Stop(ctx)

	pool := s.SQLConn(t)
	defer pool.Close()

	// Create test database
	_, err := pool.Exec("CREATE DATABASE test")
	require.NoError(t, err)

	_, err = pool.Exec("USE test")
	require.NoError(t, err)

	// Create a view that uses crdb_internal functions
	_, err = pool.Exec(`
		CREATE VIEW v_col_fn_ids AS 
		SELECT id, 
		       json_array_elements((crdb_internal.pb_to_json('cockroach.sql.sqlbase.Descriptor', descriptor, false)->'table'->'columns')->'usesFunctionIds') as uses_fn_ids
		FROM system.descriptor
	`)
	require.NoError(t, err)

	// This will panic with "index out of range [1] with length 1"
	// due to annotation mismatch between CREATE FUNCTION statement and function body
	_, err = pool.Exec(`
		CREATE FUNCTION get_col_fn_ids(table_id INT) RETURNS SETOF v_col_fn_ids
		LANGUAGE SQL
		AS $$
			SELECT * FROM v_col_fn_ids WHERE id = table_id;
		$$
	`)
	require.NoError(t, err)
}
