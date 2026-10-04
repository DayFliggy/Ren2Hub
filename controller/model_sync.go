package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 上游地址
const (
	upstreamModelsURL  = "https://basellm.github.io/llm-metadata/api/newapi/models.json"
	upstreamVendorsURL = "https://basellm.github.io/llm-metadata/api/newapi/vendors.json"
)

func normalizeLocale(locale string) (string, bool) {
	l := strings.ToLower(strings.TrimSpace(locale))
	switch l {
	case "en", "ja":
		return l, true
	case "zh", "zh-cn", "zh-hans":
		return "zh-CN", true
	case "zh-tw", "zh-hant":
		return "zh-TW", true
	default:
		return "", false
	}
}

func getUpstreamBase() string {
	return common.GetEnvOrDefaultString("SYNC_UPSTREAM_BASE", "https://basellm.github.io/llm-metadata")
}

func getUpstreamURLs(locale string) (modelsURL, vendorsURL string) {
	base := strings.TrimRight(getUpstreamBase(), "/")
	if l, ok := normalizeLocale(locale); ok && l != "" {
		return fmt.Sprintf("%s/api/i18n/%s/newapi/models.json", base, l),
			fmt.Sprintf("%s/api/i18n/%s/newapi/vendors.json", base, l)
	}
	return fmt.Sprintf("%s/api/newapi/models.json", base), fmt.Sprintf("%s/api/newapi/vendors.json", base)
}

type upstreamEnvelope[T any] struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []T    `json:"data"`
}

type upstreamModel struct {
	Description string          `json:"description"`
	Endpoints   json.RawMessage `json:"endpoints"`
	Icon        string          `json:"icon"`
	ModelName   string          `json:"model_name"`
	NameRule    int             `json:"name_rule"`
	Status      int             `json:"status"`
	Tags        string          `json:"tags"`
	VendorName  string          `json:"vendor_name"`
}

type upstreamVendor struct {
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Name        string `json:"name"`
	Status      int    `json:"status"`
}

var (
	etagCache  = make(map[string]string)
	bodyCache  = make(map[string][]byte)
	cacheMutex sync.RWMutex
)

type overwriteField struct {
	ModelName string   `json:"model_name"`
	Fields    []string `json:"fields"`
	// LocalVersion is the fingerprint returned by preview. It prevents a
	// metadata edit made after preview from being silently overwritten.
	LocalVersion string `json:"local_version"`
}

type syncRequest struct {
	Overwrite []overwriteField `json:"overwrite"`
	Locale    string           `json:"locale"`
	// CreateModels lists the previewed missing models to create; others are not created.
	CreateModels []string `json:"create_models"`
	// SourceVersion is the upstream catalog version returned by the preview.
	SourceVersion string `json:"source_version"`
}

// syncOverwriteFields are the local fields an overwrite request may replace.
var syncOverwriteFields = map[string]bool{
	"description": true,
	"icon":        true,
	"tags":        true,
	"vendor":      true,
	"name_rule":   true,
	"status":      true,
}

// validate accepts only requests built from a preview: they carry the previewed
// catalog version and name known fields for every overwritten model.
func (req *syncRequest) validate() error {
	if strings.TrimSpace(req.SourceVersion) == "" {
		return errors.New("缺少上游资料版本，请先预览")
	}
	for _, ow := range req.Overwrite {
		if strings.TrimSpace(ow.ModelName) == "" || len(ow.Fields) == 0 {
			return errors.New("覆盖项必须包含模型名称和字段")
		}
		if strings.TrimSpace(ow.LocalVersion) == "" {
			return errors.New("覆盖项缺少本地资料版本，请先预览")
		}
		for _, field := range ow.Fields {
			if !syncOverwriteFields[field] {
				return fmt.Errorf("不支持覆盖字段: %s", field)
			}
		}
	}
	return nil
}

func newHTTPClient() *http.Client {
	timeoutSec := common.GetEnvOrDefault("SYNC_HTTP_TIMEOUT_SECONDS", 10)
	dialer := &net.Dialer{Timeout: time.Duration(timeoutSec) * time.Second}
	transport := &http.Transport{
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   time.Duration(timeoutSec) * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: time.Duration(timeoutSec) * time.Second,
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig.Clone()
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if strings.HasSuffix(host, "github.io") {
			if conn, err := dialer.DialContext(ctx, "tcp4", addr); err == nil {
				return conn, nil
			}
			return dialer.DialContext(ctx, "tcp6", addr)
		}
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: transport}
}

var (
	httpClientOnce sync.Once
	httpClient     *http.Client
)

func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		httpClient = newHTTPClient()
	})
	return httpClient
}

func fetchJSON[T any](ctx context.Context, url string, out *upstreamEnvelope[T]) error {
	var lastErr error
	attempts := common.GetEnvOrDefault("SYNC_HTTP_RETRY", 3)
	if attempts < 1 {
		attempts = 1
	}
	baseDelay := 200 * time.Millisecond
	maxMB := common.GetEnvOrDefault("SYNC_HTTP_MAX_MB", 10)
	maxBytes := int64(maxMB) << 20
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		// ETag conditional request
		cacheMutex.RLock()
		if et := etagCache[url]; et != "" {
			req.Header.Set("If-None-Match", et)
		}
		cacheMutex.RUnlock()

		resp, err := getHTTPClient().Do(req)
		if err != nil {
			lastErr = err
			// backoff with jitter
			sleep := baseDelay * time.Duration(1<<attempt)
			jitter := time.Duration(rand.Intn(150)) * time.Millisecond
			time.Sleep(sleep + jitter)
			continue
		}
		func() {
			defer resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusOK:
				// read body into buffer for caching and flexible decode
				limited := io.LimitReader(resp.Body, maxBytes)
				buf, err := io.ReadAll(limited)
				if err != nil {
					lastErr = err
					return
				}
				// cache body and ETag
				cacheMutex.Lock()
				if et := resp.Header.Get("ETag"); et != "" {
					etagCache[url] = et
				}
				bodyCache[url] = buf
				cacheMutex.Unlock()

				// Try decode as envelope first
				if err := json.Unmarshal(buf, out); err != nil {
					// Try decode as pure array
					var arr []T
					if err2 := json.Unmarshal(buf, &arr); err2 != nil {
						lastErr = err
						return
					}
					out.Success = true
					out.Data = arr
					out.Message = ""
				} else {
					if !out.Success && len(out.Data) == 0 && out.Message == "" {
						out.Success = true
					}
				}
				lastErr = nil
			case http.StatusNotModified:
				// use cache
				cacheMutex.RLock()
				buf := bodyCache[url]
				cacheMutex.RUnlock()
				if len(buf) == 0 {
					lastErr = errors.New("cache miss for 304 response")
					return
				}
				if err := json.Unmarshal(buf, out); err != nil {
					var arr []T
					if err2 := json.Unmarshal(buf, &arr); err2 != nil {
						lastErr = err
						return
					}
					out.Success = true
					out.Data = arr
					out.Message = ""
				} else {
					if !out.Success && len(out.Data) == 0 && out.Message == "" {
						out.Success = true
					}
				}
				lastErr = nil
			default:
				lastErr = errors.New(resp.Status)
			}
		}()
		if lastErr == nil {
			return nil
		}
		sleep := baseDelay * time.Duration(1<<attempt)
		jitter := time.Duration(rand.Intn(150)) * time.Millisecond
		time.Sleep(sleep + jitter)
	}
	return lastErr
}

// upstreamCatalog is one fetch of the upstream model and vendor metadata.
// Version fingerprints the locale and fetched data so an apply request can be
// bound to the catalog its preview showed.
type upstreamCatalog struct {
	ModelsURL    string
	VendorsURL   string
	Version      string
	ModelByName  map[string]upstreamModel
	VendorByName map[string]upstreamVendor
}

func fetchUpstreamCatalog(ctx context.Context, locale string) (upstreamCatalog, error) {
	modelsURL, vendorsURL := getUpstreamURLs(locale)
	catalog := upstreamCatalog{ModelsURL: modelsURL, VendorsURL: vendorsURL}
	timeoutSec := common.GetEnvOrDefault("SYNC_HTTP_TIMEOUT_SECONDS", 15)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var vendorsEnv upstreamEnvelope[upstreamVendor]
	var modelsEnv upstreamEnvelope[upstreamModel]
	var modelsErr, vendorsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		vendorsErr = fetchJSON(ctx, vendorsURL, &vendorsEnv)
	}()
	go func() {
		defer wg.Done()
		modelsErr = fetchJSON(ctx, modelsURL, &modelsEnv)
	}()
	wg.Wait()
	if modelsErr != nil {
		return catalog, fmt.Errorf("获取上游模型失败: %w", modelsErr)
	}
	// Vendor rows feed created vendors and the version; a partial catalog would
	// silently create vendors without upstream details.
	if vendorsErr != nil {
		return catalog, fmt.Errorf("获取上游供应商失败: %w", vendorsErr)
	}

	catalog.VendorByName = make(map[string]upstreamVendor)
	for _, v := range vendorsEnv.Data {
		if v.Name != "" {
			catalog.VendorByName[v.Name] = v
		}
	}
	catalog.ModelByName = make(map[string]upstreamModel)
	for _, m := range modelsEnv.Data {
		if m.ModelName != "" {
			catalog.ModelByName[m.ModelName] = m
		}
	}
	// Locales that resolve to the same URLs share a version.
	resolvedLocale, _ := normalizeLocale(locale)
	encoded, err := common.Marshal([]any{resolvedLocale, catalog.ModelByName, catalog.VendorByName})
	if err != nil {
		return catalog, err
	}
	catalog.Version = fmt.Sprintf("%x", sha256.Sum256(encoded))
	return catalog, nil
}

// ensureVendorID resolves a vendor by name, creating it from upstream data when
// absent. Failures are returned so the caller skips the model instead of
// silently detaching it from its vendor.
func ensureVendorID(vendorName string, vendorByName map[string]upstreamVendor, vendorIDCache map[string]int, createdVendors *int) (int, error) {
	if vendorName == "" {
		return 0, nil
	}
	if id, ok := vendorIDCache[vendorName]; ok {
		return id, nil
	}
	var existing model.Vendor
	err := model.DB.Where("name = ?", vendorName).First(&existing).Error
	if err == nil {
		vendorIDCache[vendorName] = existing.Id
		return existing.Id, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, fmt.Errorf("query vendor %s: %w", vendorName, err)
	}
	uv := vendorByName[vendorName]
	v := &model.Vendor{
		Name:        vendorName,
		Description: uv.Description,
		Icon:        coalesce(uv.Icon, ""),
		Status:      chooseStatus(uv.Status, 1),
	}
	if err := v.Insert(); err != nil {
		return 0, fmt.Errorf("create vendor %s: %w", vendorName, err)
	}
	*createdVendors++
	vendorIDCache[vendorName] = v.Id
	return v.Id, nil
}

// SyncUpstreamModels 按预览结果同步上游模型与供应商：
// - source_version 必须与当前上游目录一致，否则返回 409，需重新预览
// - 仅创建 create_models 中当前仍缺失且上游存在的模型，新建模型 sync_official = 1
// - overwrite 仅覆盖所列模型的所列字段（前提：sync_official <> 0）
// - 创建或覆盖失败的模型进入 skipped_models
func SyncUpstreamModels(c *gin.Context) {
	var req syncRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求格式错误: " + err.Error()})
		return
	}
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 1) 拉取上游目录并校验版本
	catalog, err := fetchUpstreamCatalog(c.Request.Context(), req.Locale)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error(), "locale": req.Locale, "source_urls": gin.H{"models_url": catalog.ModelsURL, "vendors_url": catalog.VendorsURL}})
		return
	}
	if catalog.Version != req.SourceVersion {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "上游资料已变化，请重新预览", "code": "METADATA_SOURCE_CHANGED"})
		return
	}
	vendorByName := catalog.VendorByName
	modelByName := catalog.ModelByName

	// 2) 获取当前缺失模型列表
	missing, err := model.GetMissingModels()
	if err != nil {
		common.SysError("failed to get missing models: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取模型列表失败，请稍后重试"})
		return
	}
	missingSet := make(map[string]bool, len(missing))
	for _, name := range missing {
		missingSet[name] = true
	}

	// Check every selected local record before creating anything. A conflict
	// must leave the whole request untouched, including requested creations.
	for _, ow := range req.Overwrite {
		if _, ok := modelByName[ow.ModelName]; !ok {
			continue
		}
		var local model.Model
		if err := model.DB.Where("model_name = ?", ow.ModelName).First(&local).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				common.ApiError(c, err)
				return
			}
			continue
		}
		if local.SyncOfficial == 0 {
			continue
		}
		if got := modelMetadataFingerprint(local); got != ow.LocalVersion {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"message": "本地资料已变化，请重新预览",
				"code":    "METADATA_LOCAL_CHANGED",
			})
			return
		}
	}

	// 3) 创建请求所列、当前仍缺失且上游存在的模型
	createdModels := 0
	createdVendors := 0
	updatedModels := 0
	skipped := make([]string, 0)
	createdList := make([]string, 0)
	updatedList := make([]string, 0)

	// 本地缓存：vendorName -> id
	vendorIDCache := make(map[string]int)

	for _, name := range req.CreateModels {
		up, ok := modelByName[name]
		// 已有记录或已被规则覆盖的名称、上游没有的名称都不创建
		if !ok || !missingSet[name] {
			skipped = append(skipped, name)
			continue
		}

		vendorID, err := ensureVendorID(up.VendorName, vendorByName, vendorIDCache, &createdVendors)
		if err != nil {
			common.SysError(fmt.Sprintf("metadata sync: create model %s: %v", name, err))
			skipped = append(skipped, name)
			continue
		}

		mi := &model.Model{
			ModelName:    name,
			Description:  up.Description,
			Icon:         up.Icon,
			Tags:         up.Tags,
			VendorID:     vendorID,
			Status:       chooseStatus(up.Status, 1),
			SyncOfficial: 1,
			NameRule:     up.NameRule,
		}
		if err := mi.Insert(); err != nil {
			common.SysError(fmt.Sprintf("metadata sync: create model %s: %v", name, err))
			skipped = append(skipped, name)
			continue
		}
		delete(missingSet, name)
		createdModels++
		createdList = append(createdList, name)
	}

	// 4) 覆盖请求所列模型的所列字段（vendorIDCache 与创建阶段共用）
	for _, ow := range req.Overwrite {
		up, ok := modelByName[ow.ModelName]
		if !ok {
			skipped = append(skipped, ow.ModelName)
			continue
		}
		var local model.Model
		if err := model.DB.Where("model_name = ?", ow.ModelName).First(&local).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				common.SysError(fmt.Sprintf("metadata sync: load model %s: %v", ow.ModelName, err))
			}
			skipped = append(skipped, ow.ModelName)
			continue
		}
		// 跳过被禁用官方同步的模型
		if local.SyncOfficial == 0 {
			skipped = append(skipped, ow.ModelName)
			continue
		}

		updates := make(map[string]any, len(ow.Fields))
		// 只有选择 vendor 字段才解析或创建供应商
		if containsField(ow.Fields, "vendor") {
			vendorID, err := ensureVendorID(up.VendorName, vendorByName, vendorIDCache, &createdVendors)
			if err != nil {
				common.SysError(fmt.Sprintf("metadata sync: overwrite model %s: %v", ow.ModelName, err))
				skipped = append(skipped, ow.ModelName)
				continue
			}
			updates["vendor_id"] = vendorID
		}
		if containsField(ow.Fields, "description") {
			updates["description"] = up.Description
		}
		if containsField(ow.Fields, "icon") {
			updates["icon"] = up.Icon
		}
		if containsField(ow.Fields, "tags") {
			updates["tags"] = up.Tags
		}
		if containsField(ow.Fields, "name_rule") {
			updates["name_rule"] = up.NameRule
		}
		if containsField(ow.Fields, "status") {
			updates["status"] = chooseStatus(up.Status, local.Status)
		}
		if err := model.DB.Model(&model.Model{}).Where("id = ?", local.Id).Updates(updates).Error; err != nil {
			common.SysError(fmt.Sprintf("metadata sync: overwrite model %s: %v", ow.ModelName, err))
			skipped = append(skipped, ow.ModelName)
			continue
		}
		updatedModels++
		updatedList = append(updatedList, ow.ModelName)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"created_models":  createdModels,
			"created_vendors": createdVendors,
			"updated_models":  updatedModels,
			"skipped_models":  skipped,
			"created_list":    createdList,
			"updated_list":    updatedList,
			"source": gin.H{
				"locale":      req.Locale,
				"models_url":  catalog.ModelsURL,
				"vendors_url": catalog.VendorsURL,
				"version":     catalog.Version,
			},
		},
	})
}

func containsField(fields []string, key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, f := range fields {
		if strings.ToLower(strings.TrimSpace(f)) == key {
			return true
		}
	}
	return false
}

// modelMetadataFingerprint covers every local field that a metadata sync can
// observe or overwrite. The encoded map is deterministic through the common
// JSON wrapper, so the value can safely round-trip through the preview UI.
func modelMetadataFingerprint(mi model.Model) string {
	encoded, err := common.Marshal([]any{
		mi.ModelName,
		mi.Description,
		mi.Icon,
		mi.Tags,
		mi.VendorID,
		mi.Status,
		mi.SyncOfficial,
		mi.NameRule,
	})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func chooseStatus(primary, fallback int) int {
	if primary == 0 && fallback != 0 {
		return fallback
	}
	if primary != 0 {
		return primary
	}
	return 1
}

// SyncUpstreamPreview 预览上游与本地的差异（仅用于弹窗选择）
func SyncUpstreamPreview(c *gin.Context) {
	// 1) 拉取上游数据
	locale := c.Query("locale")
	catalog, err := fetchUpstreamCatalog(c.Request.Context(), locale)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error(), "locale": locale, "source_urls": gin.H{"models_url": catalog.ModelsURL, "vendors_url": catalog.VendorsURL}})
		return
	}
	modelByName := catalog.ModelByName
	upstreamNames := make([]string, 0, len(modelByName))
	for name := range modelByName {
		upstreamNames = append(upstreamNames, name)
	}

	// 2) 本地已有模型
	var locals []model.Model
	if len(upstreamNames) > 0 {
		if err := model.DB.Where("model_name IN ? AND sync_official <> 0", upstreamNames).Find(&locals).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	}

	// 本地 vendor 名称映射
	vendorIdSet := make(map[int]struct{})
	for _, m := range locals {
		if m.VendorID != 0 {
			vendorIdSet[m.VendorID] = struct{}{}
		}
	}
	vendorIDs := make([]int, 0, len(vendorIdSet))
	for id := range vendorIdSet {
		vendorIDs = append(vendorIDs, id)
	}
	idToVendorName := make(map[int]string)
	if len(vendorIDs) > 0 {
		var dbVendors []model.Vendor
		if err := model.DB.Where("id IN ?", vendorIDs).Find(&dbVendors).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		for _, v := range dbVendors {
			idToVendorName[v.Id] = v.Name
		}
	}

	// 3) 缺失且上游存在的模型
	missingList, err := model.GetMissingModels()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	missing := make([]string, 0)
	for _, name := range missingList {
		if _, ok := modelByName[name]; ok {
			missing = append(missing, name)
		}
	}

	// 4) 计算冲突字段
	type conflictField struct {
		Field    string      `json:"field"`
		Local    interface{} `json:"local"`
		Upstream interface{} `json:"upstream"`
	}
	type conflictItem struct {
		ModelName    string          `json:"model_name"`
		LocalVersion string          `json:"local_version"`
		Fields       []conflictField `json:"fields"`
	}

	var conflicts []conflictItem
	for _, local := range locals {
		up, ok := modelByName[local.ModelName]
		if !ok {
			continue
		}
		fields := make([]conflictField, 0, 6)
		if strings.TrimSpace(local.Description) != strings.TrimSpace(up.Description) {
			fields = append(fields, conflictField{Field: "description", Local: local.Description, Upstream: up.Description})
		}
		if strings.TrimSpace(local.Icon) != strings.TrimSpace(up.Icon) {
			fields = append(fields, conflictField{Field: "icon", Local: local.Icon, Upstream: up.Icon})
		}
		if strings.TrimSpace(local.Tags) != strings.TrimSpace(up.Tags) {
			fields = append(fields, conflictField{Field: "tags", Local: local.Tags, Upstream: up.Tags})
		}
		// vendor 对比使用名称
		localVendor := idToVendorName[local.VendorID]
		if strings.TrimSpace(localVendor) != strings.TrimSpace(up.VendorName) {
			fields = append(fields, conflictField{Field: "vendor", Local: localVendor, Upstream: up.VendorName})
		}
		if local.NameRule != up.NameRule {
			fields = append(fields, conflictField{Field: "name_rule", Local: local.NameRule, Upstream: up.NameRule})
		}
		if local.Status != chooseStatus(up.Status, local.Status) {
			fields = append(fields, conflictField{Field: "status", Local: local.Status, Upstream: up.Status})
		}
		if len(fields) > 0 {
			conflicts = append(conflicts, conflictItem{
				ModelName:    local.ModelName,
				LocalVersion: modelMetadataFingerprint(local),
				Fields:       fields,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"missing":   missing,
			"conflicts": conflicts,
			"source": gin.H{
				"locale":      locale,
				"models_url":  catalog.ModelsURL,
				"vendors_url": catalog.VendorsURL,
				"version":     catalog.Version,
			},
		},
	})
}
