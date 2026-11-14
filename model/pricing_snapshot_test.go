package model

import (
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pricingHasModel(prices []Pricing, name string) bool {
	return slices.ContainsFunc(prices, func(p Pricing) bool { return p.ModelName == name })
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
	prices := GetPricing()
	assert.True(t, pricingHasModel(prices, "snapshot-late-model"))
	assert.NotEmpty(t, GetModelSupportEndpointTypes("snapshot-late-model"))
}
