// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package sqlstats

import (
	"context"
	gosql "database/sql"
	"strings"

	"github.com/cockroachdb/cockroach/pkg/util/syncutil"
	"github.com/cockroachdb/cockroach/pkg/util/timeutil"
	"github.com/cockroachdb/cockroach/pkg/workload"
	"github.com/cockroachdb/cockroach/pkg/workload/histogram"
	"github.com/cockroachdb/errors"
	"github.com/spf13/pflag"
)

const (
	dummyTable = `(
		id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
		username VARCHAR(255),
	  passwordhash BYTEA
	)`
	tableName     = "sqli_workload"
	defaultDbName = "sqli"
)

var RandomSeed = workload.NewUint64RandomSeed()

type sqlI struct {
	flags     workload.Flags
	connFlags *workload.ConnFlags
}

var _ workload.Generator = &sqlI{}
var _ workload.Opser = &sqlI{}
var _ workload.Hookser = &sqlI{}

func init() {
	workload.Register(sqlIMeta)
}

var sqlIMeta = workload.Meta{
	Name:          "sqli",
	Description:   "sqli generates a workload with fingerprints which have both low and high cardinality literals",
	RandomSeed:    RandomSeed,
	Version:       "1.0.0",
	TestInfraOnly: true,
	New: func() workload.Generator {
		g := &sqlI{}
		g.flags.FlagSet = pflag.NewFlagSet(`sqli`, pflag.ContinueOnError)
		RandomSeed.AddFlag(&g.flags)
		g.connFlags = workload.NewConnFlags(&g.flags)
		return g
	},
}

// Meta implements the Generator interface.
func (s *sqlI) Meta() workload.Meta { return sqlIMeta }

// Flags implements the Flagser interface.
func (s *sqlI) Flags() workload.Flags { return s.flags }

// ConnFlags implements the ConnFlagser interface.
func (s *sqlI) ConnFlags() *workload.ConnFlags { return s.connFlags }

func (s *sqlI) Tables() []workload.Table {
	return []workload.Table{{
		Name:   tableName,
		Schema: dummyTable,
		Splits: workload.BatchedTuples{},
	}}
}

func (s *sqlI) Hooks() workload.Hooks {
	return workload.Hooks{
		Validate: func() error {
			return nil
		},
	}
}

type gen struct {
	syncutil.Mutex
}

func (g *gen) Next() string {
	g.Lock()
	defer g.Unlock()

	return `
		SELECT * FROM users
		WHERE username=$1
	`
}

func (s *sqlI) Ops(
	ctx context.Context, urls []string, reg *histogram.Registry,
) (workload.QueryLoad, error) {
	db, err := gosql.Open(`cockroach`, strings.Join(urls, ` `))
	if err != nil {
		return workload.QueryLoad{}, err
	}
	// Allow a maximum of concurrency+1 connections to the database.
	db.SetMaxOpenConns(s.connFlags.Concurrency + 1)
	db.SetMaxIdleConns(s.connFlags.Concurrency + 1)

	gen := &gen{}
	ql := workload.QueryLoad{
		Close: func(_ context.Context) error {
			return db.Close()
		},
	}
	for i := 0; i < s.connFlags.Concurrency; i++ {
		hists := reg.GetHandle()
		workerFn := func(ctx context.Context) error {
			start := timeutil.Now()

			query := gen.Next()
			updateStmt, err := db.Prepare(query)
			if err != nil {
				return errors.CombineErrors(err, db.Close())
			}

			_, err = updateStmt.Exec("johnson")
			elapsed := timeutil.Since(start)
			hists.Get(`transfer`).Record(elapsed)
			return err
		}
		ql.WorkerFns = append(ql.WorkerFns, workerFn)
	}
	return ql, nil
}
