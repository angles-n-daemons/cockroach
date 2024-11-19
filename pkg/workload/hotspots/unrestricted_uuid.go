// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package hotspots

import (
	"context"
	gosql "database/sql"
	"math/rand"
	"strings"

	"github.com/cockroachdb/cockroach/pkg/util/timeutil"
	"github.com/cockroachdb/cockroach/pkg/workload"
	"github.com/cockroachdb/cockroach/pkg/workload/histogram"
	"github.com/cockroachdb/errors"
	"github.com/spf13/pflag"
)

type unrestrictedUUID struct {
	flags     workload.Flags
	connFlags *workload.ConnFlags

	idxs        int
	unique      bool
	payload     int
	cycleLength uint64
	workload    string
}

func init() {
	workload.Register(unrestrictedUUIDMeta)
}

var unrestrictedUUIDMeta = workload.Meta{
	Name:        `hotspots__unrestricted_uuid`,
	Description: `Simulate workload similar to hotspot workloads for comparison`,
	Version:     `1.0.0`,
	RandomSeed:  RandomSeed,
	New: func() workload.Generator {
		g := &unrestrictedUUID{}
		g.flags.FlagSet = pflag.NewFlagSet(`hotspots`, pflag.ContinueOnError)
		RandomSeed.AddFlag(&g.flags)
		g.connFlags = workload.NewConnFlags(&g.flags)
		return g
	},
}

var _ workload.Generator = &unrestrictedUUID{}
var _ workload.Opser = &unrestrictedUUID{}
var _ workload.Hookser = &unrestrictedUUID{}

// Meta implements the Generator interface.
func (*unrestrictedUUID) Meta() workload.Meta { return unrestrictedUUIDMeta }

// Flags implements the Flagser interface.
func (w *unrestrictedUUID) Flags() workload.Flags { return w.flags }

// ConnFlags implements the ConnFlagser interface.
func (w *unrestrictedUUID) ConnFlags() *workload.ConnFlags { return w.connFlags }

// Hooks implements the Hookser interface.
func (w *unrestrictedUUID) Hooks() workload.Hooks {
	return workload.Hooks{
		Validate: func() error {
			return nil
		},
	}
}

// Tables implements the Generator interface.
func (w *unrestrictedUUID) Tables() []workload.Table {
	return []workload.Table{{
		Name:   `users`,
		Schema: `(id UUID PRIMARY KEY DEFAULT GEN_RANDOM_UUID(), email VARCHAR)`,
	}}
}

// Ops implements the Opser interface.
func (w *unrestrictedUUID) Ops(
	ctx context.Context, urls []string, reg *histogram.Registry,
) (workload.QueryLoad, error) {
	db, err := gosql.Open(`cockroach`, strings.Join(urls, ` `))
	if err != nil {
		return workload.QueryLoad{}, err
	}
	// Allow a maximum of concurrency+1 connections to the database.
	db.SetMaxOpenConns(w.connFlags.Concurrency + 1)
	db.SetMaxIdleConns(w.connFlags.Concurrency + 1)

	ql := workload.QueryLoad{
		Close: func(_ context.Context) error {
			return db.Close()
		},
	}
	for i := 0; i < w.connFlags.Concurrency; i++ {
		rng := rand.New(rand.NewSource(RandomSeed.Seed()))
		hists := reg.GetHandle()
		workerFn := func(ctx context.Context) error {
			start := timeutil.Now()

			query := `INSERT INTO users (email) VALUES ($1)`
			updateStmt, err := db.Prepare(query)
			if err != nil {
				return errors.CombineErrors(err, db.Close())
			}

			_, err = updateStmt.Exec(randSeq(rng, 256))
			elapsed := timeutil.Since(start)
			hists.Get(`transfer`).Record(elapsed)
			return err
		}
		ql.WorkerFns = append(ql.WorkerFns, workerFn)
	}
	return ql, nil
}
