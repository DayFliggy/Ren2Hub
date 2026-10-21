package model

import (
	"context"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// channelServesRequestPath 判断非 AdvancedCustom 渠道能否处理该路径。目前只有
// /v1/responses/compact 有可确定的端点能力；判断与 PrepareResponsesRequest 的运行时
// 拒绝完全一致，使不支持的渠道在选路时就被跳过，而不是返回不可重试的 400。
func channelServesRequestPath(ch *Channel, requestPath string) bool {
	if !strings.HasPrefix(requestPath, "/v1/responses/compact") {
		return true
	}
	apiType, _ := common.ChannelType2APIType(ch.Type)
	return common.SupportsResponsesCompact(ch.Type, apiType)
}

// ChannelRoutable 是读侧“渠道能否被选中”的唯一规则：已启用且不是退役的 type 57。
// 写入侧拒绝 type 57 在 controller/channel.go，属于另一处合同，不走这里。
func ChannelRoutable(ch *Channel) bool {
	return ch != nil && ch.Status == common.ChannelStatusEnabled && ch.Type != constant.ChannelTypeCodex
}

// RouteChannelRows is shared by dispatch and public projections. Querying all
// matching rows before filtering prevents an arbitrary top-k from hiding a route.
func RouteChannelRows(ctx context.Context, modelName string, filters []dto.ChannelFilter) ([]*Channel, error) {
	rows := []*Channel{}
	q := DB.WithContext(ctx).Model(&Channel{})
	if modelName != "" {
		names := []string{modelName}
		if normalized := ratio_setting.RoutingMatchModelName(modelName); normalized != modelName {
			names = append(names, normalized)
		}
		var abilities []Ability
		if err := DB.WithContext(ctx).Select("channel_id", "model").Where("model IN ? AND enabled = ?", names, true).Find(&abilities).Error; err != nil {
			return nil, err
		}
		// MySQL 默认排序规则不区分大小写，IN 会命中只差大小写的模型名；在 Go 侧
		// 精确比对，使三种数据库与上游内存缓存的精确查找一致。
		ids := make([]int, 0, len(abilities))
		for _, ability := range abilities {
			if slices.Contains(names, ability.Model) {
				ids = append(ids, ability.ChannelId)
			}
		}
		if len(ids) == 0 {
			return rows, nil
		}
		q = q.Where("id IN ?", ids)
	}
	if err := q.Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	if modelName == "" {
		return rows, nil
	}
	result := make([]*Channel, 0, len(rows))
	for _, c := range rows {
		if ok, _ := ChannelSatisfiesFilters(c, modelName, filters); ok {
			result = append(result, c)
		}
	}
	return result, nil
}

// EnabledRouteModels projects the same enabled ability rows used by dispatch.
// It is a single batch lookup, including when the catalog spans many channels.
func EnabledRouteModels(ctx context.Context, channelIDs []int) (map[int][]string, error) {
	result := map[int][]string{}
	if len(channelIDs) == 0 {
		return result, nil
	}
	var rows []Ability
	if err := DB.WithContext(ctx).Select("channel_id", "model").Where("channel_id IN ? AND enabled = ?", channelIDs, true).Order("model asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ChannelId] = append(result[row.ChannelId], row.Model)
	}
	return result, nil
}
