package service

import (
	"errors"
	"sort"

	"github.com/QuantumNous/new-api/common"
)

const (
	upstreamRatioNewAPIUsagePath   = "/api/usage/token/"
	upstreamRatioNewAPIPricingPath = "/api/pricing"
	upstreamRatioNewAPIAutoGroup   = "auto"
)

// NewAPIGroup 是上游定价页公开的一个分组。Ratio 为空表示上游没给倍率。
type NewAPIGroup struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Ratio       *float64 `json:"ratio,omitempty"`
}

type NewAPIDetail struct {
	Groups     []NewAPIGroup `json:"groups"`
	AutoGroups []string      `json:"auto_groups,omitempty"`
}

type newAPIUsageResponse struct {
	Code    *bool `json:"code"`
	Success *bool `json:"success"`
	Data    *struct {
		Object string `json:"object"`
		Name   string `json:"name"`
	} `json:"data"`
}

// parseNewAPIUsage 识别 NewAPI GetTokenUsage 的成功响应，返回令牌名。
// 成功时外层是 {"code":true,...}，兼容早期 {"success":true,...}。
func parseNewAPIUsage(body []byte) (string, error) {
	var response newAPIUsageResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return "", err
	}
	succeeded := (response.Code != nil && *response.Code) || (response.Success != nil && *response.Success)
	if !succeeded || response.Data == nil || response.Data.Object != "token_usage" {
		return "", errors.New("unexpected token usage response")
	}
	return response.Data.Name, nil
}

type newAPIPricingResponse struct {
	Success     bool               `json:"success"`
	GroupRatio  map[string]float64 `json:"group_ratio"`
	UsableGroup map[string]string  `json:"usable_group"`
	AutoGroups  []string           `json:"auto_groups"`
}

// parseNewAPIPricing 读取匿名 /api/pricing 的分组。匿名请求只拿到默认用户分组可见的
// 分组和基础倍率，拿不到按用户分组覆盖的倍率。
func parseNewAPIPricing(body []byte) (*NewAPIDetail, error) {
	var response newAPIPricingResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if !response.Success || response.UsableGroup == nil {
		return nil, errors.New("unexpected pricing response")
	}
	detail := &NewAPIDetail{Groups: make([]NewAPIGroup, 0, len(response.UsableGroup)), AutoGroups: response.AutoGroups}
	for name, description := range response.UsableGroup {
		group := NewAPIGroup{Name: name, Description: description}
		if ratio, ok := response.GroupRatio[name]; ok && validBillingMultiplier(ratio) {
			group.Ratio = &ratio
		}
		detail.Groups = append(detail.Groups, group)
	}
	sort.Slice(detail.Groups, func(i, j int) bool { return detail.Groups[i].Name < detail.Groups[j].Name })
	return detail, nil
}

// ratioOf 按分组名严格相等查找倍率。NewAPI 分组按 map 键精确查找，令牌名原样存储，
// 所以这里不做大小写或空白规范化。auto 分组的实际倍率取决于路由结果，不能当作固定倍率。
func (d *NewAPIDetail) ratioOf(groupName string) (float64, bool) {
	if d == nil || groupName == "" || groupName == upstreamRatioNewAPIAutoGroup {
		return 0, false
	}
	for _, group := range d.Groups {
		if group.Name == groupName && group.Ratio != nil {
			return *group.Ratio, true
		}
	}
	return 0, false
}
