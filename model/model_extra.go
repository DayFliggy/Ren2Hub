package model

// GetModelQuotaTypes 返回指定模型的计费类型集合（来自缓存）
func GetModelQuotaTypes(modelName string) []int {
	GetPricing()

	modelPricingMetadataLock.RLock()
	quota, ok := modelQuotaTypeMap[modelName]
	modelPricingMetadataLock.RUnlock()
	if !ok {
		return []int{}
	}
	return []int{quota}
}
