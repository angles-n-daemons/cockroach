// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package goodhistogram

import (
	"sync/atomic"
	"unsafe"
)

// WindowedHistogram combines a cumulative histogram (for Prometheus export)
// with a resettable current-window histogram (for TSDB quantile computation).
//
// On each scrape, the caller takes a windowed snapshot via WindowedSnapshot(),
// which atomically swaps the current window with a fresh Histogram instance.
// This means the window always represents exactly the period since the last
// scrape — no timer-based rotation, no prev/cur merging, no jitter.
//
// Recording is lock-free: each Record() atomically increments counters in both
// the cumulative and current-window histograms.
type WindowedHistogram struct {
	config Config
	cum    *Histogram // cumulative, never reset
	// cur points to the current window's Histogram. Swapped atomically on
	// each WindowedSnapshot() call. Uses atomic.Pointer via unsafe to avoid
	// the sync.Mutex that atomic.Value's type-assertion path would need.
	cur unsafe.Pointer // *Histogram
}

// NewWindowed creates a WindowedHistogram for the given range and error bound.
func NewWindowed(lo, hi, desiredError float64) *WindowedHistogram {
	config := NewConfig(lo, hi, desiredError)
	return &WindowedHistogram{
		config: config,
		cum:    newHistogram(&config),
		cur:    unsafe.Pointer(newHistogram(&config)),
	}
}

// Record adds a value to both the cumulative and current-window histograms.
func (wh *WindowedHistogram) Record(v int64) {
	wh.cum.Record(v)
	cur := (*Histogram)(atomic.LoadPointer(&wh.cur))
	cur.Record(v)
}

// CumulativeSnapshot returns a point-in-time snapshot of the cumulative
// (all-time) histogram data. Suitable for Prometheus export.
func (wh *WindowedHistogram) CumulativeSnapshot() Snapshot {
	return wh.cum.Snapshot()
}

// WindowedSnapshot atomically swaps the current window with a fresh Histogram
// and returns a snapshot of the old window. The returned snapshot represents
// all observations recorded since the previous call to WindowedSnapshot.
//
// This should be called by the TSDB scraper at a regular interval (e.g. 10s).
// The window duration is implicitly defined by the scrape interval.
func (wh *WindowedHistogram) WindowedSnapshot() Snapshot {
	fresh := newHistogram(&wh.config)
	old := (*Histogram)(atomic.SwapPointer(&wh.cur, unsafe.Pointer(fresh)))
	return old.Snapshot()
}

// Config returns the histogram's configuration.
func (wh *WindowedHistogram) Config() Config {
	return wh.config
}
