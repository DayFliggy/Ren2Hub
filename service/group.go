package service

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func ExpandCompactPermissionModels(models []string) []string {
	publishedPricing := make(map[string]struct{})
	for _, pricing := range model.GetPricing() {
		publishedPricing[pricing.ModelName] = struct{}{}
	}
	seen := make(map[string]struct{}, len(models)*2)
	expanded := make([]string, 0, len(models)*2)
	for _, modelName := range models {
		if strings.HasSuffix(modelName, ratio_setting.CompactModelSuffix) && !ratio_setting.IsVirtualCompactModelName(modelName) {
			continue
		}
		if ratio_setting.IsVirtualCompactModelName(modelName) {
			if _, ok := publishedPricing[modelName]; !ok {
				continue
			}
		}
		if _, ok := seen[modelName]; !ok {
			seen[modelName] = struct{}{}
			expanded = append(expanded, modelName)
		}
		if strings.HasSuffix(modelName, ratio_setting.CompactModelSuffix) || !ratio_setting.IsGPTCompactBaseModel(modelName) {
			continue
		}
		if !slices.Contains(model.GetModelSupportEndpointTypes(modelName), constant.EndpointTypeOpenAIResponseCompact) {
			continue
		}
		compactModel := ratio_setting.WithCompactModelSuffix(modelName)
		if _, ok := publishedPricing[compactModel]; !ok {
			continue
		}
		if _, ok := seen[compactModel]; ok {
			continue
		}
		seen[compactModel] = struct{}{}
		expanded = append(expanded, compactModel)
	}
	return expanded
}
