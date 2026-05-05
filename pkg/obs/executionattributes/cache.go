// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

import (
	"container/list"
	"sync"
)

// Entry is the resolved attribute set for an ID.
type Entry struct {
	StmtFingerprintID []byte
	AppName           string
}

// Cache is a thread-safe LRU cache from ID to Entry. Used at the gateway
// (to avoid redundant durable writes) and at the sampler (to denormalize
// at sample-write time).
type Cache struct {
	mu      sync.Mutex
	maxSize int
	entries map[ID]*list.Element
	lru     *list.List // front = most recently used
}

type cacheItem struct {
	id    ID
	entry Entry
}

// NewCache creates an LRU cache with the given maximum entry count.
func NewCache(maxSize int) *Cache {
	return &Cache{
		maxSize: maxSize,
		entries: make(map[ID]*list.Element, maxSize),
		lru:     list.New(),
	}
}

// Get returns the entry for id and marks it most-recently-used.
func (c *Cache) Get(id ID) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[id]
	if !ok {
		return Entry{}, false
	}
	c.lru.MoveToFront(el)
	return el.Value.(*cacheItem).entry, true
}

// Put inserts or updates the entry for id, evicting the least-recently-used
// entry if the cache is over capacity.
func (c *Cache) Put(id ID, entry Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[id]; ok {
		el.Value.(*cacheItem).entry = entry
		c.lru.MoveToFront(el)
		return
	}
	el := c.lru.PushFront(&cacheItem{id: id, entry: entry})
	c.entries[id] = el
	for c.lru.Len() > c.maxSize {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		delete(c.entries, oldest.Value.(*cacheItem).id)
		c.lru.Remove(oldest)
	}
}

// Size returns the current number of entries in the cache.
func (c *Cache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}
