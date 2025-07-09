package kvstats

import (
	"context"
	"time"

	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/cockroachdb/cockroach/pkg/util/stop"
)

const StatsInterval = time.Minute

type Task struct {
	Collector *Collector
}

func (t *Task) Start(ctx context.Context, stopper *stop.Stopper) error {
	return stopper.RunAsyncTask(ctx, "kv-stats-processing", func(ctx context.Context) {
		t.loop(ctx, stopper)
	})
}

func (t *Task) loop(ctx context.Context, stopper *stop.Stopper) {
	for {
		select {
		case <-stopper.ShouldQuiesce():
			return
		case <-ctx.Done():
			return
		case <-time.After(StatsInterval):
			err := t.process(ctx)
			if err != nil {
				log.Ops.Errorf(ctx, "kvstats processing error: %v", err)
			}
			return
		}
	}
}

/*
Here we can compute some useful statistics from the samples collected.
*/
func (t *Task) process(ctx context.Context) error {
	indexStats, err := ComputeStats(t.Collector.Samples(), StatsInterval)
	if err != nil {
		return err
	}
	// here we would persist these stats to some usable table
	log.Infof(ctx, "here are my stats %v", indexStats)
	return nil
}
