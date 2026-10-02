package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// TokenSetting 令牌相关配置
type TokenSetting struct {
	MaxUserTokens          int `json:"max_user_tokens"`            // 每用户最大令牌数量
	MaxTokenCreatesPerHour int `json:"max_token_creates_per_hour"` // 0 表示不限制创建频率
}

// 默认配置
var tokenSetting = TokenSetting{
	MaxUserTokens:          1000, // 默认每用户最多 1000 个令牌
	MaxTokenCreatesPerHour: 60,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("token_setting", &tokenSetting)
}

// GetTokenSetting 获取令牌配置
func GetTokenSetting() *TokenSetting {
	return &tokenSetting
}

// GetMaxUserTokens 获取每用户最大令牌数量
func GetMaxUserTokens() int {
	return GetTokenSetting().MaxUserTokens
}
