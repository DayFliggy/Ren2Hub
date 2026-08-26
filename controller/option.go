package controller

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

var completionRatioMetaOptionKeys = []string{
	"ModelPrice",
	"ModelRatio",
	"CompletionRatio",
	"CacheRatio",
	"CreateCacheRatio",
	"ImageRatio",
	"AudioRatio",
	"AudioCompletionRatio",
}

func isPaymentComplianceOptionKey(key string) bool {
	return strings.HasPrefix(key, "payment_setting.compliance_")
}

func isPositiveOptionValue(value string) bool {
	intValue, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil {
		return intValue > 0
	}
	floatValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return err == nil && floatValue > 0
}

func collectModelNamesFromOptionValue(raw string, modelNames map[string]struct{}) {
	if strings.TrimSpace(raw) == "" {
		return
	}

	var parsed map[string]any
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return
	}

	for modelName := range parsed {
		modelNames[modelName] = struct{}{}
	}
}

func buildCompletionRatioMetaValue(optionValues map[string]string) string {
	modelNames := make(map[string]struct{})
	for _, key := range completionRatioMetaOptionKeys {
		collectModelNamesFromOptionValue(optionValues[key], modelNames)
	}

	meta := make(map[string]ratio_setting.CompletionRatioInfo, len(modelNames))
	for modelName := range modelNames {
		meta[modelName] = ratio_setting.GetCompletionRatioInfo(modelName)
	}

	jsonBytes, err := common.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func GetOptions(c *gin.Context) {
	var options []*model.Option
	optionValues := make(map[string]string)
	common.OptionMapRWMutex.Lock()
	for k, v := range common.OptionMap {
		if k == "theme.frontend" {
			continue
		}
		value := common.Interface2String(v)
		if isSensitiveOptionKey(k) {
			continue
		}
		options = append(options, &model.Option{
			Key:   k,
			Value: value,
		})
		for _, optionKey := range completionRatioMetaOptionKeys {
			if optionKey == k {
				optionValues[k] = value
				break
			}
		}
	}
	common.OptionMapRWMutex.Unlock()
	options = append(options, &model.Option{
		Key:   "CompletionRatioMeta",
		Value: buildCompletionRatioMetaValue(optionValues),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    options,
	})
}

func isSensitiveOptionKey(key string) bool {
	return strings.HasSuffix(key, "Token") ||
		strings.HasSuffix(key, "Secret") ||
		strings.HasSuffix(key, "Key") ||
		strings.HasSuffix(key, "Cert") ||
		strings.HasSuffix(key, "Certificate") ||
		strings.HasSuffix(key, "Password") ||
		strings.HasSuffix(key, "secret") ||
		strings.HasSuffix(key, "api_key")
}

// GetOptionSecretStatus returns configured secret key names only. It never
// returns the values, which keeps the existing option-read boundary intact.
func GetOptionSecretStatus(c *gin.Context) {
	configured := make([]string, 0)
	common.OptionMapRWMutex.RLock()
	for key, value := range common.OptionMap {
		if isSensitiveOptionKey(key) && strings.TrimSpace(value) != "" {
			configured = append(configured, key)
		}
	}
	common.OptionMapRWMutex.RUnlock()
	sort.Strings(configured)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"configured": configured,
		},
	})
}

type OptionUpdateRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type OptionBulkUpdateRequest struct {
	Options map[string]any `json:"options"`
}

type OptionDescriptor struct {
	Key          string `json:"key"`
	ValueType    string `json:"value_type"`
	DefaultValue any    `json:"default_value"`
	Editor       string `json:"editor"`
	Sensitive    bool   `json:"sensitive"`
	Editable     bool   `json:"editable"`
	Validator    string `json:"validator,omitempty"`
}

// optionDefaults is deliberately independent from the current OptionMap. The
// catalog describes the operator contract, not the value currently persisted
// in the database. Unknown legacy/plugin options still fall back to a neutral
// value and remain visible to clients through rawOptions.
var optionDefaults = map[string]any{
	"PasswordLoginEnabled": true, "PasswordRegisterEnabled": true, "RegisterEnabled": true,
	"DataExportEnabled": true, "DataExportInterval": 5, "DataExportDefaultTime": "hour",
	"PreConsumedQuota": 500000, "QuotaPerUnit": 500000, "USDExchangeRate": 1.0,
	"Price": 7.3, "MinTopUp": 1, "ChannelDisableThreshold": 5,
	"monitor_setting.channel_test_mode":              "scheduled_all",
	"auto_pricing.models_dev_url":                    "https://models.dev/api.json",
	"billing_setting.billing_mode":                   map[string]any{},
	"billing_setting.billing_expr":                   map[string]any{},
	"group_ratio_setting.group_special_usable_group": map[string]any{},
	"payment_setting.amount_options":                 []any{},
	"payment_setting.amount_discount":                map[string]any{},
	"global.thinking_model_blacklist":                []any{},
	"global.chat_completions_to_responses_policy":    map[string]any{},
	"gemini.version_settings":                        map[string]any{},
	"gemini.supported_imagine_models":                []any{},
	"claude.model_headers_settings":                  map[string]any{},
	"qwen.sync_image_models":                         []any{},
	"channel_affinity_setting.rules":                 []any{},
}

var optionValidators = map[string]string{
	"billing_setting.billing_mode":      "billing-mode",
	"billing_setting.billing_expr":      "billing-expression",
	"payment_setting.amount_options":    "positive-amount-list",
	"payment_setting.amount_discount":   "amount-discount",
	"channel_affinity_setting.rules":    "channel-affinity-rules",
	"monitor_setting.channel_test_mode": "channel-test-mode",
}

func optionDescriptorFor(key, value string) OptionDescriptor {
	descriptor := OptionDescriptor{
		Key:          key,
		ValueType:    "string",
		DefaultValue: value,
		Editor:       "text",
		Sensitive:    isSensitiveOptionKey(key),
		Editable:     key != "CompletionRatioMeta" && !isPaymentComplianceOptionKey(key),
		Validator:    optionValidators[key],
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "true" || trimmed == "false" {
		descriptor.ValueType = "boolean"
		descriptor.DefaultValue = trimmed == "true"
		descriptor.Editor = "toggle"
	} else if number, err := strconv.ParseFloat(trimmed, 64); trimmed != "" && err == nil {
		descriptor.ValueType = "number"
		descriptor.DefaultValue = number
		descriptor.Editor = "number"
	} else if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		descriptor.ValueType = "json"
		descriptor.Editor = "json"
		var decoded any
		if common.UnmarshalJsonStr(trimmed, &decoded) == nil {
			descriptor.DefaultValue = decoded
		}
	}
	if descriptor.Sensitive {
		descriptor.Editor = "secret"
		descriptor.DefaultValue = ""
	}
	if defaultValue, ok := optionDefaults[key]; ok {
		descriptor.DefaultValue = defaultValue
	}
	switch {
	case key == "payment_setting.amount_options":
		descriptor.Editor = "amount-list"
	case key == "payment_setting.amount_discount":
		descriptor.Editor = "discount"
	case key == "billing_setting.billing_mode" || key == "billing_setting.billing_expr":
		descriptor.Editor = "billing-expression"
	case key == "global.chat_completions_to_responses_policy":
		descriptor.Editor = "conversion-policy"
	case key == "channel_affinity_setting.rules":
		descriptor.Editor = "channel-affinity-rules"
	case strings.Contains(strings.ToLower(key), "ratio") || strings.Contains(strings.ToLower(key), "price"):
		descriptor.Editor = "ratio"
	case strings.Contains(strings.ToLower(key), "url") || strings.Contains(strings.ToLower(key), "address"):
		descriptor.Editor = "url"
	case key == "FileUploadPermission" || key == "FileDownloadPermission" || key == "ImageUploadPermission" || key == "ImageDownloadPermission":
		descriptor.Editor = "role"
	}
	return descriptor
}

// GetOptionCatalog returns metadata for every persisted option. Sensitive
// defaults are intentionally blank; their configured state is available from
// /api/option/secret-status.
func GetOptionCatalog(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	keys := make([]string, 0, len(common.OptionMap))
	values := make(map[string]string, len(common.OptionMap))
	for key, value := range common.OptionMap {
		if key == "theme.frontend" {
			continue
		}
		keys = append(keys, key)
		values[key] = common.Interface2String(value)
	}
	common.OptionMapRWMutex.RUnlock()
	sort.Strings(keys)
	descriptors := make([]OptionDescriptor, 0, len(keys))
	for _, key := range keys {
		descriptors = append(descriptors, optionDescriptorFor(key, values[key]))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": descriptors})
}

func normalizeOptionValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		return common.Interface2String(typed), nil
	case float64:
		return common.Interface2String(typed), nil
	case int:
		return common.Interface2String(typed), nil
	default:
		return "", fmt.Errorf("配置项值只能是字符串、数值或布尔值")
	}
}

func optionSnapshot(overrides map[string]string) map[string]string {
	snapshot := make(map[string]string, len(common.OptionMap)+len(overrides))
	common.OptionMapRWMutex.RLock()
	for key, value := range common.OptionMap {
		snapshot[key] = value
	}
	common.OptionMapRWMutex.RUnlock()
	for key, value := range overrides {
		snapshot[key] = value
	}
	return snapshot
}

func optionValuePresent(snapshot map[string]string, key string) bool {
	return strings.TrimSpace(snapshot[key]) != ""
}

func optionListPresent(snapshot map[string]string, key string) bool {
	return len(strings.FieldsFunc(snapshot[key], func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})) > 0
}

func validateJSON(value string) error {
	var decoded any
	return common.UnmarshalJsonStr(value, &decoded)
}

func validateRatioMap(value string) error {
	var ratios map[string]float64
	return common.UnmarshalJsonStr(value, &ratios)
}

func validateNestedRatioMap(value string) error {
	var ratios map[string]map[string]float64
	return common.UnmarshalJsonStr(value, &ratios)
}

func validateStringMap(value string, name string) error {
	var values map[string]string
	if err := common.UnmarshalJsonStr(value, &values); err != nil || values == nil {
		if err == nil {
			err = fmt.Errorf("必须是 JSON 对象")
		}
		return fmt.Errorf("%s%s", name, ": "+err.Error())
	}
	return nil
}

func validateStringList(value string, name string) error {
	var values []string
	if err := common.UnmarshalJsonStr(value, &values); err != nil || values == nil {
		if err == nil {
			err = fmt.Errorf("必须是 JSON 数组")
		}
		return fmt.Errorf("%s%s", name, ": "+err.Error())
	}
	for _, item := range values {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("%s: 不允许空字符串", name)
		}
	}
	return nil
}

func validatePositiveIntList(value string, name string) error {
	var values []int
	if err := common.UnmarshalJsonStr(value, &values); err != nil || values == nil {
		if err == nil {
			err = fmt.Errorf("必须是 JSON 数组")
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	seen := make(map[int]struct{}, len(values))
	for _, item := range values {
		if item <= 0 {
			return fmt.Errorf("%s: 金额必须为正整数", name)
		}
		if _, ok := seen[item]; ok {
			return fmt.Errorf("%s: 金额不能重复", name)
		}
		seen[item] = struct{}{}
	}
	return nil
}

func validateDiscountMap(value string) error {
	var values map[string]float64
	if err := common.UnmarshalJsonStr(value, &values); err != nil || values == nil {
		if err == nil {
			err = fmt.Errorf("必须是 JSON 对象")
		}
		return fmt.Errorf("充值折扣: %w", err)
	}
	for amount, discount := range values {
		parsed, err := strconv.Atoi(amount)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("充值折扣金额必须为正整数: %s", amount)
		}
		if discount <= 0 || discount > 1 {
			return fmt.Errorf("充值折扣必须大于 0 且不超过 1: %s", amount)
		}
	}
	return nil
}

func validateChannelAffinityRules(value string) error {
	var rules []operation_setting.ChannelAffinityRule
	if err := common.UnmarshalJsonStr(value, &rules); err != nil || rules == nil {
		if err == nil {
			err = fmt.Errorf("必须是 JSON 数组")
		}
		return fmt.Errorf("渠道亲和性规则: %w", err)
	}
	for _, rule := range rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("渠道亲和性规则名称不能为空")
		}
		if rule.TTLSeconds < 0 {
			return fmt.Errorf("渠道亲和性规则 TTL 不能为负数")
		}
		for _, pattern := range append(append([]string{}, rule.ModelRegex...), rule.PathRegex...) {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("渠道亲和性规则正则无效: %w", err)
			}
		}
	}
	return nil
}

func validateChannelAffinitySettingValue(key, value string) error {
	switch key {
	case "channel_affinity_setting.max_entries":
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err != nil || n <= 0 {
			return fmt.Errorf("渠道亲和性容量必须为正整数")
		}
	case "channel_affinity_setting.default_ttl_seconds":
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err != nil || n < 0 {
			return fmt.Errorf("渠道亲和性 TTL 不能为负数")
		}
	case "channel_affinity_setting.rules":
		return validateChannelAffinityRules(value)
	}
	return nil
}

func validateBillingSettingValue(key, value string) error {
	if key == "billing_setting.billing_mode" {
		var modes map[string]string
		if err := common.UnmarshalJsonStr(value, &modes); err != nil || modes == nil {
			return fmt.Errorf("计费模式必须是 JSON 对象")
		}
		for modelName, mode := range modes {
			if mode != billing_setting.BillingModeRatio && mode != billing_setting.BillingModeTieredExpr {
				return fmt.Errorf("模型 %q 的计费模式无效", modelName)
			}
		}
		return nil
	}
	var expressions map[string]string
	if err := common.UnmarshalJsonStr(value, &expressions); err != nil || expressions == nil {
		return fmt.Errorf("计费表达式必须是 JSON 对象")
	}
	for modelName, expression := range expressions {
		if strings.TrimSpace(expression) == "" {
			return fmt.Errorf("模型 %q 的计费表达式不能为空", modelName)
		}
		if err := billing_setting.SmokeTestExpr(expression); err != nil {
			return fmt.Errorf("模型 %q 的计费表达式无效: %w", modelName, err)
		}
	}
	return nil
}

// validateOptionPatch is intentionally free of persistence and runtime side
// effects so a full patch can be checked before model.UpdateOptionsBulk opens
// its transaction. The current option map plus the patch forms the final state.
func validateOptionPatch(values map[string]string) error {
	snapshot := optionSnapshot(values)
	for key, value := range values {
		if key == "" {
			return fmt.Errorf("配置项名称不能为空")
		}
		if isPaymentComplianceOptionKey(key) {
			return fmt.Errorf("合规确认字段不允许通过通用设置接口修改")
		}
		if (key == "QuotaForInviter" || key == "QuotaForInvitee") && isPositiveOptionValue(value) && !operation_setting.IsPaymentComplianceConfirmed() {
			return fmt.Errorf("支付合规确认后才可设置邀请奖励")
		}

		switch key {
		case "GitHubOAuthEnabled":
			if value == "true" && (!optionValuePresent(snapshot, "GitHubClientId") || !optionValuePresent(snapshot, "GitHubClientSecret")) {
				return fmt.Errorf("无法启用 GitHub OAuth，请先填入 GitHub Client Id 以及 GitHub Client Secret！")
			}
		case "discord.enabled":
			if value == "true" && (!optionValuePresent(snapshot, "discord.client_id") || !optionValuePresent(snapshot, "discord.client_secret")) {
				return fmt.Errorf("无法启用 Discord OAuth，请先填入 Discord Client Id 以及 Discord Client Secret！")
			}
		case "oidc.enabled":
			if value == "true" && (!optionValuePresent(snapshot, "oidc.client_id") || !optionValuePresent(snapshot, "oidc.client_secret")) {
				return fmt.Errorf("无法启用 OIDC 登录，请先填入 OIDC Client Id 以及 OIDC Client Secret！")
			}
		case "LinuxDOOAuthEnabled":
			if value == "true" && (!optionValuePresent(snapshot, "LinuxDOClientId") || !optionValuePresent(snapshot, "LinuxDOClientSecret")) {
				return fmt.Errorf("无法启用 LinuxDO OAuth，请先填入 LinuxDO Client Id 以及 LinuxDO Client Secret！")
			}
		case "EmailDomainRestrictionEnabled":
			if value == "true" && !optionListPresent(snapshot, "EmailDomainWhitelist") {
				return fmt.Errorf("无法启用邮箱域名限制，请先填入限制的邮箱域名！")
			}
		case "WeChatAuthEnabled":
			if value == "true" && !optionValuePresent(snapshot, "WeChatServerAddress") {
				return fmt.Errorf("无法启用微信登录，请先填入微信登录相关配置信息！")
			}
		case "TurnstileCheckEnabled":
			if value == "true" && (!optionValuePresent(snapshot, "TurnstileSiteKey") || !optionValuePresent(snapshot, "TurnstileSecretKey")) {
				return fmt.Errorf("无法启用 Turnstile 校验，请先填入 Turnstile 校验相关配置信息！")
			}
		case "TelegramOAuthEnabled":
			if value == "true" && !optionValuePresent(snapshot, "TelegramBotToken") {
				return fmt.Errorf("无法启用 Telegram OAuth，请先填入 Telegram Bot Token！")
			}
		case "theme.frontend":
			if value != "default" {
				return fmt.Errorf("Classic 前端已移除，主题只能设置为 default")
			}
		case "GroupRatio":
			if err := ratio_setting.CheckGroupRatio(value); err != nil {
				return err
			}
		case "GroupGroupRatio":
			if err := validateNestedRatioMap(value); err != nil {
				return err
			}
		case "group_ratio_setting.group_special_usable_group":
			if err := validateJSON(value); err != nil {
				return fmt.Errorf("特殊可用分组配置无效: %w", err)
			}
		case "billing_setting.billing_mode", "billing_setting.billing_expr":
			if err := validateBillingSettingValue(key, value); err != nil {
				return err
			}
		case "payment_setting.amount_options":
			if err := validatePositiveIntList(value, "充值金额选项"); err != nil {
				return err
			}
		case "payment_setting.amount_discount":
			if err := validateDiscountMap(value); err != nil {
				return err
			}
		case "global.thinking_model_blacklist", "gemini.supported_imagine_models", "qwen.sync_image_models":
			if err := validateStringList(value, key); err != nil {
				return err
			}
		case "global.chat_completions_to_responses_policy":
			if err := validateJSON(value); err != nil {
				return fmt.Errorf("Chat Completions 转 Responses 策略无效: %w", err)
			}
		case "gemini.version_settings", "claude.model_headers_settings":
			if err := validateJSON(value); err != nil {
				return fmt.Errorf("模型高级配置无效: %w", err)
			}
		case "gemini.thinking_adapter_budget_tokens_percentage", "claude.thinking_adapter_budget_tokens_percentage":
			percentage, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || percentage < 0.1 || percentage > 1 {
				return fmt.Errorf("Token 预算比例必须在 0.1 到 1 之间")
			}
		case "grok.violation_deduction_amount":
			amount, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || amount < 0 {
				return fmt.Errorf("Grok 违规扣费必须为非负数字")
			}
		case "channel_affinity_setting.max_entries", "channel_affinity_setting.default_ttl_seconds", "channel_affinity_setting.rules":
			if err := validateChannelAffinitySettingValue(key, value); err != nil {
				return err
			}
		case "monitor_setting.channel_test_mode":
			if value != operation_setting.ChannelTestModeScheduledAll && value != operation_setting.ChannelTestModeAutoBanOnly && value != operation_setting.ChannelTestModePassiveRecovery {
				return fmt.Errorf("自动测试策略无效")
			}
		case "FileUploadPermission", "FileDownloadPermission", "ImageUploadPermission", "ImageDownloadPermission":
			role, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || !common.IsValidateRole(role) {
				return fmt.Errorf("文件访问权限角色无效")
			}
		case "ModelRatio", "ModelPrice", "CompletionRatio", "CacheRatio", "CreateCacheRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio", "TopupGroupRatio":
			if err := validateRatioMap(value); err != nil {
				return err
			}
		case "gemini.safety_settings":
			if err := model_setting.ValidateGeminiSafetySettings(value); err != nil {
				return err
			}
		case "claude.default_max_tokens":
			if err := model_setting.ValidateClaudeDefaultMaxTokens(value); err != nil {
				return err
			}
		case operation_setting.ToolPriceOptionKey:
			if err := operation_setting.ValidateToolPricesJSON(value); err != nil {
				return err
			}
		case "ModelRequestRateLimitGroup":
			if err := setting.CheckModelRequestRateLimitGroup(value); err != nil {
				return err
			}
		case "AutomaticDisableStatusCodes", "AutomaticRetryStatusCodes":
			if _, err := operation_setting.ParseHTTPStatusCodeRanges(value); err != nil {
				return err
			}
		case "console_setting.api_info":
			if err := console_setting.ValidateConsoleSettings(value, "ApiInfo"); err != nil {
				return err
			}
		case "console_setting.announcements":
			if err := console_setting.ValidateConsoleSettings(value, "Announcements"); err != nil {
				return err
			}
		case "console_setting.faq":
			if err := console_setting.ValidateConsoleSettings(value, "FAQ"); err != nil {
				return err
			}
		case "console_setting.uptime_kuma_groups":
			if err := console_setting.ValidateConsoleSettings(value, "UptimeKumaGroups"); err != nil {
				return err
			}
		case "Chats", "AutoGroups", "UserUsableGroups", "PayMethods", "WaffoPayMethods":
			if err := validateJSON(value); err != nil {
				return err
			}
		}
	}
	oidcEndpointKeys := []string{
		"oidc.authorization_endpoint",
		"oidc.token_endpoint",
		"oidc.user_info_endpoint",
	}
	if snapshot["oidc.enabled"] == "true" {
		for _, key := range oidcEndpointKeys {
			if !optionValuePresent(snapshot, key) {
				return fmt.Errorf("无法启用 OIDC 登录，请先填入完整的 OIDC 端点配置")
			}
		}
	}
	for _, key := range append(oidcEndpointKeys, "oidc.well_known") {
		_, changed := values[key]
		if snapshot["oidc.enabled"] != "true" && !changed {
			continue
		}
		value := strings.TrimSpace(snapshot[key])
		if value == "" {
			continue
		}
		if err := oauth.ValidateOAuthEndpoint(value); err != nil {
			return fmt.Errorf("OIDC 端点必须使用可公开访问的 HTTPS 地址")
		}
	}
	return nil
}

func writeOptionValidationError(c *gin.Context, err error) {
	c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
}

func UpdateOption(c *gin.Context) {
	var option OptionUpdateRequest
	err := common.DecodeJson(c.Request.Body, &option)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	normalizedValue, err := normalizeOptionValue(option.Value)
	if err != nil {
		writeOptionValidationError(c, err)
		return
	}
	option.Value = normalizedValue
	if err = validateOptionPatch(map[string]string{option.Key: normalizedValue}); err != nil {
		writeOptionValidationError(c, err)
		return
	}
	switch option.Key {
	case "QuotaForInviter", "QuotaForInvitee", "AffiliateEnabled":
		if isPositiveOptionValue(option.Value.(string)) && !operation_setting.IsPaymentComplianceConfirmed() {
			common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
			return
		}
	case "AffiliateActivatedAt":
		common.ApiErrorMsg(c, "邀请返利激活时间不允许直接修改")
		return
	default:
		if isPaymentComplianceOptionKey(option.Key) {
			common.ApiErrorMsg(c, "合规确认字段不允许通过通用设置接口修改")
			return
		}
	}
	switch option.Key {
	case "AffiliateEnabled":
		enabled, parseErr := strconv.ParseBool(option.Value.(string))
		if parseErr != nil {
			common.ApiErrorMsg(c, "邀请返利开关参数无效")
			return
		}
		if err := model.SetAffiliateProgramEnabled(enabled); err != nil {
			common.ApiError(c, err)
			return
		}
		recordManageAudit(c, "option.update", map[string]interface{}{"key": option.Key})
		common.ApiSuccess(c, nil)
		return
	case "AffiliateRegistrationRequired":
		if option.Value == "true" {
			if common.AffiliateActivatedAt <= 0 {
				common.ApiErrorMsg(c, "请先激活邀请返利计划")
				return
			}
			hasSeed, seedErr := model.HasActiveAffiliateSeed()
			if seedErr != nil {
				common.ApiError(c, seedErr)
				return
			}
			if !hasSeed {
				common.ApiErrorMsg(c, "至少需要一个有效的管理员或根用户邀请码")
				return
			}
		}
	case "AffiliateRebateRateBps":
		value, parseErr := strconv.Atoi(option.Value.(string))
		if parseErr != nil || value < 0 || value > 10000 {
			common.ApiErrorMsg(c, "返利比例必须在 0 到 10000 基点之间")
			return
		}
	case "AffiliateFreezeHours", "AffiliateDurationDays":
		value, parseErr := strconv.Atoi(option.Value.(string))
		if parseErr != nil || value < 0 {
			common.ApiErrorMsg(c, "邀请返利周期不能为负数")
			return
		}
	case "AffiliatePerInviteeCap":
		value, parseErr := strconv.ParseFloat(option.Value.(string), 64)
		if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			common.ApiErrorMsg(c, "单个受邀用户返利上限必须是非负有限数值")
			return
		}
	case "QuotaForInviter":
		if common.AffiliateActivatedAt > 0 && isPositiveOptionValue(option.Value.(string)) {
			common.ApiErrorMsg(c, "邀请返利计划激活后固定邀请人奖励已废弃")
			return
		}
	case "GitHubOAuthEnabled":
		if option.Value == "true" && common.GitHubClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 GitHub OAuth，请先填入 GitHub Client Id 以及 GitHub Client Secret！",
			})
			return
		}
	case "discord.enabled":
		if option.Value == "true" && system_setting.GetDiscordSettings().ClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 Discord OAuth，请先填入 Discord Client Id 以及 Discord Client Secret！",
			})
			return
		}
	case "oidc.enabled":
		if option.Value == "true" && system_setting.GetOIDCSettings().ClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 OIDC 登录，请先填入 OIDC Client Id 以及 OIDC Client Secret！",
			})
			return
		}
	case "LinuxDOOAuthEnabled":
		if option.Value == "true" && common.LinuxDOClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 LinuxDO OAuth，请先填入 LinuxDO Client Id 以及 LinuxDO Client Secret！",
			})
			return
		}
	case "EmailDomainRestrictionEnabled":
		if option.Value == "true" && len(common.EmailDomainWhitelist) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用邮箱域名限制，请先填入限制的邮箱域名！",
			})
			return
		}
	case "WeChatAuthEnabled":
		if option.Value == "true" && common.WeChatServerAddress == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用微信登录，请先填入微信登录相关配置信息！",
			})
			return
		}
	case "TurnstileCheckEnabled":
		if option.Value == "true" && common.TurnstileSiteKey == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 Turnstile 校验，请先填入 Turnstile 校验相关配置信息！",
			})

			return
		}
	case "TelegramOAuthEnabled":
		if option.Value == "true" && common.TelegramBotToken == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 Telegram OAuth，请先填入 Telegram Bot Token！",
			})
			return
		}
	case "theme.frontend":
		if option.Value != "default" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Classic 前端已移除，主题只能设置为 default",
			})
			return
		}
	case "GroupRatio":
		err = ratio_setting.CheckGroupRatio(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "gemini.safety_settings":
		err = model_setting.ValidateGeminiSafetySettings(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "claude.default_max_tokens":
		err = model_setting.ValidateClaudeDefaultMaxTokens(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case operation_setting.ToolPriceOptionKey:
		err = operation_setting.ValidateToolPricesJSON(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "图片倍率设置失败: " + err.Error(),
			})
			return
		}
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "音频倍率设置失败: " + err.Error(),
			})
			return
		}
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "音频补全倍率设置失败: " + err.Error(),
			})
			return
		}
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "缓存创建倍率设置失败: " + err.Error(),
			})
			return
		}
	case "ModelRequestRateLimitGroup":
		err = setting.CheckModelRequestRateLimitGroup(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticDisableStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticRetryStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.api_info":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "ApiInfo")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.announcements":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "Announcements")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.faq":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "FAQ")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.uptime_kuma_groups":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "UptimeKumaGroups")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	err = model.UpdateOption(option.Key, option.Value.(string))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 出于安全考虑只记录被修改的配置项名称，不记录配置值（可能含密钥等敏感信息）。
	recordManageAudit(c, "option.update", map[string]interface{}{
		"key": option.Key,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// UpdateOptionsBulk validates a patch against its final configuration state
// before using the model-layer transaction to persist all values together.
func UpdateOptionsBulk(c *gin.Context) {
	var request OptionBulkUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Options) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}

	values := make(map[string]string, len(request.Options))
	for key, value := range request.Options {
		normalizedValue, err := normalizeOptionValue(value)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		values[key] = normalizedValue
	}
	if err := validateOptionPatch(values); err != nil {
		writeOptionValidationError(c, err)
		return
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	recordManageAudit(c, "option.bulk_update", map[string]interface{}{
		"keys": keys,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    gin.H{"keys": keys},
	})
}
