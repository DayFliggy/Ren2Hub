package main

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupRetiredAdmissionNamespaceOnly(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	ctx := context.Background()
	for _, key := range []string{"gateway:provider:v1:channel:1", "gateway:rate:v1:user:1", "route:lease:v20:untouched", "billing:recovery:1"} {
		require.NoError(t, client.Set(ctx, key, "retained", 0).Err())
	}
	require.NoError(t, client.ZAdd(ctx, legacyNamespace+"channel:1", &redis.Z{Member: "historical", Score: 1}).Err())
	require.NoError(t, client.Set(ctx, legacyNamespace+"queue:1", "historical", 0).Err())
	report, err := cleanupLegacyAdmission(ctx, client, false)
	require.NoError(t, err)
	assert.EqualValues(t, 2, report.Observed)
	assert.Zero(t, report.Unlinked)
	assert.Equal(t, map[string]int64{"string": 1, "zset": 1}, report.Types)
	assert.Len(t, server.Keys(), 6)
	report, err = cleanupLegacyAdmission(ctx, client, true)
	require.NoError(t, err)
	assert.EqualValues(t, 2, report.Unlinked)
	assert.Len(t, server.Keys(), 4)
	for _, key := range server.Keys() {
		assert.NotContains(t, key, legacyNamespace)
	}
	report, err = cleanupLegacyAdmission(ctx, client, true)
	require.NoError(t, err)
	assert.Zero(t, report.Unlinked)
}
