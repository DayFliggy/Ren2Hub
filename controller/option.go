package controller

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

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

func GetOptions(c *gin.Context) {
	var options []*model.Option
	common.OptionMapRWMutex.Lock()
	for k, v := range common.OptionMap {
		if k == "theme.frontend" || model.IsRetiredBillingOption(k) {
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

	}
	common.OptionMapRWMutex.Unlock()

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
	"PayAddress": "", "CustomCallbackAddress": "", "EpayId": "", "EpayKey": "",
	"GmpayPayAddress": "", "GmpayId": "", "GmpayKey": "",
	"GmpayPrice": 1.0, "GmpayMinTopUp": 1, "GmpayAssets": []any{},
	"GmpayGroupRatio": map[string]any{"default": 1},
	"TopupGroupRatio": map[string]any{"default": 1, "vip": 1, "svip": 1},
	"PayMethods": []map[string]string{
		{"name": "支付宝", "type": "alipay", "icon": "SiAlipay"},
		{"name": "微信", "type": "wxpay", "icon": "SiWechat"},
		{"name": "自定义1", "type": "custom1", "icon": "LuCreditCard", "min_topup": "50"},
	},
	"monitor_setting.channel_test_mode":           "scheduled_all",
	"monitor_setting.channel_test_concurrency":    "1",
	"payment_setting.amount_options":              []int{10, 20, 50, 100, 200, 500},
	"payment_setting.amount_discount":             map[string]any{},
	"payment_setting.epay_settlement_enabled":     true,
	"payment_setting.gmpay_settlement_enabled":    true,
	"global.thinking_model_blacklist":             []any{},
	"global.chat_completions_to_responses_policy": map[string]any{},
	"gemini.version_settings":                     map[string]any{},
	"gemini.supported_imagine_models":             []any{},
	"claude.model_headers_settings":               map[string]any{},
	"qwen.sync_image_models":                      []any{},
	"channel_affinity_setting.rules":              []any{},
}

var optionValidators = map[string]string{
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
		Editable:     key != "QuotaPerUnit" && !model.IsRetiredBillingOption(key) && !isPaymentComplianceOptionKey(key),
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
	switch key {
	// Payment contracts must not change with the saved value. In particular,
	// numeric-looking merchant IDs are opaque strings, never JSON numbers.
	case "EpayId", "GmpayId":
		descriptor.ValueType, descriptor.Editor = "string", "text"
	case "EpayKey", "GmpayKey":
		descriptor.ValueType, descriptor.Editor = "string", "secret"
	case "PayAddress", "GmpayPayAddress", "CustomCallbackAddress":
		descriptor.ValueType, descriptor.Editor = "string", "url"
	case "Price", "GmpayPrice":
		descriptor.ValueType, descriptor.Editor = "number", "ratio"
	case "MinTopUp", "GmpayMinTopUp":
		descriptor.ValueType, descriptor.Editor = "number", "number"
	case "PayMethods", "GmpayAssets":
		descriptor.ValueType, descriptor.Editor = "json", "json"
	case "TopupGroupRatio", "GmpayGroupRatio":
		descriptor.ValueType, descriptor.Editor = "json", "ratio"
	case "payment_setting.amount_options", "payment_setting.amount_discount":
		descriptor.ValueType = "json"
	case "payment_setting.epay_settlement_enabled", "payment_setting.gmpay_settlement_enabled":
		descriptor.ValueType, descriptor.Editor = "boolean", "toggle"
	case "QuotaForNewUser", "QuotaForInviter", "QuotaForInvitee", "QuotaRemindThreshold", "AffiliatePerInviteeCap":
		descriptor.ValueType, descriptor.Editor, descriptor.DefaultValue = "string", "text", value
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
		if key == "theme.frontend" || model.IsRetiredBillingOption(key) {
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
		return "", common.NewMessage("Option values must be strings, numbers, or booleans")
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

func validateStringList(value string, name string) error {
	var values []string
	if err := common.UnmarshalJsonStr(value, &values); err != nil {
		return common.NewMessage("Invalid {{name}}: {{error}}", map[string]any{"name": name, "error": err.Error()})
	}
	if values == nil {
		return common.NewMessage("{{name}} must be a JSON array", map[string]any{"name": name})
	}
	for _, item := range values {
		if strings.TrimSpace(item) == "" {
			return common.NewMessage("{{name}} cannot contain empty strings", map[string]any{"name": name})
		}
	}
	return nil
}

func validateAmountOptions(value string) error {
	var values []int
	if err := common.UnmarshalJsonStr(value, &values); err != nil {
		return common.NewMessage("Invalid top-up amount options: {{error}}", map[string]any{"error": err.Error()})
	}
	if values == nil {
		return common.NewMessage("Top-up amount options must be a JSON array")
	}
	seen := make(map[int]struct{}, len(values))
	for _, item := range values {
		if item <= 0 {
			return common.NewMessage("Top-up amount options must be positive integers")
		}
		if _, ok := seen[item]; ok {
			return common.NewMessage("Top-up amount options cannot repeat")
		}
		seen[item] = struct{}{}
	}
	return nil
}

func validateDiscountMap(value string) error {
	var values map[string]float64
	if err := common.UnmarshalJsonStr(value, &values); err != nil {
		return common.NewMessage("Invalid top-up discounts: {{error}}", map[string]any{"error": err.Error()})
	}
	if values == nil {
		return common.NewMessage("Top-up discounts must be a JSON object")
	}
	for amount, discount := range values {
		parsed, err := strconv.Atoi(amount)
		if err != nil || parsed <= 0 {
			return common.NewMessage("Top-up discount amounts must be positive integers: {{amount}}", map[string]any{"amount": amount})
		}
		if discount <= 0 || discount > 1 {
			return common.NewMessage("Top-up discounts must be greater than 0 and at most 1: {{amount}}", map[string]any{"amount": amount})
		}
	}
	return nil
}

func validateChannelAffinityRules(value string) error {
	var rules []operation_setting.ChannelAffinityRule
	if err := common.UnmarshalJsonStr(value, &rules); err != nil {
		return common.NewMessage("Invalid channel affinity rules: {{error}}", map[string]any{"error": err.Error()})
	}
	if rules == nil {
		return common.NewMessage("Channel affinity rules must be a JSON array")
	}
	for _, rule := range rules {
		if strings.TrimSpace(rule.Name) == "" {
			return common.NewMessage("Channel affinity rule names cannot be empty")
		}
		if rule.TTLSeconds < 0 {
			return common.NewMessage("Channel affinity rule TTL cannot be negative")
		}
		for _, pattern := range append(append([]string{}, rule.ModelRegex...), rule.PathRegex...) {
			if _, err := regexp.Compile(pattern); err != nil {
				return common.NewMessage("Invalid channel affinity rule regex: {{error}}", map[string]any{"error": err.Error()})
			}
		}
	}
	return nil
}

func validateChannelAffinitySettingValue(key, value string) error {
	switch key {
	case "channel_affinity_setting.max_entries":
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err != nil || n <= 0 {
			return common.NewMessage("Channel affinity capacity must be a positive integer")
		}
	case "channel_affinity_setting.default_ttl_seconds":
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err != nil || n < 0 {
			return common.NewMessage("Channel affinity TTL cannot be negative")
		}
	case "channel_affinity_setting.rules":
		return validateChannelAffinityRules(value)
	}
	return nil
}

// validateOptionPatch is intentionally free of persistence and runtime side
// effects so a full patch can be checked before model.UpdateOptionsBulk opens
// its transaction. The current option map plus the patch forms the final state.
func validateOptionPatch(values map[string]string) error {
	for key := range values {
		if model.IsRetiredBillingOption(key) {
			return common.NewMessage("automatic price publication is no longer supported")
		}
		if key == "QuotaPerUnit" {
			return common.NewMessage("quota conversion is fixed at 500000 per USD")
		}
	}
	snapshot := optionSnapshot(values)
	for key, value := range values {
		if key == operation_setting.ToolPriceOptionKey {
			if err := operation_setting.ValidateToolPricesJSON(value); err != nil {
				return err
			}
		}
		if err := operation_setting.ValidateQuotaOption(key, value); err != nil {
			return err
		}
		if key == "" {
			return common.NewMessage("Option name cannot be empty")
		}
		if isPaymentComplianceOptionKey(key) {
			return common.NewMessage("Compliance confirmation fields cannot be changed through the general settings API")
		}
		if (key == "QuotaForInviter" || key == "QuotaForInvitee") && isPositiveOptionValue(value) && !operation_setting.IsPaymentComplianceConfirmed() {
			return common.NewMessage("Invite rewards can be set only after payment compliance is confirmed")
		}

		switch key {
		case "GitHubOAuthEnabled":
			if value == "true" && (!optionValuePresent(snapshot, "GitHubClientId") || !optionValuePresent(snapshot, "GitHubClientSecret")) {
				return common.NewMessage("Cannot enable {{provider}} OAuth. Enter the {{provider}} Client ID and Client Secret first.", map[string]any{"provider": "GitHub"})
			}
		case "discord.enabled":
			if value == "true" && (!optionValuePresent(snapshot, "discord.client_id") || !optionValuePresent(snapshot, "discord.client_secret")) {
				return common.NewMessage("Cannot enable {{provider}} OAuth. Enter the {{provider}} Client ID and Client Secret first.", map[string]any{"provider": "Discord"})
			}
		case "oidc.enabled":
			if value == "true" && (!optionValuePresent(snapshot, "oidc.client_id") || !optionValuePresent(snapshot, "oidc.client_secret")) {
				return common.NewMessage("Cannot enable OIDC login. Enter the OIDC Client ID and Client Secret first.")
			}
		case "LinuxDOOAuthEnabled":
			if value == "true" && (!optionValuePresent(snapshot, "LinuxDOClientId") || !optionValuePresent(snapshot, "LinuxDOClientSecret")) {
				return common.NewMessage("Cannot enable {{provider}} OAuth. Enter the {{provider}} Client ID and Client Secret first.", map[string]any{"provider": "LinuxDO"})
			}
		case "EmailDomainRestrictionEnabled":
			if value == "true" && !optionListPresent(snapshot, "EmailDomainWhitelist") {
				return common.NewMessage("Cannot enable the email domain restriction. Enter the allowed email domains first.")
			}
		case "WeChatAuthEnabled":
			if value == "true" && !optionValuePresent(snapshot, "WeChatServerAddress") {
				return common.NewMessage("Cannot enable WeChat login. Enter the WeChat login settings first.")
			}
		case "TurnstileCheckEnabled":
			if value == "true" && (!optionValuePresent(snapshot, "TurnstileSiteKey") || !optionValuePresent(snapshot, "TurnstileSecretKey")) {
				return common.NewMessage("Cannot enable Turnstile verification. Enter the Turnstile settings first.")
			}
		case "TelegramOAuthEnabled":
			if value == "true" && !optionValuePresent(snapshot, "TelegramBotToken") {
				return common.NewMessage("Cannot enable Telegram OAuth. Enter the Telegram Bot Token first.")
			}
		case "theme.frontend":
			if value != "default" {
				return common.NewMessage("The Classic frontend has been removed. The theme can only be set to default")
			}
		case "GroupRatio":
			if err := ratio_setting.CheckGroupRatio(value); err != nil {
				return err
			}
		case "payment_setting.amount_options":
			if err := validateAmountOptions(value); err != nil {
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
				return common.NewMessage("Invalid Chat Completions to Responses policy: {{error}}", map[string]any{"error": err.Error()})
			}
		case "gemini.version_settings", "claude.model_headers_settings":
			if err := validateJSON(value); err != nil {
				return common.NewMessage("Invalid advanced model settings: {{error}}", map[string]any{"error": err.Error()})
			}
		case "gemini.thinking_adapter_budget_tokens_percentage", "claude.thinking_adapter_budget_tokens_percentage":
			percentage, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || percentage < 0.1 || percentage > 1 {
				return common.NewMessage("The token budget ratio must be between 0.1 and 1")
			}
		case "grok.violation_deduction_amount":
			if _, err := billing.ParseAmountUSD(strings.TrimSpace(value)); err != nil {
				return common.NewMessage("The Grok violation fee must be a non-negative number")
			}
		case "channel_affinity_setting.max_entries", "channel_affinity_setting.default_ttl_seconds", "channel_affinity_setting.rules":
			if err := validateChannelAffinitySettingValue(key, value); err != nil {
				return err
			}
		case "monitor_setting.channel_test_concurrency":
			concurrency, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || concurrency < 1 || concurrency > 32 {
				return common.NewMessage("Channel test concurrency must be between 1 and 32")
			}
		case "monitor_setting.channel_test_mode":
			if value != operation_setting.ChannelTestModeScheduledAll && value != operation_setting.ChannelTestModeAutoBanOnly && value != operation_setting.ChannelTestModePassiveRecovery {
				return common.NewMessage("Invalid automatic test policy")
			}
		case "FileUploadPermission", "FileDownloadPermission", "ImageUploadPermission", "ImageDownloadPermission":
			role, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || !common.IsValidateRole(role) {
				return common.NewMessage("Invalid file access permission role")
			}
		case "Price":
			if _, err := operation_setting.ParseEpayPrice(value); err != nil {
				return err
			}
		case "TopupGroupRatio":
			if _, err := operation_setting.ParseEpayGroupRatios(value); err != nil {
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
		case "PayMethods":
			if _, err := operation_setting.ParseEpayMethods(value); err != nil {
				return err
			}
		case "Chats", "WaffoPayMethods":
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
				return common.NewMessage("Cannot enable OIDC login. Enter all OIDC endpoint settings first.")
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
			return common.NewMessage("OIDC endpoints must use publicly accessible HTTPS addresses")
		}
	}
	return nil
}

func UpdateOption(c *gin.Context) {
	var option OptionUpdateRequest
	err := common.DecodeJson(c.Request.Body, &option)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	normalizedValue, err := normalizeOptionValue(option.Value)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	option.Value = normalizedValue
	if err = validateOptionPatch(map[string]string{option.Key: normalizedValue}); err != nil {
		common.ApiError(c, err)
		return
	}
	switch option.Key {
	case "QuotaForInviter", "QuotaForInvitee", "AffiliateEnabled":
		if isPositiveOptionValue(option.Value.(string)) && !operation_setting.IsPaymentComplianceConfirmed() {
			common.ApiErrorT(c, "Payment, redemption, and invitation reward features are disabled. The administrator must confirm compliance terms before enabling them.")
			return
		}
	case "AffiliateActivatedAt":
		common.ApiErrorT(c, "The invite rebate activation time cannot be changed directly")
		return
	default:
		if isPaymentComplianceOptionKey(option.Key) {
			common.ApiErrorT(c, "Compliance confirmation fields cannot be changed through the general settings API")
			return
		}
	}
	switch option.Key {
	case "AffiliateEnabled":
		enabled, parseErr := strconv.ParseBool(option.Value.(string))
		if parseErr != nil {
			common.ApiErrorT(c, "Invalid invite rebate switch value")
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
				common.ApiErrorT(c, "Activate the invite rebate program first")
				return
			}
			hasSeed, seedErr := model.HasActiveAffiliateSeed()
			if seedErr != nil {
				common.ApiError(c, seedErr)
				return
			}
			if !hasSeed {
				common.ApiErrorT(c, "At least one valid administrator or root user invite code is required")
				return
			}
		}
	case "AffiliateRebateRateBps":
		value, parseErr := strconv.Atoi(option.Value.(string))
		if parseErr != nil || value < 0 || value > 10000 {
			common.ApiErrorT(c, "The rebate rate must be between 0 and 10000 basis points")
			return
		}
	case "AffiliateFreezeHours", "AffiliateDurationDays":
		value, parseErr := strconv.Atoi(option.Value.(string))
		if parseErr != nil || value < 0 {
			common.ApiErrorT(c, "Invite rebate periods cannot be negative")
			return
		}
	case "AffiliatePerInviteeCap":
		_, parseErr := model.AffiliateCapQuota(option.Value.(string))
		if parseErr != nil {
			common.ApiErrorT(c, "The rebate cap per invitee must be a finite non-negative number")
			return
		}
	case "QuotaForInviter":
		if common.AffiliateActivatedAt > 0 && isPositiveOptionValue(option.Value.(string)) {
			common.ApiErrorT(c, "The fixed inviter reward is retired once the invite rebate program is active")
			return
		}
	case "GitHubOAuthEnabled":
		if option.Value == "true" && common.GitHubClientId == "" {
			common.ApiErrorT(c, "Cannot enable {{provider}} OAuth. Enter the {{provider}} Client ID and Client Secret first.", map[string]any{"provider": "GitHub"})
			return
		}
	case "discord.enabled":
		if option.Value == "true" && system_setting.GetDiscordSettings().ClientId == "" {
			common.ApiErrorT(c, "Cannot enable {{provider}} OAuth. Enter the {{provider}} Client ID and Client Secret first.", map[string]any{"provider": "Discord"})
			return
		}
	case "oidc.enabled":
		if option.Value == "true" && system_setting.GetOIDCSettings().ClientId == "" {
			common.ApiErrorT(c, "Cannot enable OIDC login. Enter the OIDC Client ID and Client Secret first.")
			return
		}
	case "LinuxDOOAuthEnabled":
		if option.Value == "true" && common.LinuxDOClientId == "" {
			common.ApiErrorT(c, "Cannot enable {{provider}} OAuth. Enter the {{provider}} Client ID and Client Secret first.", map[string]any{"provider": "LinuxDO"})
			return
		}
	case "EmailDomainRestrictionEnabled":
		if option.Value == "true" && len(common.EmailDomainWhitelist) == 0 {
			common.ApiErrorT(c, "Cannot enable the email domain restriction. Enter the allowed email domains first.")
			return
		}
	case "WeChatAuthEnabled":
		if option.Value == "true" && common.WeChatServerAddress == "" {
			common.ApiErrorT(c, "Cannot enable WeChat login. Enter the WeChat login settings first.")
			return
		}
	case "TurnstileCheckEnabled":
		if option.Value == "true" && common.TurnstileSiteKey == "" {
			common.ApiErrorT(c, "Cannot enable Turnstile verification. Enter the Turnstile settings first.")
			return
		}
	case "TelegramOAuthEnabled":
		if option.Value == "true" && common.TelegramBotToken == "" {
			common.ApiErrorT(c, "Cannot enable Telegram OAuth. Enter the Telegram Bot Token first.")
			return
		}
	case "theme.frontend":
		if option.Value != "default" {
			common.ApiErrorT(c, "The Classic frontend has been removed. The theme can only be set to default")
			return
		}
	case "GroupRatio":
		err = ratio_setting.CheckGroupRatio(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "gemini.safety_settings":
		err = model_setting.ValidateGeminiSafetySettings(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "claude.default_max_tokens":
		err = model_setting.ValidateClaudeDefaultMaxTokens(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "ModelRequestRateLimitGroup":
		err = setting.CheckModelRequestRateLimitGroup(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "AutomaticDisableStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "AutomaticRetryStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "console_setting.api_info":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "ApiInfo")
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "console_setting.announcements":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "Announcements")
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "console_setting.faq":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "FAQ")
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "console_setting.uptime_kuma_groups":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "UptimeKumaGroups")
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	err = model.UpdateOption(option.Key, option.Value.(string))
	if err != nil {
		writePasskeyDomainSettingsError(c, err)
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
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}

	values := make(map[string]string, len(request.Options))
	for key, value := range request.Options {
		normalizedValue, err := normalizeOptionValue(value)
		if err != nil {
			common.ApiErrorStatus(c, http.StatusBadRequest, err)
			return
		}
		values[key] = normalizedValue
	}
	if err := validateOptionPatch(values); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		writePasskeyDomainSettingsError(c, err)
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
