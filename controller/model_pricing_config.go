package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func decodeModelPriceRequest(c *gin.Context, request any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	return common.DecodeJsonNumbers(c.Request.Body, request)
}

func GetModelPricingConfig(c *gin.Context) {
	snapshot, err := model.GetModelPricingSnapshot(c.QueryArray("model"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range snapshot.Entries {
		snapshot.Entries[i].Configured = model.PricingValuesForAPI(snapshot.Entries[i].Configured)
		snapshot.Entries[i].Effective = model.PricingValuesForAPI(snapshot.Entries[i].Effective)
	}
	for key, raw := range snapshot.Options {
		var values map[string]any
		if err := common.DecodeJsonNumbers(bytes.NewBufferString(raw), &values); err != nil {
			common.ApiError(c, err)
			return
		}
		for name, value := range values {
			if number, ok := value.(json.Number); ok {
				values[name] = string(number)
			}
		}
		encoded, err := common.Marshal(values)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		snapshot.Options[key] = string(encoded)
	}
	common.ApiSuccess(c, snapshot)
}

func PreviewModelPricingConversion(c *gin.Context) {
	var request struct {
		ModelName string              `json:"model_name"`
		Pricing   model.PricingValues `json:"pricing"`
	}
	if err := decodeModelPriceRequest(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	preview, err := model.PreviewModelPricingConversion(request.ModelName, request.Pricing)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	preview.Effective = model.PricingValuesForAPI(preview.Effective)
	common.ApiSuccess(c, preview)
}

func PreviewModelPricing(c *gin.Context) {
	var request struct {
		ModelName string              `json:"model_name"`
		Pricing   model.PricingValues `json:"pricing"`
	}
	if err := decodeModelPriceRequest(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	preview, err := model.PreviewModelPricing(request.ModelName, request.Pricing)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, model.ModelPricingDescription{
		Effective:      model.PricingValuesForAPI(preview),
		CacheWriteMode: model.ResolveCacheWriteMode(request.ModelName, request.Pricing),
		BillingDetails: model.ResolveLegacyBillingDetails(request.ModelName, preview, request.Pricing),
	})
}

func UpdateModelPricingConfig(c *gin.Context) {
	var request struct {
		Changes []model.ModelPricingChange `json:"changes"`
	}
	if err := decodeModelPriceRequest(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.UpdateModelPricing(request.Changes); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrModelPricingConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	names := make([]string, 0, len(request.Changes))
	for _, change := range request.Changes {
		names = append(names, change.ModelName)
	}
	recordManageAudit(c, "model.pricing.update", map[string]any{"models": names})
	common.ApiSuccess(c, gin.H{"updated_models": names})
}

type modelPricingSimulationRequest struct {
	ModelName string              `json:"model_name"`
	PluginKey string              `json:"plugin_key,omitempty"`
	Pricing   model.PricingValues `json:"pricing"`
	Usage     map[string]any      `json:"usage"`
	Request   struct {
		Params  map[string]any    `json:"params"`
		Headers map[string]string `json:"headers"`
	} `json:"request"`
	Time string `json:"time,omitempty"`
}

func SimulateModelPricing(c *gin.Context) {
	var request modelPricingSimulationRequest
	if err := decodeModelPriceRequest(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid pricing simulation request"})
		return
	}
	charge, err := simulateModelPricing(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	// Evaluation inputs and encrypted replay evidence are intentionally omitted.
	common.ApiSuccess(c, gin.H{"amount_usd": charge.TotalUSD, "details": gin.H{"base_usd": charge.BaseUSD, "quota": charge.FinalQuota, "rounding_delta": charge.RoundingDelta}})
}

func simulateModelPricing(request modelPricingSimulationRequest) (*billing.ChargeResult, error) {
	if request.Usage == nil {
		return nil, errors.New("simulation usage is required")
	}
	instant := time.Now().UTC()
	if request.Time != "" {
		var err error
		instant, err = time.Parse(time.RFC3339Nano, request.Time)
		if err != nil {
			return nil, errors.New("simulation time must use RFC3339 with a timezone")
		}
	}
	effective, err := model.PreviewModelPricing(request.ModelName, request.Pricing)
	if err != nil {
		return nil, err
	}
	generation := jsplugin.DefaultRegistry.Generation()
	plugins := generation.PluginsByModel(request.ModelName)
	declaredModel := request.ModelName
	if len(plugins) == 0 {
		if target, ok := model.ResolveTaskModelAlias(generation, request.ModelName); ok {
			if plugin, exists := generation.Get(target.PluginKey); exists {
				plugins = []*jsplugin.LoadedPlugin{plugin}
				declaredModel = target.Declared
			}
		}
	}
	if len(plugins) > 1 && request.PluginKey == "" {
		return nil, errors.New("plugin_key is required to simulate a shared task model")
	}
	var selected *jsplugin.LoadedPlugin
	for _, plugin := range plugins {
		if request.PluginKey == plugin.Meta.Key || (request.PluginKey == "" && len(plugins) == 1) {
			selected = plugin
			break
		}
	}
	if request.PluginKey != "" && selected == nil {
		return nil, errors.New("selected plugin does not serve this model")
	}
	var schema *billingexpr.UsageSchema
	taskUsage := false
	if selected != nil {
		variants, _ := effective[billing_setting.PluginBillingExprOption].(map[string]any)
		if expression, exists := variants[selected.Meta.Key]; exists {
			effective["billing_setting.billing_mode"] = "tiered_expr"
			effective["billing_setting.billing_expr"] = expression
		}
		if effective["billing_setting.billing_mode"] == "tiered_expr" {
			manifest := selected.Meta.UsageFactsForModels(declaredModel)
			schema = model.ModelPricingUsageSchema(manifest)
			taskUsage = true
		}
	}
	version, err := billing.NewModelPriceVersion(request.ModelName, billing.PricingValues(effective), taskUsage)
	if err != nil {
		return nil, err
	}
	version.UsageSchema = schema
	quote, err := billing.NewQuote(version, "1", nil, instant)
	if err != nil {
		return nil, err
	}
	input := billingexpr.RequestInput{Headers: request.Request.Headers, Evaluation: billingexpr.NewEvaluation()}
	input.Evaluation.Now = func() time.Time { return instant }
	if request.Request.Params != nil {
		input.Body, err = common.Marshal(request.Request.Params)
		if err != nil {
			return nil, err
		}
	}
	usage := billing.Usage{InputIncludesCache: true, Source: "admin_simulation", Complete: true, Terminal: "completed"}
	if taskUsage {
		// The task runtime exposes plugin usage facts, not the submission body
		// or transport headers. Preview must use that same evaluation boundary.
		input.Body, input.Headers = nil, nil
		facts := make(map[string]any, len(request.Usage))
		for name, value := range request.Usage {
			field, exists := schema.Fields[name]
			if !exists {
				return nil, fmt.Errorf("undeclared task usage field: %s", name)
			}
			normalized, err := billingexpr.NormalizeUsageValue(field, value)
			if err != nil {
				return nil, fmt.Errorf("invalid task usage field: %s", name)
			}
			facts[name] = normalized
		}
		input.Usage, err = service.TaskExpressionUsage(facts, schema)
		if err != nil {
			return nil, err
		}
	} else {
		if err := applySimulationUsage(request.Usage, &usage, &input); err != nil {
			return nil, err
		}
	}
	cipher, err := billing.NewEvidenceCipher([]byte(common.CryptoSecret))
	if err != nil {
		return nil, errors.New("simulation encryption is not configured")
	}
	identity := uuid.NewString()
	_, charge, err := cipher.Evaluate(quote, usage, input, billing.EvidenceBinding{RequestID: identity, AttemptID: identity, ConfigHash: billing.PricingConfigHash(quote), Phase: "preview"})
	return charge, err
}

func applySimulationUsage(values map[string]any, usage *billing.Usage, input *billingexpr.RequestInput) error {
	fields := map[string]*int64{"p": &usage.InputTokens, "c": &usage.OutputTokens, "len": &usage.ContextTokens, "cr": &usage.CacheReadTokens, "cc": &usage.CacheWriteTokens, "cc1h": &usage.CacheWrite1hTokens, "img": &usage.ImageInputTokens, "img_o": &usage.ImageOutputTokens, "ai": &usage.AudioInputTokens, "ao": &usage.AudioOutputTokens, "reasoning": &usage.ReasoningTokens}
	for key, raw := range values {
		number, ok := raw.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be an integer token quantity", key)
		}
		value, err := number.Int64()
		if err != nil || value < 0 || value > math.MaxInt32 {
			return fmt.Errorf("%s must be an integer between 0 and %d", key, math.MaxInt32)
		}
		if field, exists := fields[key]; exists {
			*field = value
			continue
		}
		switch key {
		case "img_cr":
			usage.ImageCacheReadTokens = &value
		case "ai_cr":
			usage.AudioCacheReadTokens = &value
		case "image_count":
			if value < 1 || value > dto.MaxImageN {
				return fmt.Errorf("image_count must be between 1 and %d", dto.MaxImageN)
			}
			count := int(value)
			input.ImageCount = &count
			usage.Quantities = map[string]string{"images": strconv.FormatInt(value, 10)}
		default:
			return fmt.Errorf("unknown token usage field: %s", key)
		}
	}
	if _, exists := values["len"]; !exists {
		usage.ContextTokens = usage.InputTokens
	}
	if usage.AudioInputTokens > 0 || usage.AudioOutputTokens > 0 {
		usage.PricingKind = "audio"
	}
	return nil
}
