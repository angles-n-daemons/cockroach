// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package perftrace_test

import (
	"context"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/cockroachdb/cockroach/pkg/sql/execinfrapb"
	"github.com/cockroachdb/cockroach/pkg/sql/perftrace"
	"github.com/cockroachdb/cockroach/pkg/util/hlc"
	"github.com/stretchr/testify/require"
)

// TestQueryTagPropagation verifies that query tags can be serialized to
// SetupFlowRequest and then extracted back into the context on a remote node.
func TestQueryTagPropagation(t *testing.T) {
	// Simulate gateway: add query tags to context
	gatewayCtx := context.Background()
	gatewayTags := []perftrace.QueryTag{
		{Key: "app", Value: "myapp"},
		{Key: "action", Value: "getUser"},
		{Key: "traceparent", Value: "00-abc123-def456-01"},
	}
	gatewayCtx = perftrace.WithQueryTags(gatewayCtx, gatewayTags)

	// Simulate gateway: populate SetupFlowRequest with query tags (as done in distsql_running.go)
	var setupReq execinfrapb.SetupFlowRequest
	if tags := perftrace.GetQueryTagsFromContext(gatewayCtx); len(tags) > 0 {
		setupReq.QueryTags = make([]execinfrapb.QueryTag, len(tags))
		for i, tag := range tags {
			setupReq.QueryTags[i] = execinfrapb.QueryTag{
				Key:   tag.Key,
				Value: tag.Value,
			}
		}
	}

	// Verify SetupFlowRequest has the tags
	require.Len(t, setupReq.QueryTags, 3)
	require.Equal(t, "app", setupReq.QueryTags[0].Key)
	require.Equal(t, "myapp", setupReq.QueryTags[0].Value)
	require.Equal(t, "action", setupReq.QueryTags[1].Key)
	require.Equal(t, "getUser", setupReq.QueryTags[1].Value)

	// Simulate remote node: extract query tags from request into context (as done in server.go)
	remoteCtx := context.Background()
	if len(setupReq.QueryTags) > 0 {
		perfTags := make([]perftrace.QueryTag, len(setupReq.QueryTags))
		for i, tag := range setupReq.QueryTags {
			perfTags[i] = perftrace.QueryTag{
				Key:   tag.Key,
				Value: tag.Value,
			}
		}
		remoteCtx = perftrace.WithQueryTags(remoteCtx, perfTags)
	}

	// Verify remote context has the tags
	remoteTags := perftrace.GetQueryTagsFromContext(remoteCtx)
	require.Len(t, remoteTags, 3)
	require.Equal(t, "app", remoteTags[0].Key)
	require.Equal(t, "myapp", remoteTags[0].Value)
	require.Equal(t, "action", remoteTags[1].Key)
	require.Equal(t, "getUser", remoteTags[1].Value)
	require.Equal(t, "traceparent", remoteTags[2].Key)
	require.Equal(t, "00-abc123-def456-01", remoteTags[2].Value)
}

// TestQueryTagsInStartSpan verifies that StartSpan captures query tags from context.
// This simulates what happens when a remote processor calls StartSpan.
func TestQueryTagsInStartSpan(t *testing.T) {
	// Create a collector (as would exist on a remote node)
	settings := cluster.MakeTestingClusterSettings()
	nodeIDContainer := &base.NodeIDContainer{}
	nodeIDContainer.Set(context.Background(), 2) // Simulate node 2
	clock := hlc.NewClockForTesting(nil)
	collector := perftrace.NewCollector(nodeIDContainer, clock, settings)

	// Enable perftrace with 100% sampling for testing
	perftrace.Enabled.Override(context.Background(), &settings.SV, true)
	perftrace.SampleRate.Override(context.Background(), &settings.SV, 1.0)

	// Simulate remote node: context has query tags (propagated from gateway)
	ctx := context.Background()
	queryTags := []perftrace.QueryTag{
		{Key: "app", Value: "testapp"},
		{Key: "route", Value: "/api/users"},
	}
	ctx = perftrace.WithQueryTags(ctx, queryTags)

	// Call StartSpan as processors do
	handle := collector.StartSpan(ctx, "sql.TableReader", 12345, 0)
	require.NotNil(t, handle)
	require.True(t, handle.ID() != 0, "span should be sampled")

	// Finish the span to record it
	handle.Finish()

	// Drain to get the recorded spans
	spans := collector.Sampler().Drain()
	require.Len(t, spans, 1)

	// Verify the span has the query tags
	span := spans[0]
	require.Equal(t, "sql.TableReader", span.Component)
	require.Len(t, span.QueryTags, 2)
	require.Equal(t, "app", span.QueryTags[0].Key)
	require.Equal(t, "testapp", span.QueryTags[0].Value)
	require.Equal(t, "route", span.QueryTags[1].Key)
	require.Equal(t, "/api/users", span.QueryTags[1].Value)
}

// TestQueryTagPropagationEmpty verifies that empty query tags are handled correctly.
func TestQueryTagPropagationEmpty(t *testing.T) {
	// Context without query tags
	ctx := context.Background()

	// GetQueryTagsFromContext returns nil
	tags := perftrace.GetQueryTagsFromContext(ctx)
	require.Nil(t, tags)

	// Simulate SetupFlowRequest with no tags
	var setupReq execinfrapb.SetupFlowRequest
	require.Len(t, setupReq.QueryTags, 0)

	// Remote extraction with no tags should not modify context
	remoteCtx := context.Background()
	if len(setupReq.QueryTags) > 0 {
		// This block should not execute
		t.Fatal("should not enter this block for empty tags")
	}

	remoteTags := perftrace.GetQueryTagsFromContext(remoteCtx)
	require.Nil(t, remoteTags)
}
