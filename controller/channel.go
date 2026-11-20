package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/ollama"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type OpenAIModel struct {
	ID         string         `json:"id"`
	Object     string         `json:"object"`
	Created    int64          `json:"created"`
	OwnedBy    string         `json:"owned_by"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Permission []struct {
		ID                 string `json:"id"`
		Object             string `json:"object"`
		Created            int64  `json:"created"`
		AllowCreateEngine  bool   `json:"allow_create_engine"`
		AllowSampling      bool   `json:"allow_sampling"`
		AllowLogprobs      bool   `json:"allow_logprobs"`
		AllowSearchIndices bool   `json:"allow_search_indices"`
		AllowView          bool   `json:"allow_view"`
		AllowFineTuning    bool   `json:"allow_fine_tuning"`
		Organization       string `json:"organization"`
		IsBlocking         bool   `json:"is_blocking"`
	} `json:"permission"`
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type OpenAIModelsResponse struct {
	Data    []OpenAIModel `json:"data"`
	Success bool          `json:"success"`
}

func parseStatusFilter(statusParam string) int {
	switch strings.ToLower(statusParam) {
	case "enabled", "1":
		return common.ChannelStatusEnabled
	case "disabled", "0":
		return 0
	default:
		return -1
	}
}

// clearChannelInfo keeps upstream's name. A channel holds one key
// (Ren2Hub single-key-channel), so it only fills the balance projection.
func clearChannelInfo(channel *model.Channel) {
	channel.BalanceDetail = channel.GetBalanceDetail()
}

func channelIDsFromChannels(channels []*model.Channel) []int {
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel != nil && channel.Id > 0 {
			ids = append(ids, channel.Id)
		}
	}
	return ids
}

func closeActiveChannelWebSockets(channelIDs []int) {
	service.CloseActiveWebSocketsForChannels(channelIDs, service.ChannelDisabledCloseReason)
}

func applyChannelStatusFilter(query *gorm.DB, statusFilter int) *gorm.DB {
	if statusFilter == common.ChannelStatusEnabled {
		return query.Where("status = ?", common.ChannelStatusEnabled)
	}
	if statusFilter == 0 {
		return query.Where("status != ?", common.ChannelStatusEnabled)
	}
	return query
}

func buildChannelListQuery(labIDs []int, statusFilter int, typeFilter int) *gorm.DB {
	query := model.DB.Model(&model.Channel{})
	query = applyChannelLabFilter(query, labIDs)
	query = applyChannelStatusFilter(query, statusFilter)
	if typeFilter >= 0 {
		query = query.Where("type = ?", typeFilter)
	}
	return query
}

func GetChannelOps(c *gin.Context) {
	snapshot := model.CurrentRequestPolicy()
	automaticDisable, source := snapshot.AutoDisable, "global"
	if value, present := c.GetQuery("auto_ban"); present {
		autoBan, err := strconv.ParseBool(value)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		automaticDisable = snapshot.AutoDisable && autoBan
		if snapshot.AutoDisable {
			source = "global_and_channel"
			if !autoBan {
				source = "channel"
			}
		}
	}
	common.ApiSuccess(c, gin.H{
		"retry_times":    snapshot.RetryTimes,
		"request_policy": gin.H{"automatic_disable": automaticDisable, "source": source},
	})
}

func GetChannelDefaultBaseURLs(c *gin.Context) {
	baseURLs := make(map[int]string)
	for channelType, baseURL := range constant.ChannelBaseURLs {
		if baseURL != "" {
			baseURLs[channelType] = baseURL
		}
	}
	common.ApiSuccess(c, baseURLs)
}

func GetAllChannels(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	channelData := make([]*model.Channel, 0)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	sortOptions := model.NewChannelSortOptions(c.Query("sort_by"), c.Query("sort_order"), idSort)
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	statusParam := c.Query("status")
	// statusFilter: -1 all, 1 enabled, 0 disabled (include auto & manual)
	statusFilter := parseStatusFilter(statusParam)
	// type filter
	typeStr := c.Query("type")
	typeFilter := -1
	if typeStr != "" {
		if t, err := strconv.Atoi(typeStr); err == nil {
			typeFilter = t
		}
	}

	labIDs, err := channelLabFilterIDs(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var total int64

	if enableTagMode {
		tags, err := model.GetPaginatedChannelTags(buildChannelListQuery(labIDs, statusFilter, typeFilter), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
		if err != nil {
			common.SysError(common.LogText("failed to get paginated tags: %s", err.Error()))
			common.ApiErrorT(c, "Failed to fetch tags. Please try again later")
			return
		}
		total, err = model.CountChannelTags(buildChannelListQuery(labIDs, statusFilter, typeFilter))
		if err != nil {
			common.SysError(common.LogText("failed to count tags: %s", err.Error()))
			common.ApiErrorT(c, "Failed to count tags. Please try again later")
			return
		}
		for _, tag := range tags {
			if tag == nil || *tag == "" {
				continue
			}
			var tagChannels []*model.Channel
			err := sortOptions.Apply(buildChannelListQuery(labIDs, statusFilter, typeFilter).Where("tag = ?", *tag)).
				Omit("key").
				Find(&tagChannels).Error
			if err != nil {
				common.SysError(common.LogText("failed to get channels by tag: %s", err.Error()))
				common.ApiErrorT(c, "Failed to fetch channels with this tag. Please try again later")
				return
			}
			channelData = append(channelData, tagChannels...)
		}
	} else {
		if err := buildChannelListQuery(labIDs, statusFilter, typeFilter).Count(&total).Error; err != nil {
			common.SysError(common.LogText("failed to count channels: %s", err.Error()))
			common.ApiErrorT(c, "Failed to count channels. Please try again later")
			return
		}

		err := sortOptions.Apply(buildChannelListQuery(labIDs, statusFilter, typeFilter)).
			Limit(pageInfo.GetPageSize()).
			Offset(pageInfo.GetStartIdx()).
			Omit("key").
			Find(&channelData).Error
		if err != nil {
			common.SysError(common.LogText("failed to get channels: %s", err.Error()))
			common.ApiErrorT(c, "Failed to fetch channels. Please try again later")
			return
		}
	}

	if err := model.AttachChannelLabs(c.Request.Context(), channelData); err != nil {
		common.ApiError(c, err)
		return
	}
	attachChannelInFlight(channelData)
	for _, datum := range channelData {
		clearChannelInfo(datum)
	}

	countQuery := buildChannelListQuery(labIDs, statusFilter, -1)
	var results []struct {
		Type  int64
		Count int64
	}
	if err := countQuery.Select("type, count(*) as count").Group("type").Find(&results).Error; err != nil {
		common.SysError(common.LogText("failed to count channel types: %s", err.Error()))
		common.ApiErrorT(c, "Failed to count channel types. Please try again later")
		return
	}
	typeCounts := make(map[int64]int64)
	for _, r := range results {
		typeCounts[r.Type] = r.Count
	}
	common.ApiSuccess(c, gin.H{
		"items":       channelData,
		"total":       total,
		"page":        pageInfo.GetPage(),
		"page_size":   pageInfo.GetPageSize(),
		"type_counts": typeCounts,
	})
	return
}

func buildFetchModelsHeaders(channel *model.Channel, key string) (http.Header, error) {
	var headers http.Header
	switch channel.Type {
	case constant.ChannelTypeAnthropic:
		headers = GetClaudeAuthHeader(key)
	default:
		headers = GetAuthHeader(key)
	}

	if err := applyFetchModelsHeaderOverrides(channel, key, headers); err != nil {
		return nil, err
	}
	return headers, nil
}

func applyFetchModelsHeaderOverrides(channel *model.Channel, key string, headers http.Header) error {
	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:          key,
			HeadersOverride: channel.GetHeaderOverride(),
		},
	}
	overrides, err := relaychannel.ResolveHeaderOverride(info, nil)
	if err != nil {
		return err
	}
	for name, value := range overrides {
		headers.Set(name, value)
	}

	return nil
}

func FetchUpstreamModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel, err := model.GetChannelById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	ids, err := fetchChannelUpstreamModelIDs(channel)
	if err != nil {
		common.ApiErrorT(c, "Failed to fetch models: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    ids,
	})
}

func FixChannelsAbilities(c *gin.Context) {
	success, fails, err := model.FixAbility()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"success": success,
			"fails":   fails,
		},
	})
}

func SearchChannels(c *gin.Context) {
	keyword := c.Query("keyword")
	modelKeyword := c.Query("model")
	statusParam := c.Query("status")
	statusFilter := parseStatusFilter(statusParam)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	sortOptions := model.NewChannelSortOptions(c.Query("sort_by"), c.Query("sort_order"), idSort)
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	channelData := make([]*model.Channel, 0)
	if enableTagMode {
		tags, err := model.SearchTags(keyword, modelKeyword, idSort)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for _, tag := range tags {
			if tag != nil && *tag != "" {
				var tagChannels []*model.Channel
				err := sortOptions.Apply(buildChannelListQuery(nil, -1, -1).Where("tag = ?", *tag)).
					Omit("key").
					Find(&tagChannels).Error
				if err != nil {
					common.ApiError(c, err)
					return
				}
				channelData = append(channelData, tagChannels...)
			}
		}
	} else {
		channels, err := model.SearchChannels(keyword, modelKeyword, idSort, sortOptions)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		channelData = channels
	}

	if statusFilter == common.ChannelStatusEnabled || statusFilter == 0 {
		filtered := make([]*model.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if statusFilter == common.ChannelStatusEnabled && ch.Status != common.ChannelStatusEnabled {
				continue
			}
			if statusFilter == 0 && ch.Status == common.ChannelStatusEnabled {
				continue
			}
			filtered = append(filtered, ch)
		}
		channelData = filtered
	}

	if err := model.AttachChannelLabs(c.Request.Context(), channelData); err != nil {
		common.ApiError(c, err)
		return
	}
	attachChannelInFlight(channelData)
	if lab := c.Query("lab"); lab != "" {
		filtered := make([]*model.Channel, 0, len(channelData))
		for _, channel := range channelData {
			if channel.Lab.ID == lab {
				filtered = append(filtered, channel)
			}
		}
		channelData = filtered
	}

	// calculate type counts for search results
	typeCounts := make(map[int64]int64)
	for _, channel := range channelData {
		typeCounts[int64(channel.Type)]++
	}

	typeParam := c.Query("type")
	typeFilter := -1
	if typeParam != "" {
		if tp, err := strconv.Atoi(typeParam); err == nil {
			typeFilter = tp
		}
	}

	if typeFilter >= 0 {
		filtered := make([]*model.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if ch.Type == typeFilter {
				filtered = append(filtered, ch)
			}
		}
		channelData = filtered
	}

	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	total := len(channelData)
	startIdx := min((page-1)*pageSize, total)
	endIdx := min(startIdx+pageSize, total)

	pagedData := channelData[startIdx:endIdx]

	for _, datum := range pagedData {
		clearChannelInfo(datum)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":       pagedData,
			"total":       total,
			"type_counts": typeCounts,
		},
	})
	return
}

func GetChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.GetChannelById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel != nil {
		if err := model.AttachChannelLabs(c.Request.Context(), []*model.Channel{channel}); err != nil {
			common.ApiError(c, err)
			return
		}
		attachChannelInFlight([]*model.Channel{channel})
		clearChannelInfo(channel)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channel,
	})
	return
}

// GetChannelKey 获取渠道密钥（需要通过安全验证中间件）
// 此函数依赖 SecureVerificationRequired 中间件，确保用户已通过安全验证
func GetChannelKey(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorT(c, "Invalid channel ID")
		return
	}

	// 获取渠道信息（包含密钥）
	channel, err := model.GetChannelById(channelId, true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorT(c, "Channel does not exist")
		return
	}
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}

	// 记录操作审计日志（高危：查看渠道密钥）
	recordManageAudit(c, "channel.key_view", map[string]any{
		"id":   channelId,
		"name": channel.Name,
	})

	// 返回渠道密钥
	// A plugin credential shows as its summary, never its secrets.
	common.ApiSuccessT(c, "Fetched successfully", map[string]any{
		"key": jsplugin.RedactCredentialLines(channel.Key),
	})
}

// maxTaskExtendPluginKeys bounds the plugins one New API channel can be
// extended with; it comfortably covers every built-in and installed plugin.
const maxTaskExtendPluginKeys = 32

// normalizePluginConfig validates the plugin config in the channel's settings
// and stores it as the schema reads it: declared properties only, defaults
// left to the read.
func normalizePluginConfig(channel *model.Channel, plugin *jsplugin.LoadedPlugin) error {
	otherSettings := channel.GetOtherSettings()
	config, err := plugin.ValidateChannelConfig(otherSettings.PluginConfig)
	if err != nil {
		return common.NewMessage("Invalid plugin config: {{error}}", map[string]any{"error": err.Error()})
	}
	if plugin.Meta.ChannelConfig == nil || string(config) == "{}" {
		config = nil
	}
	if !bytes.Equal(config, otherSettings.PluginConfig) {
		otherSettings.PluginConfig = config
		channel.SetOtherSettings(otherSettings)
	}
	return nil
}

// validateChannel 通用的渠道校验函数
func validateChannel(channel *model.Channel, isAdd bool) error {
	if channel == nil {
		return fmt.Errorf("channel cannot be empty")
	}

	// 校验 channel settings
	if err := channel.ValidateSettings(); err != nil {
		return common.NewMessage("Invalid channel settings format: {{error}}", map[string]any{"error": err.Error()})
	}
	if channel.Type == constant.ChannelTypeTaskPlugin {
		pluginKey := strings.TrimSpace(channel.GetSetting().TaskPluginKey)
		if pluginKey == "" {
			return fmt.Errorf("task plugin key is required")
		}
		if len(pluginKey) > 30 {
			return fmt.Errorf("task plugin key must not exceed 30 characters")
		}
		plugin, ok := jsplugin.DefaultRegistry.Get(pluginKey)
		if !ok {
			return fmt.Errorf("task plugin %q is not registered", pluginKey)
		}
		if !plugin.Meta.HasTaskPart() && !plugin.Meta.HasProviderPart() {
			return common.NewMessage("Plugin {{plugin}} only processes requests and cannot be a channel type", map[string]any{"plugin": pluginKey})
		}
		if channel.BaseURL == nil || strings.TrimSpace(*channel.BaseURL) == "" {
			// The plugin default is persisted onto the channel instead of being
			// resolved per request, so the destination host stays an auditable
			// channel property that only an administrator edit can change.
			if plugin.Meta.BaseURL == "" {
				return fmt.Errorf("base URL is required for task plugin channels")
			}
			defaultBaseURL := plugin.Meta.BaseURL
			channel.BaseURL = &defaultBaseURL
		}
		if err := normalizePluginConfig(channel, plugin); err != nil {
			return err
		}
	}

	setting := channel.GetSetting()
	if len(setting.RequestPlugins) > 0 {
		saved := map[string]bool{}
		for _, instance := range request_plugin_setting.GetInstances() {
			saved[instance.ID] = true
		}
		bound := make(map[string]bool, len(setting.RequestPlugins))
		for _, id := range setting.RequestPlugins {
			if !saved[id] {
				return common.NewMessage("Request plugin instance {{id}} does not exist", map[string]any{"id": id})
			}
			if bound[id] {
				return common.NewMessage("Request plugin instance {{id}} is bound more than once", map[string]any{"id": id})
			}
			bound[id] = true
		}
	}
	if channel.Type != constant.ChannelTypeNewAPI && len(setting.TaskExtendPluginKeys) > 0 {
		return fmt.Errorf("task_extend_plugin_keys is only supported on New API channels")
	}
	if channel.Type == constant.ChannelTypeNewAPI {
		if len(setting.TaskExtendPluginKeys) > maxTaskExtendPluginKeys {
			return fmt.Errorf("task_extend_plugin_keys must not exceed %d plugins", maxTaskExtendPluginKeys)
		}
		// The single key stays valid on a New API channel, so both bindings are
		// checked as one set.
		keys := setting.TaskExtendPluginKeys
		if setting.TaskPluginKey != "" {
			keys = append([]string{setting.TaskPluginKey}, keys...)
		}
		bound := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			if key == "" || key != strings.TrimSpace(key) {
				return fmt.Errorf("task plugin key %q is invalid", key)
			}
			if len(key) > 30 {
				return fmt.Errorf("task plugin key must not exceed 30 characters")
			}
			if _, duplicate := bound[key]; duplicate {
				return fmt.Errorf("task plugin %q is bound more than once", key)
			}
			plugin, ok := jsplugin.DefaultRegistry.Get(key)
			if !ok {
				return fmt.Errorf("task plugin %q is not registered", key)
			}
			if !plugin.Meta.SupportsUpstream(jsplugin.UpstreamKindNewAPI) {
				return fmt.Errorf("task plugin %q does not support a New API upstream and cannot be bound to a New API channel", key)
			}
			bound[key] = struct{}{}
		}
	}

	if channel.Type == constant.ChannelTypeNewAPI && strings.TrimSpace(channel.GetBaseURL()) == "" {
		return fmt.Errorf("New API channel base URL cannot be empty")
	}
	if channel.Type == constant.ChannelTypeVLLM && strings.TrimSpace(channel.GetBaseURL()) == "" {
		return fmt.Errorf("vLLM channel base URL cannot be empty")
	}
	if channel.Type == constant.ChannelTypeSGLang && strings.TrimSpace(channel.GetBaseURL()) == "" {
		return fmt.Errorf("SGLang channel base URL cannot be empty")
	}

	// 如果是添加操作，检查 channel 和 key 是否为空
	if isAdd {
		// A channel of a plugin that logs in may start without a key.
		if channel.Key == "" && !service.ChannelSupportsPluginLogin(channel) {
			return fmt.Errorf("channel cannot be empty")
		}
		if err := service.CheckSubmittedPluginCredentials(channel.Type, channel.GetSetting().TaskPluginKey, channel.Key, ""); err != nil {
			return err
		}

		// 检查模型名称长度是否超过 255
		for _, m := range channel.GetModels() {
			if len(m) > 255 {
				return common.NewMessage("Model name is too long: {{model}}", map[string]any{"model": m})
			}
		}
	}

	// VertexAI 特殊校验
	if channel.Type == constant.ChannelTypeVertexAi {
		if channel.Other == "" {
			return common.NewMessage("Deployment region cannot be empty")
		}

		regionMap, err := common.StrToMap(channel.Other)
		if err != nil {
			return common.NewMessage("Deployment region must be valid JSON, for example {{example}}", map[string]any{"example": `{"default": "us-central1", "region2": "us-east1"}`})
		}

		if regionMap["default"] == nil {
			return common.NewMessage("Deployment region must include a default field")
		}

		switch channel.GetOtherSettings().VertexAPIVersion {
		case "", "v1", "v1beta1":
		default:
			return common.NewMessage("Vertex AI API version must be v1 or v1beta1")
		}
	}

	// The dedicated legacy Codex channel remains retired; generic plugin login is supported.
	if err := rejectRetiredChannelType(channel.Type); err != nil {
		return err
	}

	return nil
}

type AddChannelRequest struct {
	Mode                      string         `json:"mode"`
	BatchAddSetKeyPrefix2Name bool           `json:"batch_add_set_key_prefix_2_name"`
	Channel                   *model.Channel `json:"channel"`
}

func getVertexArrayKeys(keys string) ([]string, []int, error) {
	if keys == "" {
		return nil, nil, nil
	}
	var keyArray []any
	err := common.Unmarshal([]byte(keys), &keyArray)
	if err != nil {
		return nil, nil, common.NewMessage("Batch-adding Vertex AI keys requires a JSON array, for example {{example}}. Check the input: {{error}}", map[string]any{"example": "[{key1}, {key2}...]", "error": err.Error()})
	}
	cleanKeys := make([]string, 0, len(keyArray))
	inputIndices := make([]int, 0, len(keyArray))
	for i, key := range keyArray {
		var keyStr string
		switch v := key.(type) {
		case string:
			keyStr = strings.TrimSpace(v)
		default:
			bytes, err := common.Marshal(v)
			if err != nil {
				return nil, nil, common.NewMessage("Failed to encode Vertex AI key JSON: {{error}}", map[string]any{"error": err.Error()})
			}
			keyStr = string(bytes)
		}
		if keyStr != "" {
			cleanKeys = append(cleanKeys, keyStr)
			inputIndices = append(inputIndices, i)
		}
	}
	if len(cleanKeys) == 0 {
		return nil, nil, common.NewMessage("Vertex AI keys for batch adding cannot be empty")
	}
	return cleanKeys, inputIndices, nil
}

func AddChannel(c *gin.Context) {
	addChannelRequest := AddChannelRequest{}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	err = common.Unmarshal(body, &addChannelRequest)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var provided struct {
		Channel map[string]common.RawMessage `json:"channel"`
	}
	if err := common.Unmarshal(body, &provided); err != nil {
		common.ApiError(c, err)
		return
	}
	var requestData map[string]any
	if err := common.Unmarshal(body, &requestData); err != nil {
		common.ApiError(c, err)
		return
	}
	if rejectsMultiKeyFields(requestData) {
		common.ApiError(c, errChannelHoldsOneKey)
		return
	}
	if addChannelRequest.Channel != nil {
		clearChannelProjections(addChannelRequest.Channel)
		if _, ok := provided.Channel["channel_ratio"]; !ok {
			addChannelRequest.Channel.ChannelRatio = "1"
		}
		if err := addChannelRequest.Channel.ValidateRoutingFields(); err != nil {
			common.ApiError(c, err)
			return
		}
	}

	// Binding a plugin needs the same permission on a New API channel as the
	// type-61 channel dedicated to it.
	if addChannelRequest.Channel != nil &&
		(addChannelRequest.Channel.Type == constant.ChannelTypeTaskPlugin || len(addChannelRequest.Channel.GetSetting().TaskPluginBindings()) > 0) &&
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.TaskPluginBind) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "task plugin channels require the task_plugin.bind permission",
		})
		return
	}

	baseURLFromPluginDefault := addChannelRequest.Channel != nil &&
		addChannelRequest.Channel.Type == constant.ChannelTypeTaskPlugin &&
		(addChannelRequest.Channel.BaseURL == nil || strings.TrimSpace(*addChannelRequest.Channel.BaseURL) == "")
	// 使用统一的校验函数
	if err := validateChannel(addChannelRequest.Channel, true); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := checkPluginChannelKeys(c, addChannelRequest.Channel, addChannelRequest.Channel.Key, ""); err != nil {
		common.ApiError(c, err)
		return
	}

	// A channel of a plugin that logs in may start without a key and wait for
	// a login; batch mode makes one channel per key, so it still needs keys.
	keyless := strings.TrimSpace(addChannelRequest.Channel.Key) == "" && service.ChannelSupportsPluginLogin(addChannelRequest.Channel)
	if keyless && addChannelRequest.Mode == "batch" {
		common.ApiError(c, fmt.Errorf("channel cannot be empty"))
		return
	}

	addChannelRequest.Channel.CreatedTime = common.GetTimestamp()
	keys := make([]string, 0)
	inputIndices := make([]int, 0)
	switch addChannelRequest.Mode {
	case "batch":
		if addChannelRequest.Channel.Type == constant.ChannelTypeVertexAi && addChannelRequest.Channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
			// multi json
			keys, inputIndices, err = getVertexArrayKeys(addChannelRequest.Channel.Key)
			if err != nil {
				common.ApiError(c, err)
				return
			}
		} else {
			keys = strings.Split(addChannelRequest.Channel.Key, "\n")
			for i := range keys {
				inputIndices = append(inputIndices, i)
			}
		}
	case "single":
		keys = []string{addChannelRequest.Channel.Key}
		inputIndices = []int{0}
	default:
		common.ApiErrorT(c, "Unsupported add mode")
		return
	}

	channels := make([]model.Channel, 0, len(keys))
	channelInputIndices := make([][]int, 0, len(keys))
	for i, key := range keys {
		if key == "" {
			continue
		}
		localChannel := addChannelRequest.Channel
		localChannel.Key = key
		if err := validateSingleKey(localChannel.Type, localChannel.GetOtherSettings(), localChannel.GetSetting().TaskPluginKey, key); err != nil {
			common.ApiError(c, err)
			return
		}
		if addChannelRequest.BatchAddSetKeyPrefix2Name && len(keys) > 1 {
			keyPrefix := localChannel.Key
			if len(localChannel.Key) > 8 {
				keyPrefix = localChannel.Key[:8]
			}
			localChannel.Name = fmt.Sprintf("%s %s", localChannel.Name, keyPrefix)
		}
		channels = append(channels, *localChannel)
		channelInputIndices = append(channelInputIndices, []int{inputIndices[i]})
	}
	if keyless && len(channels) == 0 {
		addChannelRequest.Channel.Key = ""
		service.PluginLoginNeededStatus(addChannelRequest.Channel)
		channels = append(channels, *addChannelRequest.Channel)
		channelInputIndices = append(channelInputIndices, nil)
	}
	err = model.BatchInsertChannels(channels)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	createAudit := map[string]any{
		"name":  addChannelRequest.Channel.Name,
		"type":  addChannelRequest.Channel.Type,
		"count": len(channels),
	}
	if baseURLFromPluginDefault {
		createAudit["base_url_source"] = "plugin_default"
	}
	recordManageAudit(c, "channel.create", createAudit)
	ids := make([]int, len(channels))
	results := make([]gin.H, 0, len(inputIndices))
	for i, channel := range channels {
		ids[i] = channel.Id
		for _, inputIndex := range channelInputIndices[i] {
			results = append(results, gin.H{"input_index": inputIndex, "channel_id": channel.Id})
		}
	}
	data := gin.H{"ids": ids, "results": results}
	if len(ids) == 1 {
		data["id"] = ids[0]
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
	return
}

func DeleteChannel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	deleted, err := model.DeleteChannels([]int{id})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channelName, channelProxy := "", ""
	if len(deleted) > 0 {
		channelName = deleted[0].Name
		channelProxy = deleted[0].GetSetting().Proxy
	}
	service.RevokeDeletedChannelCredentials(deleted)
	model.InitChannelCache()
	service.ReleaseDeletedChannels(c.Request.Context(), []int{id})
	if len(deleted) == 0 {
		service.ResetProxyClientCache()
	} else {
		service.InvalidateProxyClient(channelProxy)
	}
	recordManageAudit(c, "channel.delete", map[string]any{
		"id":   id,
		"name": channelName,
	})
	closeActiveChannelWebSockets([]int{id})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteDisabledChannel(c *gin.Context) {
	revoked, err := model.DeleteDisabledChannels()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids := make([]int, len(revoked))
	for index, channel := range revoked {
		ids[index] = channel.Id
	}
	rows := int64(len(ids))
	service.RevokeDeletedChannelCredentials(revoked)
	model.InitChannelCache()
	service.ReleaseDeletedChannels(c.Request.Context(), ids)
	if rows > 0 {
		service.ResetProxyClientCache()
	}
	recordManageAudit(c, "channel.delete_disabled", map[string]any{
		"count": rows,
	})
	closeActiveChannelWebSockets(ids)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

type ChannelTag struct {
	Tag            string  `json:"tag"`
	NewTag         *string `json:"new_tag"`
	Priority       *int64  `json:"priority"`
	Weight         *uint   `json:"weight"`
	ModelMapping   *string `json:"model_mapping"`
	Models         *string `json:"models"`
	ParamOverride  *string `json:"param_override"`
	HeaderOverride *string `json:"header_override"`
}

func DisableTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil || channelTag.Tag == "" {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	channels, err := model.GetChannelsByTag(channelTag.Tag, false, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids := channelIDsFromChannels(channels)
	err = model.DisableChannelByTag(channelTag.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_disable", map[string]any{
		"tag": channelTag.Tag,
	})
	closeActiveChannelWebSockets(ids)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func EnableTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil || channelTag.Tag == "" {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	err = model.EnableChannelByTag(channelTag.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_enable", map[string]any{
		"tag": channelTag.Tag,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func EditTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if channelTag.Tag == "" {
		common.ApiErrorT(c, "Tag cannot be empty")
		return
	}
	if (channelTag.ParamOverride != nil || channelTag.HeaderOverride != nil) &&
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ChannelSensitiveWrite) {
		common.ApiErrorT(c, "Unauthorized, insufficient privileges")
		return
	}
	if channelTag.ParamOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.ParamOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			common.ApiErrorT(c, "Parameter override must be valid JSON format")
			return
		}
		channelTag.ParamOverride = common.GetPointer[string](trimmed)
	}
	if channelTag.HeaderOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.HeaderOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			common.ApiErrorT(c, "Header override must be valid JSON format")
			return
		}
		channelTag.HeaderOverride = common.GetPointer[string](trimmed)
	}
	err = model.EditChannelByTag(channelTag.Tag, channelTag.NewTag, channelTag.ModelMapping, channelTag.Models, channelTag.Priority, channelTag.Weight, channelTag.ParamOverride, channelTag.HeaderOverride)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_edit", map[string]any{
		"tag": channelTag.Tag,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

type ChannelBatch struct {
	Ids []int   `json:"ids"`
	Tag *string `json:"tag"`
}

func DeleteChannelBatch(c *gin.Context) {
	channelBatch := ChannelBatch{}
	err := c.ShouldBindJSON(&channelBatch)
	if err != nil || len(channelBatch.Ids) == 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	revoked, err := model.DeleteChannels(channelBatch.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	deletedCount := int64(len(revoked))
	service.RevokeDeletedChannelCredentials(revoked)
	model.InitChannelCache()
	service.ReleaseDeletedChannels(c.Request.Context(), channelBatch.Ids)
	if deletedCount > 0 {
		service.ResetProxyClientCache()
	}
	recordManageAudit(c, "channel.delete_batch", map[string]any{
		"count": deletedCount,
	})
	closeActiveChannelWebSockets(channelBatch.Ids)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    deletedCount,
	})
	return
}

type PatchChannel struct {
	model.Channel
}

type ChannelStatusRequest struct {
	Status int `json:"status"`
}

type ChannelStatusBatchRequest struct {
	Ids    []int `json:"ids"`
	Status int   `json:"status"`
}

func UpdateChannel(c *gin.Context) {
	channel := PatchChannel{}
	rawBody, err := c.GetRawData()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := common.Unmarshal(rawBody, &channel); err != nil {
		common.ApiError(c, err)
		return
	}
	var requestData map[string]any
	if err := common.Unmarshal(rawBody, &requestData); err != nil {
		common.ApiError(c, err)
		return
	}
	if _, ok := requestData["status"]; ok {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	// A channel holds one key, so the retired key list modes cannot change it.
	if rejectsMultiKeyFields(requestData) {
		common.ApiError(c, errChannelHoldsOneKey)
		return
	}
	clearChannelReadOnlyFields(&channel, requestData)

	if channel.Type == constant.ChannelTypeTaskPlugin &&
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.TaskPluginBind) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "task plugin channels require the task_plugin.bind permission",
		})
		return
	}

	baseURLFromPluginDefault := channel.Type == constant.ChannelTypeTaskPlugin &&
		(channel.BaseURL == nil || strings.TrimSpace(*channel.BaseURL) == "")
	// 使用统一的校验函数
	if err := validateChannel(&channel.Channel, false); err != nil {
		common.ApiError(c, err)
		return
	}
	// Preserve existing ChannelInfo to ensure multi-key channels keep correct state even if the client does not send ChannelInfo in the request.
	originChannel, err := model.GetChannelById(channel.Id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if _, provided := requestData["channel_ratio"]; !provided {
		channel.ChannelRatio = originChannel.ChannelRatio
	}
	if _, provided := requestData["max_concurrency"]; !provided {
		channel.MaxConcurrency = originChannel.MaxConcurrency
	}
	if err := channel.Channel.ValidateRoutingFields(); err != nil {
		common.ApiError(c, err)
		return
	}
	originProxy := originChannel.GetSetting().Proxy
	proxyChanged := false
	_, settingProvided := requestData["setting"]
	if settingProvided {
		newProxy, _ := service.NormalizeProxyURL(channel.GetSetting().Proxy)
		normalizedOriginProxy, originProxyErr := service.NormalizeProxyURL(originProxy)
		proxyChanged = originProxyErr != nil || normalizedOriginProxy != newProxy
	}
	// Changing which plugins a channel binds needs the bind permission on any
	// channel type; resubmitting an unchanged New API binding list does not, so
	// administrators without it can still edit the rest of a gateway channel.
	if settingProvided &&
		!slices.Equal(channel.GetSetting().TaskPluginBindings(), originChannel.GetSetting().TaskPluginBindings()) &&
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.TaskPluginBind) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "task plugin channels require the task_plugin.bind permission",
		})
		return
	}

	if channelHasSensitiveChanges(&channel, originChannel, requestData) &&
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ChannelSensitiveWrite) {
		common.ApiErrorT(c, "Unauthorized, insufficient privileges")
		return
	}

	requestedKey := channel.Key
	// An edit that leaves the key as it read it writes no key, so it cannot put
	// back a credential a refresh replaced meanwhile.
	keyChanged := requestedKey != "" && requestedKey != originChannel.Key
	// A type or setting left out keeps the stored one.
	keptKey, keptType, keptPluginKey := originChannel.Key, channel.Type, channel.GetSetting().TaskPluginKey
	if keyChanged {
		keptKey = requestedKey
	}
	if keptType == 0 {
		keptType = originChannel.Type
	}
	if !settingProvided {
		keptPluginKey = originChannel.GetSetting().TaskPluginKey
	}
	// A new key, or a new way of reading the kept one (type, plugin or Vertex
	// key type), is checked as the channel will read it.
	originSettings, keptSettings := originChannel.GetOtherSettings(), originChannel.GetOtherSettings()
	if channel.OtherSettings != "" {
		keptSettings = channel.GetOtherSettings()
	}
	authChanged := keptType != originChannel.Type || keptPluginKey != originChannel.GetSetting().TaskPluginKey ||
		keptSettings.VertexKeyType != originSettings.VertexKeyType
	if keyChanged || authChanged {
		if err := validateSingleKey(keptType, keptSettings, keptPluginKey, keptKey); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := service.CheckSubmittedPluginCredentials(keptType, keptPluginKey, keptKey, originChannel.Key); err != nil {
		common.ApiError(c, err)
		return
	}
	// validateChannel checks the plugin config of a request that names type
	// 61; settings sent without the type are checked against the kept plugin.
	if channel.Type == 0 && keptType == constant.ChannelTypeTaskPlugin && channel.OtherSettings != "" {
		if plugin, ok := jsplugin.DefaultRegistry.Get(strings.TrimSpace(keptPluginKey)); ok {
			if err := normalizePluginConfig(&channel.Channel, plugin); err != nil {
				common.ApiError(c, err)
				return
			}
		}
	}
	if keyChanged && keptType == constant.ChannelTypeTaskPlugin {
		checked := channel.Channel
		checked.Type = keptType
		checked.SetSetting(dto.ChannelSettings{TaskPluginKey: keptPluginKey})
		if checked.OtherSettings == "" {
			checked.OtherSettings = originChannel.OtherSettings
		}
		if checked.BaseURL == nil {
			checked.BaseURL = originChannel.BaseURL
		}
		if err := checkPluginChannelKeys(c, &checked, keptKey, originChannel.Key); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	// The new key is written under the channel's key lock, the lock a
	// credential refresh also takes to write its result, and is checked against
	// the key stored at that moment.
	var keyBefore, keyAfter string
	if keyChanged {
		stored, err := model.UpdateChannelLocked(channel.Id, func(stored *model.Channel) error {
			if err := service.CheckSubmittedPluginCredentials(keptType, keptPluginKey, requestedKey, stored.Key); err != nil {
				return err
			}
			keyBefore, stored.Key = stored.Key, requestedKey
			return nil
		})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		keyAfter = stored.Key
	}
	err = channel.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if proxyChanged {
		service.InvalidateProxyClient(originProxy)
	}
	if keyChanged {
		service.RevokeRemovedChannelCredentials(originChannel, keyBefore, keyAfter)
	}
	// 记录变更的字段名（语言无关的字段标识），密钥仅记录"已更换"绝不记录内容。
	changedFields := make([]string, 0)
	if channel.Models != originChannel.Models {
		changedFields = append(changedFields, "models")
	}
	if channel.Type != originChannel.Type {
		changedFields = append(changedFields, "type")
	}
	if !equalStringPtr(channel.BaseURL, originChannel.BaseURL) {
		changedFields = append(changedFields, "base_url")
	}
	if keyChanged {
		changedFields = append(changedFields, "key")
	}
	updateAudit := map[string]any{
		"id":             channel.Id,
		"name":           channel.Name,
		"changed_fields": changedFields,
	}
	if baseURLFromPluginDefault {
		updateAudit["base_url_source"] = "plugin_default"
	}
	recordManageAudit(c, "channel.update", updateAudit)
	channel.Key = ""
	attachChannelInFlight([]*model.Channel{&channel.Channel})
	clearChannelInfo(&channel.Channel)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channel,
	})
	return
}

func UpdateChannelStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	req := ChannelStatusRequest{}
	if err := c.ShouldBindJSON(&req); err != nil || !isManageableChannelStatus(req.Status) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	result := model.UpdateChannelStatusDetailed(id, "", req.Status, "manual operation")
	if result.Changed {
		model.InitChannelCache()
		if req.Status != common.ChannelStatusEnabled {
			closeActiveChannelWebSockets([]int{id})
		}
	}
	recordManageAudit(c, "channel.status_update", map[string]any{
		"id":                  id,
		"status":              req.Status,
		"changed":             result.Changed,
		"publication_pending": result.PublicationPending,
	})
	c.JSON(http.StatusOK, gin.H{
		"success":             true,
		"message":             "",
		"data":                result.Changed,
		"publication_pending": result.PublicationPending,
	})
}

func BatchUpdateChannelStatus(c *gin.Context) {
	req := ChannelStatusBatchRequest{}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Ids) == 0 || !isManageableChannelStatus(req.Status) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	changedCount := 0
	var disabledIDs []int
	pendingCount := 0
	for _, id := range req.Ids {
		result := model.UpdateChannelStatusDetailed(id, "", req.Status, "manual batch operation")
		if result.Changed {
			changedCount++
			if result.PublicationPending {
				pendingCount++
			}
			if req.Status != common.ChannelStatusEnabled {
				disabledIDs = append(disabledIDs, id)
			}
		}
	}
	if changedCount > 0 {
		model.InitChannelCache()
	}
	if len(disabledIDs) > 0 {
		closeActiveChannelWebSockets(disabledIDs)
	}
	recordManageAudit(c, "channel.status_update_batch", map[string]any{
		"count":               changedCount,
		"total":               len(req.Ids),
		"status":              req.Status,
		"publication_pending": pendingCount,
	})
	c.JSON(http.StatusOK, gin.H{
		"success":             true,
		"message":             "",
		"data":                changedCount,
		"publication_pending": pendingCount,
	})
}

func isManageableChannelStatus(status int) bool {
	return status == common.ChannelStatusEnabled || status == common.ChannelStatusManuallyDisabled
}

// equalStringPtr 比较两个 *string 是否相等（均为 nil 视为相等）。
func equalStringPtr(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

type fetchModelsRequest struct {
	ChannelID      int     `json:"channel_id"`
	BaseURL        *string `json:"base_url"`
	Type           int     `json:"type"`
	Key            string  `json:"key"`
	TaskPluginKey  string  `json:"task_plugin_key"`
	AdvancedCustom *string `json:"advanced_custom"`
	HeaderOverride *string `json:"header_override"`
	Proxy          *string `json:"proxy"`
}

func buildAdvancedCustomModelPreviewChannel(req fetchModelsRequest) (*model.Channel, error) {
	var channel *model.Channel
	if req.ChannelID > 0 {
		savedChannel, err := model.GetChannelById(req.ChannelID, true)
		if err != nil {
			return nil, err
		}
		if savedChannel.Type != constant.ChannelTypeAdvancedCustom {
			return nil, fmt.Errorf("channel %d is not an advanced custom channel", req.ChannelID)
		}
		channel = savedChannel
	} else {
		key := strings.TrimSpace(req.Key)
		if key != "" {
			key = strings.Split(key, "\n")[0]
		}
		channel = &model.Channel{
			Type: req.Type,
			Key:  key,
		}
	}

	if channel.Type != constant.ChannelTypeAdvancedCustom {
		return nil, fmt.Errorf("channel type must be advanced custom")
	}
	if req.BaseURL != nil {
		baseURL := strings.TrimSpace(*req.BaseURL)
		channel.BaseURL = &baseURL
	}

	settings := channel.GetOtherSettings()
	if req.AdvancedCustom != nil {
		rawConfig := strings.TrimSpace(*req.AdvancedCustom)
		if rawConfig == "" {
			return nil, fmt.Errorf("advanced_custom is required")
		}
		var config dto.AdvancedCustomConfig
		if err := common.UnmarshalJsonStr(rawConfig, &config); err != nil {
			return nil, err
		}
		settings.AdvancedCustom = &config
	} else if req.ChannelID <= 0 {
		return nil, fmt.Errorf("advanced_custom is required")
	}
	channel.SetOtherSettings(settings)

	if req.HeaderOverride != nil {
		rawHeaderOverride := strings.TrimSpace(*req.HeaderOverride)
		if rawHeaderOverride != "" {
			var headerOverride map[string]any
			if err := common.UnmarshalJsonStr(rawHeaderOverride, &headerOverride); err != nil {
				return nil, fmt.Errorf("header_override must be a JSON object: %w", err)
			}
		}
		channel.HeaderOverride = &rawHeaderOverride
	}
	if req.Proxy != nil {
		channelSettings := channel.GetSetting()
		channelSettings.Proxy = strings.TrimSpace(*req.Proxy)
		channel.SetSetting(channelSettings)
	}

	if err := validateChannel(channel, false); err != nil {
		return nil, err
	}
	return channel, nil
}

// checkPluginChannelKeys runs the checkKey of a task plugin channel's plugin
// on the key lines an administrator typed in: those of key that storedKey
// does not hold.
func checkPluginChannelKeys(c *gin.Context, channel *model.Channel, key, storedKey string) error {
	if channel == nil || channel.Type != constant.ChannelTypeTaskPlugin {
		return nil
	}
	plugin, ok := jsplugin.DefaultRegistry.Get(channel.GetSetting().TaskPluginKey)
	if !ok {
		return nil
	}
	config := plugin.ChannelConfigFor(channel.GetOtherSettings().PluginConfig)
	return service.CheckPluginKeys(c.Request.Context(), plugin, channel.GetBaseURL(), config, key, storedKey, i18n.StatedLang(c))
}

// applyTaskPluginModelPreview sets up an unsaved task plugin channel for a
// model list preview as the Advanced Custom preview does: the plugin's
// meta.baseUrl stands in for an empty base URL, and the form's header
// override and proxy apply.
func applyTaskPluginModelPreview(channel *model.Channel, req fetchModelsRequest) error {
	pluginKey := strings.TrimSpace(req.TaskPluginKey)
	settings := dto.ChannelSettings{TaskPluginKey: pluginKey}
	if req.Proxy != nil {
		settings.Proxy = strings.TrimSpace(*req.Proxy)
	}
	channel.SetSetting(settings)
	if channel.GetBaseURL() == "" {
		if plugin, ok := jsplugin.DefaultRegistry.Get(pluginKey); ok && plugin.Meta.BaseURL != "" {
			baseURL := plugin.Meta.BaseURL
			channel.BaseURL = &baseURL
		}
	}
	if req.HeaderOverride != nil {
		rawHeaderOverride := strings.TrimSpace(*req.HeaderOverride)
		if rawHeaderOverride != "" {
			var headerOverride map[string]any
			if err := common.UnmarshalJsonStr(rawHeaderOverride, &headerOverride); err != nil {
				return fmt.Errorf("header_override must be a JSON object: %w", err)
			}
		}
		channel.HeaderOverride = &rawHeaderOverride
	}
	return nil
}

func FetchModels(c *gin.Context) {
	var req fetchModelsRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request",
		})
		return
	}

	var channel *model.Channel
	if req.Type == constant.ChannelTypeAdvancedCustom || req.ChannelID > 0 {
		var err error
		channel, err = buildAdvancedCustomModelPreviewChannel(req)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		baseURL := ""
		if req.BaseURL != nil {
			baseURL = strings.TrimSpace(*req.BaseURL)
		}
		if baseURL == "" {
			baseURL = constant.GetChannelBaseURL(req.Type)
		}

		key := strings.TrimSpace(req.Key)
		key = strings.Split(key, "\n")[0]
		channel = &model.Channel{
			Type:    req.Type,
			Key:     key,
			BaseURL: &baseURL,
		}
		if req.Type == constant.ChannelTypeTaskPlugin {
			if err := applyTaskPluginModelPreview(channel, req); err != nil {
				common.ApiError(c, err)
				return
			}
		}
	}

	models, err := fetchChannelUpstreamModelIDs(channel)
	if err != nil {
		common.ApiErrorT(c, "Failed to fetch models: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
}

func BatchSetChannelTag(c *gin.Context) {
	channelBatch := ChannelBatch{}
	err := c.ShouldBindJSON(&channelBatch)
	if err != nil || len(channelBatch.Ids) == 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	err = model.BatchSetChannelTag(channelBatch.Ids, channelBatch.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_batch_set", map[string]any{
		"count": len(channelBatch.Ids),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    len(channelBatch.Ids),
	})
	return
}

func GetTagModels(c *gin.Context) {
	tag := c.Query("tag")
	if tag == "" {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Tag cannot be empty"))
		return
	}

	channels, err := model.GetChannelsByTag(tag, false, false) // idSort=false, selectAll=false
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	var longestModels string
	maxLength := 0

	// Find the longest models string among all channels with the given tag
	for _, channel := range channels {
		if channel.Models != "" {
			currentModels := strings.Split(channel.Models, ",")
			if len(currentModels) > maxLength {
				maxLength = len(currentModels)
				longestModels = channel.Models
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    longestModels,
	})
	return
}

// CopyChannel handles cloning an existing channel with its key.
// POST /api/channel/copy/:id
// Optional query params:
//
//	suffix         - string appended to the original name (default "_copy")
//	reset_balance  - bool, when true will reset balance & used_quota to 0 (default true)
func CopyChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid id"})
		return
	}

	suffix := c.DefaultQuery("suffix", "_copy")
	resetBalance := true
	if rbStr := c.DefaultQuery("reset_balance", "true"); rbStr != "" {
		if v, err := strconv.ParseBool(rbStr); err == nil {
			resetBalance = v
		}
	}

	// fetch original channel with key
	origin, err := model.GetChannelById(id, true)
	if err != nil {
		common.SysError(common.LogText("failed to get channel by id: %s", err.Error()))
		common.ApiErrorT(c, "Failed to fetch channel. Please try again later")
		return
	}
	if (origin.Type == constant.ChannelTypeTaskPlugin || len(origin.GetSetting().TaskPluginBindings()) > 0) &&
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.TaskPluginBind) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "task plugin channels require the task_plugin.bind permission"})
		return
	}

	// clone channel
	clone := *origin // shallow copy is sufficient as we will overwrite primitives
	clone.Id = 0     // let DB auto-generate
	clone.CreatedTime = common.GetTimestamp()
	clone.Name = origin.Name + suffix
	clone.TestTime = 0
	clone.ResponseTime = 0
	if resetBalance {
		clone.Balance = 0
		clone.UsedQuota = 0
	}
	service.DropPluginCredentials(&clone)

	if err := clone.ValidateSettings(); err != nil {
		common.SysError(common.LogText("failed to validate cloned channel: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Failed to copy channel: invalid channel settings"})
		return
	}

	// insert
	if err := clone.Insert(); err != nil {
		common.SysError(common.LogText("failed to clone channel: %s", err.Error()))
		common.ApiErrorT(c, "Failed to copy channel. Please try again later")
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.copy", map[string]any{
		"sourceId": id,
		"id":       clone.Id,
		"name":     clone.Name,
	})
	// success
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": clone.Id}})
}

// OllamaPullModel 拉取 Ollama 模型
func OllamaPullModel(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request parameters",
		})
		return
	}

	if req.ChannelID == 0 || req.ModelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Channel ID and model name are required",
		})
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "This operation is only supported for Ollama channels",
		})
		return
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	key := strings.Split(channel.Key, "\n")[0]
	err = ollama.PullOllamaModel(baseURL, key, req.ModelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("Failed to pull model: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Model %s pulled successfully", req.ModelName),
	})
}

// OllamaPullModelStream 流式拉取 Ollama 模型
func OllamaPullModelStream(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request parameters",
		})
		return
	}

	if req.ChannelID == 0 || req.ModelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Channel ID and model name are required",
		})
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "This operation is only supported for Ollama channels",
		})
		return
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	// 设置 SSE 头部
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	key := strings.Split(channel.Key, "\n")[0]

	// 创建进度回调函数
	progressCallback := func(progress ollama.OllamaPullResponse) {
		data, _ := json.Marshal(progress)
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
		c.Writer.Flush()
	}

	// 执行拉取
	err = ollama.PullOllamaModelStream(baseURL, key, req.ModelName, progressCallback)

	if err != nil {
		errorData, _ := json.Marshal(gin.H{
			"error": err.Error(),
		})
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(errorData))
	} else {
		successData, _ := json.Marshal(gin.H{
			"message": fmt.Sprintf("Model %s pulled successfully", req.ModelName),
		})
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(successData))
	}

	// 发送结束标志
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}

// OllamaDeleteModel 删除 Ollama 模型
func OllamaDeleteModel(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request parameters",
		})
		return
	}

	if req.ChannelID == 0 || req.ModelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Channel ID and model name are required",
		})
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "This operation is only supported for Ollama channels",
		})
		return
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	key := strings.Split(channel.Key, "\n")[0]
	err = ollama.DeleteOllamaModel(baseURL, key, req.ModelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("Failed to delete model: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Model %s deleted successfully", req.ModelName),
	})
}

// OllamaVersion 获取 Ollama 服务版本信息
func OllamaVersion(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid channel id",
		})
		return
	}

	channel, err := model.GetChannelById(id, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "This operation is only supported for Ollama channels",
		})
		return
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	key := strings.Split(channel.Key, "\n")[0]
	version, err := ollama.FetchOllamaVersion(baseURL, key)
	if err != nil {
		common.ApiErrorT(c, "Failed to fetch Ollama version: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"version": version,
		},
	})
}
