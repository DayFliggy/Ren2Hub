package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strconv"
)

// These rows are execution evidence derived from model_pricing. They are never
// an independently editable price catalogue or an active-price pointer.
type BillingPriceVersion struct {
	ID         int64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ScopeKind  string   `gorm:"type:varchar(32);not null" json:"scope_kind"`
	ChannelID  int      `gorm:"not null" json:"channel_id"`
	Model      string   `gorm:"type:varchar(255);not null;index" json:"model"`
	Definition LongText `gorm:"not null" json:"-"`
	Hash       string   `gorm:"type:varchar(64);not null;uniqueIndex" json:"hash"`
	CreatedAt  int64    `gorm:"type:bigint;not null" json:"created_at"`
	CreatedBy  int      `gorm:"not null" json:"created_by"`
}

func (*BillingPriceVersion) BeforeUpdate(*gorm.DB) error {
	return errors.New("billing configuration evidence is immutable")
}
func (*BillingPriceVersion) BeforeDelete(*gorm.DB) error {
	return errors.New("billing configuration evidence is immutable")
}

func ModelPriceConfigured(values PricingValues) bool {
	if values["billing_setting.billing_mode"] == billing_setting.BillingModeTieredExpr {
		expression, _ := values["billing_setting.billing_expr"].(string)
		return strings.TrimSpace(expression) != ""
	}
	_, ratio := values["ModelRatio"]
	_, price := values["ModelPrice"]
	return ratio || price
}

// ResolveModelBillingPrice reads the sole administrator pricing configuration.
// Evidence is stored at the actual request boundary, never by a price preview.
func ResolveModelBillingPrice(ctx context.Context, modelName, pluginKey string) (*billing.PriceVersion, error) {
	return resolveModelBillingPrice(ctx, modelName, pluginKey, "", nil)
}

// A running task uses the schema of the executable selected for this attempt.
func ResolveTaskModelBillingPrice(ctx context.Context, modelName, mappedModel string, ref TaskPluginRef) (*billing.PriceVersion, error) {
	return resolveModelBillingPrice(ctx, modelName, ref.Key, mappedModel, &ref)
}

func resolveModelBillingPrice(ctx context.Context, modelName, pluginKey, mappedModel string, ref *TaskPluginRef) (*billing.PriceVersion, error) {
	if DB == nil {
		return nil, errors.New("pricing database unavailable")
	}
	values, _, _, err := readModelPricingMaps(DB.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	effective := effectiveModelPricing(values, modelName)
	taskUsage := false
	var usageSchema *billingexpr.UsageSchema
	if pluginKey != "" {
		plugin, exists := jsplugin.DefaultRegistry.Generation().Get(pluginKey)
		if !exists {
			return nil, fmt.Errorf("task pricing plugin is unavailable")
		}
		if ref != nil && (plugin.Meta.Version != ref.Version || plugin.SourceHash != ref.SourceHash) {
			return nil, fmt.Errorf("task pricing plugin changed before quoting")
		}
		names := []string{modelName}
		if mappedModel != "" && mappedModel != modelName {
			names = append(names, mappedModel)
		}
		var expression any
		for _, name := range names {
			if configured, exists := values[billing_setting.PluginBillingExprOption][billing_setting.PluginBillingExprKey(pluginKey, name)]; exists {
				expression = configured
				break
			}
		}
		if expression == nil {
			for _, name := range names {
				candidate := effectiveModelPricing(values, name)
				if candidate["billing_setting.billing_mode"] == billing_setting.BillingModeTieredExpr {
					expression = candidate["billing_setting.billing_expr"]
					break
				}
			}
		}
		if expression != nil {
			effective["billing_setting.billing_mode"] = billing_setting.BillingModeTieredExpr
			effective["billing_setting.billing_expr"] = expression
		}
		if effective["billing_setting.billing_mode"] == billing_setting.BillingModeTieredExpr {
			expression, _ := effective["billing_setting.billing_expr"].(string)
			schema, _ := plugin.Meta.UsageForModels(mappedModel, modelName)
			usageSchema = ModelPricingUsageSchema(schema)
			if !billing_setting.TaskExprCompatible(expression, schema) {
				return nil, fmt.Errorf("model expression is not configured for the executing task plugin")
			}
			taskUsage = true
		}
	}
	if !ModelPriceConfigured(effective) {
		return nil, gorm.ErrRecordNotFound
	}
	version, err := billing.NewModelPriceVersion(modelName, billing.PricingValues(effective), taskUsage)
	if err != nil {
		return nil, err
	}
	version.UsageSchema = usageSchema
	for name, price := range operation_setting.ModelToolPrices(modelName) {
		version.Prices = append(version.Prices, billing.UnitPrice{Quantity: "tool:" + name, Unit: "per_thousand_calls", Price: strconv.FormatFloat(price, 'g', -1, 64)})
	}
	sort.Slice(version.Prices, func(i, j int) bool { return version.Prices[i].Quantity < version.Prices[j].Quantity })
	return &version, nil
}

func StoreBillingPriceEvidence(ctx context.Context, version billing.PriceVersion) (*billing.PriceVersion, error) {
	version.ID, version.CreatedAt, version.Hash = 0, 0, ""
	prepared, err := billing.PreparePriceVersion(version)
	if err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(prepared)
	if err != nil {
		return nil, err
	}
	row := BillingPriceVersion{ScopeKind: prepared.ScopeKind, ChannelID: prepared.ChannelID, Model: prepared.Model, Definition: LongText(encoded), Hash: prepared.Hash, CreatedAt: common.GetTimestamp()}
	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "hash"}}, DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	if err := DB.WithContext(ctx).Where("hash = ?", prepared.Hash).First(&row).Error; err != nil {
		return nil, err
	}
	return decodeBillingPrice(row)
}

func decodeBillingPrice(row BillingPriceVersion) (*billing.PriceVersion, error) {
	var version billing.PriceVersion
	if err := common.UnmarshalJsonStr(string(row.Definition), &version); err != nil {
		return nil, err
	}
	if err := billing.ValidateFrozenPriceVersion(version); err != nil {
		return nil, err
	}
	if version.Hash != row.Hash || version.Model != row.Model || version.ScopeKind != row.ScopeKind || version.ChannelID != row.ChannelID {
		return nil, errors.New("billing configuration evidence mismatch")
	}
	version.ID, version.CreatedAt = row.ID, row.CreatedAt
	return &version, nil
}

func GetActiveBillingPrice(ctx context.Context, modelName string) (*billing.PriceVersion, error) {
	return ResolveModelBillingPrice(ctx, modelName, "")
}
func GetActiveBillingPriceForChannel(ctx context.Context, modelName string, _ int) (*billing.PriceVersion, error) {
	return GetActiveBillingPrice(ctx, modelName)
}
