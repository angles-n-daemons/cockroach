// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package hotspots

import (
	"context"
	gosql "database/sql"
	"math"
	"math/rand"
	"strings"

	"github.com/cockroachdb/cockroach/pkg/util/timeutil"
	"github.com/cockroachdb/cockroach/pkg/workload"
	"github.com/cockroachdb/cockroach/pkg/workload/histogram"
	"github.com/cockroachdb/errors"
	"github.com/spf13/pflag"
)

type indexRandomINT struct {
	flags     workload.Flags
	connFlags *workload.ConnFlags

	ranges int
}

func init() {
	workload.Register(indexRandomINTMeta)
}

var indexRandomINTMeta = workload.Meta{
	Name:        `hotspots__random_int`,
	Description: `Simulate workload similar to hotspot workloads for comparison`,
	Version:     `1.0.0`,
	RandomSeed:  RandomSeed,
	New: func() workload.Generator {
		g := &indexRandomINT{}
		g.flags.FlagSet = pflag.NewFlagSet(`hotspots`, pflag.ContinueOnError)
		g.flags.IntVar(&g.ranges, `num-ranges`, 16, `Initial number of ranges to break the tables into`)
		RandomSeed.AddFlag(&g.flags)
		g.connFlags = workload.NewConnFlags(&g.flags)
		return g
	},
}

var _ workload.Generator = &indexRandomINT{}
var _ workload.Opser = &indexRandomINT{}
var _ workload.Hookser = &indexRandomINT{}

// Meta implements the Generator interface.
func (*indexRandomINT) Meta() workload.Meta { return indexRandomINTMeta }

// Flags implements the Flagser interface.
func (w *indexRandomINT) Flags() workload.Flags { return w.flags }

// ConnFlags implements the ConnFlagser interface.
func (w *indexRandomINT) ConnFlags() *workload.ConnFlags { return w.connFlags }

// Hooks implements the Hookser interface.
func (w *indexRandomINT) Hooks() workload.Hooks {
	return workload.Hooks{
		Validate: func() error {
			return nil
		},
	}
}

// Tables implements the Generator interface.
func (w *indexRandomINT) Tables() []workload.Table {
	return []workload.Table{{
		Name:   `users`,
		Schema: `(id BIGINT PRIMARY KEY, email VARCHAR)`,
		Splits: workload.Tuples(w.ranges, func(i int) []interface{} {
			offset := float64(i) / float64(w.ranges)
			return []interface{}{offset * 9223372036854775807}
		}),
	}}
}

// Ops implements the Opser interface.
func (w *indexRandomINT) Ops(
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

			query := `INSERT INTO users (id, email) VALUES ($1, $2)`
			updateStmt, err := db.Prepare(query)
			if err != nil {
				return errors.CombineErrors(err, db.Close())
			}

			_, err = updateStmt.Exec(rand.Float64()*math.MaxInt64, randSeq(rng, 256))
			elapsed := timeutil.Since(start)
			hists.Get(`transfer`).Record(elapsed)
			return err
		}
		ql.WorkerFns = append(ql.WorkerFns, workerFn)
	}
	return ql, nil
}
