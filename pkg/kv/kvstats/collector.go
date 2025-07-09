package kvstats

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/settings"
	"github.com/cockroachdb/cockroach/pkg/util/container/ring"
)

// KVAccessSamplingEnabled controls whether the system is collecting samples at
// the kv layer.
var KVAccessSamplingEnabled = settings.RegisterBoolSetting(
	settings.SystemVisible,
	"server.telemetry.kv_access_sampling.enabled",
	"enable/disable sampling kv access statistics",
	true,
)

const (
	numSamples = 1000
)

// Sample keeps track of a single kv access batch. It records information like
// wait time, cpu utilization, and write bytes associated with the batch, so
// that more information can be derived from it in aggregate.
type Sample struct {
	StartTime  int64
	Span       roachpb.Span
	CPUTime    int64
	WaitTime   int64 // Time spent waiting for locks in nanoseconds
	WriteBytes int64 // Number of bytes written in this batch
}

func (s *Sample) Proto() any {
	return nil
}

type Collector struct {
	mu struct {
		sync.Mutex
		Samples ring.Buffer[Sample]
		Start   time.Time // When the collector was initialized for collecting samples.
	}
}

func NewCollector() *Collector {
	c := &Collector{}
	c.InitSamples()
	return c
}

// Record adds a sample to the collection if shouldSample returns true.
func (c *Collector) Record(ctx context.Context, s Sample) {
	if c.shouldSample(s) {
		c.addSample(s)
	}
}

func (c *Collector) Samples() []Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Sample, c.mu.Samples.Length())
	for i := 0; i < c.mu.Samples.Length(); i++ {
		result[i] = c.mu.Samples.At(i)
	}
	return result
}

func (c *Collector) InitSamples() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mu.Samples = ring.Buffer[Sample]{}
	c.mu.Start = time.Now()
}

// shouldSample determines whether the collector should sample based on a few
// factors:
// 1. The appropriate cluster settings are enabled for sampling to occur.
// 2. The collector is not yet full.
// 3. The sample is selected by a weighted random sampling process based on one
// or more heuristics (wait time, cpu, etc).
//
// One of the main goals of this sampling function is that its execution is
// lightweight. As it will be in the hotpath, we want to avoid any wasted cpu cycles
// and therefore will be short circuiting early and often.
//
// (note): right now this just does a simple random sampling of .1% of all requests.
func (c *Collector) shouldSample(_ Sample) bool {
	return rand.Intn(1000) == 0
}

// addSample adds a sample to the collector. If the collector is full, it will
// evict the oldest record.
//
// In the future, depending on the weighted sampling function used, it may
// require index-specific eviction
func (c *Collector) addSample(s Sample) {
	// acquire a read lock or equivalent on the collector so that it doesn't begin processing.
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mu.Samples.Push(s)
	if c.mu.Samples.Length() > numSamples {
		c.mu.Samples.Pop(1)
	}
}
