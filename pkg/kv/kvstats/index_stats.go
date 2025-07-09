package kvstats

import (
	"time"

	"github.com/cockroachdb/cockroach/pkg/keys"
)

type IndexStats struct {
	TableID      uint32
	IndexID      uint32
	CPUTime      int64
	WriteBytes   int64
	WaitTime     int64
	SamplePeriod time.Duration
}

/*
ComputeStats turns a set of samples into a set of index statistics.

It accomplishes this by first organizing the samples into a set of time ordered
sample lists, broken out by table / index.

Once there, it uses the duration measurement, as well as the samples
to estimate the used resources.
*/
func ComputeStats(samples []Sample, d time.Duration) ([]IndexStats, error) {
	// create sample lists, separated by the index of their start key
	sep := map[uint32]map[uint32][]Sample{}
	for i := range samples {
		_, tid, iid, err := keys.DecodeTableIDIndexID(samples[i].Span.Key)
		if err != nil {
			return nil, err
		}
		if sep[tid] == nil {
			sep[tid] = map[uint32][]Sample{}
		}
		if sep[tid][iid] == nil {
			sep[tid][iid] = []Sample{}
		}

		sep[tid][iid] = append(sep[tid][iid], samples[i])
	}

	results := []IndexStats{}
	for tid := range sep {
		for iid := range sep[tid] {
			stats := ProcessIndex(tid, iid, sep[tid][iid], d)
			results = append(results, stats)
		}
	}
	return results, nil
}

/*
ProcessIndex does the processing of samples for a single index.
*/
func ProcessIndex(tid, iid uint32, samples []Sample, d time.Duration) IndexStats {
	stats := IndexStats{TableID: tid, IndexID: iid, SamplePeriod: d}
	for i := range samples {
		stats.CPUTime += samples[i].CPUTime
		stats.WriteBytes += samples[i].WriteBytes
		stats.WaitTime += samples[i].WaitTime
	}
	return stats
}
