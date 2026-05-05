// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package executionattributes

// GatewayResolver computes IDs and triggers durable writes for
// statement-shaped attribution at the SQL gateway.
type GatewayResolver struct {
	cache  *Cache
	writer *Writer
}

// NewGatewayResolver constructs the resolver bound to a per-node cache
// and writer.
func NewGatewayResolver(cache *Cache, writer *Writer) *GatewayResolver {
	return &GatewayResolver{cache: cache, writer: writer}
}

// Resolve returns the ID for the given attributes. On cache miss, the
// entry is added to the local cache immediately and enqueued for durable
// write. The returned ID can be stamped on outbound BatchRequests
// without waiting for the write.
func (g *GatewayResolver) Resolve(stmtFingerprintID []byte, appName string) ID {
	id := ComputeID(stmtFingerprintID, appName)
	if _, ok := g.cache.Get(id); ok {
		return id
	}
	// Defensive copy: callers may reuse the slice.
	entry := Entry{
		StmtFingerprintID: append([]byte(nil), stmtFingerprintID...),
		AppName:           appName,
	}
	g.cache.Put(id, entry)
	g.writer.Enqueue(writeRequest{ID: id, Entry: entry})
	return id
}
