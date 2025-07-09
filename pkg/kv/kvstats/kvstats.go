package kvstats

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/util/stop"
)

// Init starts the kv stats subsystem. This system consists of a few parts, a
// task, a collector, a processor and a store. Each of these is a database
// process singleton, in that each database process contains one of each of
// these structs.
//
// The task is a node-local task which coordinates the processing and saving of
// aggregate data from the access samples.
//
// The collector is responsible for storing kv access samples. It is passed to
// the replicas, and written to on kv batch activity. Each samples stores a few
// bites of information about that particular kv batch including:
// - The key span used for that batch.
// - The CPU time that batched used for processing.
// - The wait time required for that batch to complete.
// - The number of bytes in or out of the storage subsystem.
//
// The processor is a utility struct which takes raw samples and computes
// aggregate statistics for downstream usage.
//
// The store is a storage utility which is responsible for persisting the stats
// to the keyspace.
func Init(ctx context.Context, stopper *stop.Stopper) (*Collector, error) {
	collector := NewCollector()
	task := &Task{Collector: collector}
	return collector, task.Start(ctx, stopper)
}
