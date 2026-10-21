package service

import "github.com/shopspring/decimal"

// FormatChannelRatio 把计费实际使用的渠道倍率写成精确十进制字符串，供消费日志使用。
// 渠道倍率允许最多 18 位小数，按 %.2f 截断会让日志与实际扣费对不上；
// 这里取 float64 的最短可还原十进制表示，不用科学计数法。
func FormatChannelRatio(ratio float64) string {
	return decimal.NewFromFloat(ratio).String()
}
