// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package tests

import (
	"context"
	"math/rand"
	"time"

	"github.com/cockroachdb/cockroach/pkg/cmd/roachtest/cluster"
	"github.com/cockroachdb/cockroach/pkg/cmd/roachtest/registry"
	"github.com/cockroachdb/cockroach/pkg/cmd/roachtest/roachtestutil/mixedversion"
	"github.com/cockroachdb/cockroach/pkg/cmd/roachtest/spec"
	"github.com/cockroachdb/cockroach/pkg/cmd/roachtest/test"
	"github.com/cockroachdb/cockroach/pkg/roachprod/logger"
)

func registerStatusServerMixedVersion(r registry.Registry) {
	// sql-tats/mixed-version tests that requesting sql stats from admin-ui works across
	// mixed version clusters.
	r.Add(registry.TestSpec{
		Name:             "status-server/mixed-version",
		Owner:            registry.OwnerObservability,
		Cluster:          r.MakeClusterSpec(5, spec.WorkloadNode()),
		CompatibleClouds: registry.AllClouds,
		Suites:           registry.Suites(registry.MixedVersion, registry.Nightly),
		Randomized:       true,
		Run:              runStatusServerMixedVersion,
		Timeout:          1 * time.Hour,
	})
}

func runStatusServerMixedVersion(ctx context.Context, t test.Test, c cluster.Cluster) {
	mvt := mixedversion.NewTest(ctx, t, t.L(), c,
		c.CRDBNodes(),
		mixedversion.MinimumSupportedVersion("v24.3.0"),
	)

	mvt.InMixedVersion("status server mixed versions", func(ctx context.Context, l *logger.Logger, rng *rand.Rand, h *mixedversion.Helper) error {
		runStatusServerTests(ctx, t, c)
		return nil
	})
	mvt.Run()
}
