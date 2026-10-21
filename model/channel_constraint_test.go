package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelSatisfiesFiltersPerChannel keeps the per-channel cases of the
// removed id-list filter: identity keys, legacy task types, request paths and
// first-violation attribution in filterEvalOrder regardless of slice order.
func TestChannelSatisfiesFiltersPerChannel(t *testing.T) {
	alphaSetting := `{"task_plugin_key":"alpha"}`
	betaSetting := `{"task_plugin_key":"beta"}`
	alpha := &Channel{Id: 900001, Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled, Setting: &alphaSetting}
	beta := &Channel{Id: 900002, Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled, Setting: &betaSetting}
	ordinary := &Channel{Id: 900003, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	kling := &Channel{Id: 900004, Type: constant.ChannelTypeKling, Status: common.ChannelStatusEnabled}
	jimeng := &Channel{Id: 900005, Type: constant.ChannelTypeJimeng, Status: common.ChannelStatusEnabled}
	matchingCustom := &Channel{Id: 900010, Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled}
	matchingCustom.SetOtherSettings(kitdto.ChannelOtherSettings{
		AdvancedCustom: &kitdto.AdvancedCustomConfig{
			Routes: []kitdto.AdvancedCustomRoute{{
				IncomingPath: "/v1/chat/completions",
				Models:       []string{"gpt-4"},
			}},
		},
	})
	otherCustom := &Channel{Id: 900011, Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled}
	otherCustom.SetOtherSettings(kitdto.ChannelOtherSettings{
		AdvancedCustom: &kitdto.AdvancedCustomConfig{
			Routes: []kitdto.AdvancedCustomRoute{{
				IncomingPath: "/v1/responses",
				Models:       []string{"gpt-4"},
			}},
		},
	})

	pathFilter := dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: "/v1/chat/completions"}
	emptyPathFilter := dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: ""}

	tests := []struct {
		name     string
		channel  *Channel
		model    string
		filters  []dto.ChannelFilter
		wantOK   bool
		wantKind dto.ChannelFilterKind
	}{
		{"identity keeps matching type-59 key", alpha, "shared", identityFilters("alpha", nil), true, ""},
		{"identity drops other type-59 key", beta, "shared", identityFilters("alpha", nil), false, dto.FilterTaskPluginIdentity},
		{"identity empty key drops type-59", alpha, "shared", identityFilters("", nil), false, dto.FilterTaskPluginIdentity},
		{"identity empty key keeps ordinary channel", ordinary, "ordinary", identityFilters("", nil), true, ""},
		{"identity keeps listed legacy type", kling, "legacy", identityFilters("legacy-alpha", []int{constant.ChannelTypeKling}), true, ""},
		{"identity drops unlisted legacy type", jimeng, "legacy", identityFilters("legacy-alpha", []int{constant.ChannelTypeKling}), false, dto.FilterTaskPluginIdentity},
		{"identity keeps every listed legacy type", jimeng, "legacy", identityFilters("legacy-alpha", []int{constant.ChannelTypeKling, constant.ChannelTypeJimeng}), true, ""},
		{"identity keyed with no types drops legacy", kling, "legacy", identityFilters("legacy-alpha", nil), false, dto.FilterTaskPluginIdentity},
		{"empty request path passes type-58", otherCustom, "gpt-4", []dto.ChannelFilter{emptyPathFilter}, true, ""},
		{"request path keeps ordinary", ordinary, "gpt-4", []dto.ChannelFilter{pathFilter}, true, ""},
		{"request path keeps matching type-58", matchingCustom, "gpt-4", []dto.ChannelFilter{pathFilter}, true, ""},
		{"request path drops unmatched type-58", otherCustom, "gpt-4", []dto.ChannelFilter{pathFilter}, false, dto.FilterRequestPath},
		{"identity is blamed after path passes", matchingCustom, "gpt-4", []dto.ChannelFilter{pathFilter, identityFilters("missing", nil)[0]}, false, dto.FilterTaskPluginIdentity},
		{"path is blamed first whatever the slice order", otherCustom, "gpt-4", []dto.ChannelFilter{identityFilters("", nil)[0], pathFilter}, false, dto.FilterRequestPath},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ok, kind := ChannelSatisfiesFilters(testCase.channel, testCase.model, testCase.filters)
			assert.Equal(t, testCase.wantOK, ok)
			assert.Equal(t, testCase.wantKind, kind)
		})
	}
}

func TestChannelSatisfiesFilters(t *testing.T) {
	alphaSetting := `{"task_plugin_key":"alpha"}`
	alpha := &Channel{Id: 1, Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled, Setting: &alphaSetting}
	ordinary := &Channel{Id: 2, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	custom := &Channel{Id: 3, Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled}
	custom.SetOtherSettings(kitdto.ChannelOtherSettings{
		AdvancedCustom: &kitdto.AdvancedCustomConfig{
			Routes: []kitdto.AdvancedCustomRoute{{
				IncomingPath: "/v1/chat/completions",
				Models:       []string{"gpt-4"},
			}},
		},
	})

	ok, kind := ChannelSatisfiesFilters(nil, "gpt-4", nil)
	assert.False(t, ok)
	assert.Equal(t, dto.ChannelFilterKind(""), kind)

	ok, kind = ChannelSatisfiesFilters(alpha, "shared", identityFilters("alpha", nil))
	require.True(t, ok)
	assert.Equal(t, dto.ChannelFilterKind(""), kind)

	ok, kind = ChannelSatisfiesFilters(alpha, "shared", identityFilters("beta", nil))
	assert.False(t, ok)
	assert.Equal(t, dto.FilterTaskPluginIdentity, kind)

	ok, kind = ChannelSatisfiesFilters(ordinary, "gpt-4", []dto.ChannelFilter{{
		Kind:        dto.FilterRequestPath,
		RequestPath: "/v1/chat/completions",
	}})
	require.True(t, ok)
	assert.Equal(t, dto.ChannelFilterKind(""), kind)

	ok, kind = ChannelSatisfiesFilters(custom, "gpt-4", []dto.ChannelFilter{{
		Kind:        dto.FilterRequestPath,
		RequestPath: "/v1/responses",
	}})
	assert.False(t, ok)
	assert.Equal(t, dto.FilterRequestPath, kind)
}
