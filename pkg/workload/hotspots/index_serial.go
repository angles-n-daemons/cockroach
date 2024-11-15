// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package hotspots

import (
	"context"
	gosql "database/sql"
	"fmt"
	"math/rand"
	"strings"

	"github.com/cockroachdb/cockroach/pkg/util/timeutil"
	"github.com/cockroachdb/cockroach/pkg/workload"
	"github.com/cockroachdb/cockroach/pkg/workload/histogram"
	"github.com/cockroachdb/errors"
	"github.com/spf13/pflag"
)

var RandomSeed = workload.NewInt64RandomSeed()

type indexSerial struct {
	flags     workload.Flags
	connFlags *workload.ConnFlags

	idxs        int
	unique      bool
	payload     int
	cycleLength uint64
	workload    string
}

func init() {
	fmt.Println("hotspots loaded")
	workload.Register(indexesMeta)
}

var indexesMeta = workload.Meta{
	Name:        `hotspots__index_serial`,
	Description: `Simulate an index hotspot by inserting into a table with a serial primary key.`,
	Version:     `1.0.0`,
	RandomSeed:  RandomSeed,
	New: func() workload.Generator {
		g := &indexSerial{}
		g.flags.FlagSet = pflag.NewFlagSet(`hotspots`, pflag.ContinueOnError)
		RandomSeed.AddFlag(&g.flags)
		g.connFlags = workload.NewConnFlags(&g.flags)
		return g
	},
}

var _ workload.Generator = &indexSerial{}
var _ workload.Opser = &indexSerial{}
var _ workload.Hookser = &indexSerial{}

// Meta implements the Generator interface.
func (*indexSerial) Meta() workload.Meta { return indexesMeta }

// Flags implements the Flagser interface.
func (w *indexSerial) Flags() workload.Flags { return w.flags }

// ConnFlags implements the ConnFlagser interface.
func (w *indexSerial) ConnFlags() *workload.ConnFlags { return w.connFlags }

// Hooks implements the Hookser interface.
func (w *indexSerial) Hooks() workload.Hooks {
	return workload.Hooks{
		Validate: func() error {
			return nil
		},
	}
}

// Tables implements the Generator interface.
func (w *indexSerial) Tables() []workload.Table {

	return []workload.Table{{
		Name:   `users`,
		Schema: `(id SERIAL PRIMARY KEY, email VARCHAR)`,
	}}
}

// Ops implements the Opser interface.
func (w *indexSerial) Ops(
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
