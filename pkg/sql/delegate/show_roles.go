// Copyright 2019 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package delegate

import (
	"github.com/cockroachdb/cockroach/pkg/clusterversion"
	"github.com/cockroachdb/cockroach/pkg/sql/pgwire/pgnotice"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/sqltelemetry"
)

// delegateShowRoles implements SHOW ROLES which returns all the roles.
// Privileges: SELECT on system.users.
func (d *delegator) delegateShowRoles() (tree.Statement, error) {
	sqltelemetry.IncrementShowCounter(sqltelemetry.Roles)
	selectClause := `
SELECT
	u.username,
	COALESCE(array_remove(array_agg(o.option || COALESCE('=' || o.value, '') ORDER BY o.option), NULL), ARRAY[]::STRING[]) AS options,
	ARRAY (SELECT role FROM system.role_members AS rm WHERE rm.member = u.username ORDER BY 1) AS member_of`
	selectLastLoginTime := `,
	u.estimated_last_login_time`
	endingClauses := `
FROM
	system.users AS u LEFT JOIN system.role_options AS o ON u.username = o.username
GROUP BY
	u.username
ORDER BY 1;
`
	if d.evalCtx.Settings.Version.IsActive(d.ctx, clusterversion.V25_3) {
		d.evalCtx.ClientNoticeSender.BufferClientNotice(d.ctx, pgnotice.Newf(
			"estimated_last_login_time is computed on a best effort basis; it is not guaranteed to capture every login event"))
		return d.parse(selectClause + selectLastLoginTime + endingClauses)
	}
	return d.parse(selectClause + endingClauses)
}
