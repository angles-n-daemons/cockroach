// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package unsafesql

import (
	"context"
	"fmt"

	"github.com/cockroachdb/cockroach/pkg/settings"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/sessiondata"
	"github.com/cockroachdb/cockroach/pkg/sql/sqlerrors"
)

// CheckInternalsAccess checks if the current session has permission to access
// unsafe internal tables and functionality. This includes system tables and
// virtual tables / builtins in the crdb_internal schema.
func CheckInternalsAccess(
	ctx context.Context,
	sd *sessiondata.SessionData,
	stmt tree.Statement,
	ann *tree.Annotations,
	sv *settings.Values,
) error {
	// If the querier is internal, we should allow it.
	if sd.Internal {
		return nil
	}

	q := tree.FormatAstAsRedactableString(stmt, ann, sv)
	fmt.Printf("Here is my query %s", q)
	// If an override is set, allow access to this virtual table.
	if sd.AllowUnsafeInternals {
		return nil
	}

	return sqlerrors.ErrUnsafeTableAccess
}
