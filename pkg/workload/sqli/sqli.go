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
	"fmt"
	"strings"

	"github.com/cockroachdb/cockroach/pkg/util/timeutil"
	"github.com/cockroachdb/cockroach/pkg/workload"
	"github.com/cockroachdb/cockroach/pkg/workload/histogram"
	"github.com/cockroachdb/errors"
	"github.com/spf13/pflag"
	"golang.org/x/exp/rand"
)

var RandomSeed = workload.NewUint64RandomSeed()

var rng = rand.New(rand.NewSource(RandomSeed.Seed()))
var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rng.Intn(len(letterRunes))]
	}
	return string(b)
}

const (
	dummyUserTable = `(
		id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
		username VARCHAR(255),
	  passwordhash BYTEA
	)`
	userTableName = "sqli_users"

	dummyCommentTable = `(
		id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
		text TEXT
	)`
	commentTableName = "sqli_comments"

	dummyPaymentTable = `(
		id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
		amount NUMERIC,
	  status VARCHAR(255)
	)`
	paymentTableName = "sqli_payments"

	defaultDbName = "sqli"
)

/*
 * The sqlI workload is meant to simulate web application traffic using both
 * safe and unsafe access patterns. It sends along prepared statements in addition
 * to low and high cardinality string literals, so that the server can can
 * idenitfy unsafe usage.
 */
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
	return []workload.Table{
		{
			Name:   userTableName,
			Schema: dummyUserTable,
			Splits: workload.BatchedTuples{},
		}, {
			Name:   commentTableName,
			Schema: dummyCommentTable,
			Splits: workload.BatchedTuples{},
		}, {
			Name:   paymentTableName,
			Schema: dummyPaymentTable,
			Splits: workload.BatchedTuples{},
		},
	}
}

func (s *sqlI) Hooks() workload.Hooks {
	return workload.Hooks{
		Validate: func() error {
			return nil
		},
	}
}

type query interface {
	Query() string
	Args() []any
}

// query1s characteristics are what we are looking for in terms of injection
// vulnerabilities. its a sql statement with string literals which have a high
// degree of cardinality.
type query1 struct{}

func (q query1) Query() string {
	return fmt.Sprintf(`
		INSERT INTO sqli_users(username)
		VALUES ('%s')
	`, randString(10))
}
func (q query1) Args() []any { return []any{} }

// query2s characteristics are similar to query1s, but with arguments passed
// instead of string literals. because of this, it should not fire warnings
type query2 struct{}

func (q query2) Query() string {
	return "INSERT INTO sqli_comments (text) VALUES ($1)"
}
func (q query2) Args() []any {
	return []any{randString(10)}
}

// query3s characteristics are slightly different. query3 will pass a string
// literal, but with a low cardinality. this should simulate usage of a query
// which uses a string enum, and is unlikely to be an entry point for sqli
// something to note is that this exposes how if a numeric (or other literal)
// is passed, it will escape the injection scanner
type query3 struct{}

var paymentStatuses = []string{"PENDING", "COMPLETED", "REJECTED"}

func (q query3) Query() string {
	amount := rng.Intn(1000)
	status := paymentStatuses[rng.Intn(3)]
	return fmt.Sprintf("INSERT INTO sqli_payments (amount, status) VALUES (%d, '%s')", amount, status)
}
func (q query3) Args() []any { return []any{} }

// // query4 is meant to be like query1 but executed so infrequently that it
// // doesn't trigger the warning due to the cache ttl
// type query4 struct{}
//
// func (q query4) Query() string { return "" }
// func (q query4) Args() []any   { return []any{} }

func runQuery(db *gosql.DB, q query) error {
	query := q.Query()
	args := q.Args()
	updateStmt, err := db.Prepare(query)
	if err != nil {
		return errors.CombineErrors(err, db.Close())
	}

	_, err = updateStmt.Exec(args...)
	return err
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

	ql := workload.QueryLoad{
		Close: func(_ context.Context) error {
			return db.Close()
		},
	}
	for i := 0; i < s.connFlags.Concurrency; i++ {
		hists := reg.GetHandle()
		workerFn := func(ctx context.Context) error {
			start := timeutil.Now()

			err = runQuery(db, query3{})
			if err != nil {
				fmt.Println(query3{}.Query())
				return err
			}

			err = runQuery(db, query2{})
			if err != nil {
				fmt.Println(query2{}.Query())
				return err
			}

			err = runQuery(db, query1{})
			if err != nil {
				fmt.Println(query1{}.Query())
				return err
			}

			elapsed := timeutil.Since(start)
			hists.Get(`transfer`).Record(elapsed)
			return err
		}
		ql.WorkerFns = append(ql.WorkerFns, workerFn)
	}
	return ql, nil
}
