package model

import (
	"slices"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pricingHasModel(prices []Pricing, name string) bool {
	return slices.ContainsFunc(prices, func(p Pricing) bool { return p.ModelName == name })
}

// 并发读写下，读者只能看到完整发布的快照（配合 go test -race）。
func TestPricingSnapshotStaysConsistentUnderConcurrentRefresh(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingEndpointChannel(t, 301, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 301, "snapshot-model")
	InitChannelCache()
	RefreshPricing()

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				prices, _, _, err := ReadPricing()
				if !assert.NoError(t, err) {
					return
				}
				assert.True(t, pricingHasModel(prices, "snapshot-model"))
				assert.NotEmpty(t, GetModelSupportEndpointTypes("snapshot-model"))
				assert.True(t, pricingHasModel(GetPricing(), "snapshot-model"))
			}
		}()
	}
	for range 50 {
		RefreshPricing()
		InvalidatePricingCache()
	}
	wg.Wait()
}

// 作废后下一次读价格会重建；重建之前端点查询仍返回最后一次成功的结果。
func TestPricingInvalidationRebuildsOnNextRead(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingEndpointChannel(t, 302, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 302, "snapshot-model")
	InitChannelCache()
	require.True(t, pricingHasModel(GetPricing(), "snapshot-model"))

	insertPricingEndpointAbility(t, 302, "snapshot-late-model")
	assert.False(t, pricingHasModel(GetPricing(), "snapshot-late-model"), "a fresh snapshot is served without rebuilding")

	InvalidatePricingCache()
	assert.NotEmpty(t, GetModelSupportEndpointTypes("snapshot-model"), "endpoint lookups keep the last good snapshot until a rebuild")
	prices, _, _, err := ReadPricing()
	require.NoError(t, err)
	assert.True(t, pricingHasModel(prices, "snapshot-late-model"))
	assert.NotEmpty(t, GetModelSupportEndpointTypes("snapshot-late-model"))
}

// 刷新失败时明确报错，不发布 updatePricing 中途改了一半的工作区。
func TestPricingRefreshFailureNeverPublishesPartialState(t *testing.T) {
	resetPricingEndpointTestTables(t)
	common.MemoryCacheEnabled = false // 让高级自定义渠道的配置从数据库读取
	insertPricingEndpointChannel(t, 303, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/chat/completions"},
	))
	insertPricingEndpointAbility(t, 303, "snapshot-custom-model")
	insertPricingEndpointChannel(t, 304, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 304, "snapshot-model")
	RefreshPricing()
	before := GetModelSupportEndpointTypes("snapshot-custom-model")
	require.NotEmpty(t, before)

	require.NoError(t, DB.Exec("UPDATE channels SET settings = ? WHERE id = ?", "{broken", 303).Error)
	RefreshPricing()
	prices, vendors, endpoints, err := ReadPricing()
	require.Error(t, err, "a failed rebuild must surface instead of serving a half-built table")
	assert.Nil(t, prices)
	assert.Nil(t, vendors)
	assert.Nil(t, endpoints)
	assert.Nil(t, GetPricing())
	assert.Equal(t, before, GetModelSupportEndpointTypes("snapshot-custom-model"))

	require.NoError(t, DB.Exec("UPDATE channels SET settings = ? WHERE id = ?", "{}", 303).Error)
	prices, _, _, err = ReadPricing()
	require.NoError(t, err)
	assert.True(t, pricingHasModel(prices, "snapshot-model"))
}
