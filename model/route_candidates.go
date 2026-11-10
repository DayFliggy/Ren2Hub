package model

import (
	"context"
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
// Like upstream GetChannel and the memory cache, channels declaring the exact
// name win; the routing name (for example without @ modifiers) is only tried
// when no exact channel passes the filters.
func RouteChannelRows(ctx context.Context, modelName string, filters []dto.ChannelFilter) ([]*Channel, error) {
	return RouteChannelRowsAccepting(ctx, modelName, filters, nil)
}

// RouteChannelRowsAccepting 与 RouteChannelRows 相同，另用 accept 判调用方的策略（例如
// Key 的倍率上限和手动列表）。精确模型名一档要在全部规则之后仍有渠道才算命中，否则改用
// 归一化名，避免精确渠道被后续规则滤空时整次选路无候选。accept 为 nil 表示不加限制。
func RouteChannelRowsAccepting(ctx context.Context, modelName string, filters []dto.ChannelFilter, accept func(*Channel) bool) ([]*Channel, error) {
	if modelName == "" {
		rows := []*Channel{}
		if err := DB.WithContext(ctx).Order("id asc").Find(&rows).Error; err != nil {
			return nil, err
		}
		return rows, nil
	}
	rows, err := routeChannelRowsForAbility(ctx, modelName, modelName, filters, accept)
	if err != nil || len(rows) > 0 {
		return rows, err
	}
	if normalized := ratio_setting.RoutingMatchModelName(modelName); normalized != "" && normalized != modelName {
		return routeChannelRowsForAbility(ctx, normalized, modelName, filters, accept)
	}
	return rows, nil
}

// routeChannelRowsForAbility 返回声明了 abilityModel 且满足过滤条件的渠道；过滤仍按请求的
// modelName 判定，与上游 filterAbilitiesByConstraints 的参数一致。
func routeChannelRowsForAbility(ctx context.Context, abilityModel, modelName string, filters []dto.ChannelFilter, accept func(*Channel) bool) ([]*Channel, error) {
	rows := []*Channel{}
	var abilities []Ability
	if err := DB.WithContext(ctx).Select("channel_id", "model").Where("model = ? AND enabled = ?", abilityModel, true).Find(&abilities).Error; err != nil {
		return nil, err
	}
	// MySQL 默认排序规则不区分大小写，会命中只差大小写的模型名；在 Go 侧精确比对，
	// 使三种数据库与上游内存缓存的精确查找一致。
	ids := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		if ability.Model == abilityModel {
			ids = append(ids, ability.ChannelId)
		}
	}
	if len(ids) == 0 {
		return rows, nil
	}
	if err := DB.WithContext(ctx).Where("id IN ?", ids).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*Channel, 0, len(rows))
	for _, c := range rows {
		if ok, _ := ChannelSatisfiesFilters(c, modelName, filters); ok && (accept == nil || accept(c)) {
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

// DeclaredRouteModels 返回每个渠道声明的全部模型及其能力行是否启用，只给路由预览说明原因用。
// 渠道禁用时能力行也被置为禁用，只读启用行就无法区分“禁用了”和“不服务该模型”。
func DeclaredRouteModels(ctx context.Context, channelIDs []int) (map[int]map[string]bool, error) {
	result := map[int]map[string]bool{}
	if len(channelIDs) == 0 {
		return result, nil
	}
	var rows []Ability
	if err := DB.WithContext(ctx).Select("channel_id", "model", "enabled").Where("channel_id IN ?", channelIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if result[row.ChannelId] == nil {
			result[row.ChannelId] = map[string]bool{}
		}
		result[row.ChannelId][row.Model] = row.Enabled
	}
	return result, nil
}
