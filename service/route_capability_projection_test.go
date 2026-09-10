package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A row projected by an older build no longer describes the same facts as the
// current shape, so it must not select a channel. The live index is the last
// boundary before routing and is therefore where the version is enforced.
func TestRebuildRouteCapabilityIndexRejectsStaleProjectionVersion(t *testing.T) {
	db := setupRouteProfileTest(t)
	_, _, channelID := seedRouteProfileFixture(t, db)
	endpoints, err := common.Marshal([]string{string(constant.EndpointTypeOpenAI)})
	require.NoError(t, err)

	for _, test := range []struct {
		name              string
		projectionVersion int
		indexed           bool
	}{
		{name: "current projection is indexed", projectionVersion: model.ChannelCapabilityProjectionCurrent, indexed: true},
		{name: "superseded projection is rejected", projectionVersion: model.ChannelCapabilityProjectionV1, indexed: false},
		{name: "unset projection is rejected", projectionVersion: 0, indexed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, db.Exec("DELETE FROM channel_model_capabilities").Error)
			require.NoError(t, db.Exec("DELETE FROM channel_capability_snapshots").Error)
			require.NoError(t, db.Create(&model.ChannelCapabilitySnapshot{
				ChannelID: channelID, ActiveVersion: 1, CatalogVersion: "catalog-1",
				SourceHash: "source-1", RefreshStatus: model.RouteCapabilityRefreshActive,
			}).Error)
			require.NoError(t, db.Create(&model.ChannelModelCapability{
				ChannelID: channelID, SnapshotVersion: 1, RequestModel: "gpt-test", ActualModel: "gpt-test",
				LabSlug: "openai", Source: "canonical", Confidence: 1, CatalogVersion: "catalog-1",
				SourceHash: "source-1", AbilityEnabled: true, EndpointTypes: string(endpoints),
				ChannelStatus: common.ChannelStatusEnabled, ChannelType: constant.ChannelTypeOpenAI,
				ProjectionVersion: test.projectionVersion, State: model.RouteCapabilityStateEligible,
			}).Error)

			require.NoError(t, RebuildRouteCapabilityIndex(context.Background()))
			index, _ := routeCapabilityIndex.Load().(*capabilityIndex)
			require.NotNil(t, index)
			assert.Equal(t, test.indexed, len(index.ByRequestModel["gpt-test"]) == 1)
		})
	}
}

// The source fingerprint does not change when the projection shape changes, so
// a scan that only compares hashes would skip every channel after an upgrade
// and leave them on rows the index rejects. Skipping must also require a
// current projection.
func TestFingerprintScanRefusesToSkipStaleProjection(t *testing.T) {
	db := setupRouteProfileTest(t)
	_, _, channelID := seedRouteProfileFixture(t, db)
	const sourceHash = "fingerprint-source"
	const catalogVersion = "fingerprint-catalog"
	require.NoError(t, db.Create(&model.ChannelCapabilitySnapshot{
		ChannelID: channelID, ActiveVersion: 1, CatalogVersion: catalogVersion,
		SourceHash: sourceHash, RefreshStatus: model.RouteCapabilityRefreshActive,
	}).Error)

	capability := model.ChannelModelCapability{
		ChannelID: channelID, SnapshotVersion: 1, RequestModel: "gpt-test", ActualModel: "gpt-test",
		LabSlug: "openai", Source: "canonical", Confidence: 1, CatalogVersion: catalogVersion,
		SourceHash: sourceHash, AbilityEnabled: true, ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1,
		State: model.RouteCapabilityStateEligible,
	}
	require.NoError(t, db.Create(&capability).Error)
	assert.False(t, channelCapabilitySnapshotMatches(channelID, sourceHash, catalogVersion),
		"a stale projection must be reprojected even when the fingerprint is unchanged")

	require.NoError(t, db.Model(&model.ChannelModelCapability{}).Where("id = ?", capability.ID).
		Update("projection_version", model.ChannelCapabilityProjectionCurrent).Error)
	assert.True(t, channelCapabilitySnapshotMatches(channelID, sourceHash, catalogVersion),
		fmt.Sprintf("an unchanged fingerprint at projection %d may be skipped", model.ChannelCapabilityProjectionCurrent))
}
