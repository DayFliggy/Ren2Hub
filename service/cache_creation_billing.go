package service

// 缓存创建可能是唯一用量；不能用普通输入输出为零抹掉已算出的缓存费用。
func (s *textQuotaSummary) hasBillableUsage() bool {
	return s.FixedPriceBilling || s.TotalTokens > 0 || cacheWriteTokensTotal(*s) > 0 ||
		(s.IsClaudeUsageSemantic && s.CacheTokens > 0) || !s.ToolCallSurchargeQuota.IsZero()
}

// TTL 字段是总量的拆分，取较大值以补齐缺失总量，不重复累计已有总量。
func cacheWriteTokensTotal(summary textQuotaSummary) int {
	return max(summary.CacheCreationTokens,
		summary.CacheCreationTokens5m+summary.CacheCreationTokens1h+summary.CacheCreationTokens12h)
}
