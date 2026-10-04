package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const GatewayConfigKey = "gateway_runtime_v2"

const GatewayMaxWaitMS = 5000
const GatewayMaxUserQueueSize = 20

var ErrGatewayConfigConflict = errors.New("gateway configuration version conflict")
var ErrGatewayConfigReference = errors.New("gateway rate configuration references an unknown channel or credential")
var ErrGatewayTPMUnsupported = errors.New("TPM requires a channel with bounded text or embedding operations")

type GatewayProviderRateLimit struct {
	RPM int64 `json:"rpm"`
	TPM int64 `json:"tpm"`
}

func (limit GatewayProviderRateLimit) Validate() error {
	if limit.RPM < 0 || limit.RPM > 1_000_000_000 || limit.TPM < 0 || limit.TPM > 1_000_000_000 {
		return errors.New("invalid gateway provider rate limit")
	}
	return nil
}

type GatewayAdmissionOverride struct {
	Concurrency *int `json:"concurrency"`
	WaitMS      *int `json:"wait_ms"`
	QueueSize   *int `json:"queue_size"`
}

type GatewayConfig struct {
	TotalQueueSize     int   `json:"total_queue_size"`
	RetryBudgetEnabled bool  `json:"retry_budget_enabled"`
	RetryBudgetPercent int   `json:"retry_budget_percent"`
	RetryBudgetBurst   int   `json:"retry_budget_burst"`
	Version            int64 `json:"version"`
	UserConcurrency    int   `json:"user_concurrency"`
	WaitMS             int   `json:"wait_ms"`
	UserQueueSize      int   `json:"user_queue_size"`
	ChannelQueueSize   int   `json:"channel_queue_size"`
	// MaxChannelRatio is the channel price ratio ceiling applied when
	// automatic routing qualifies candidates. Empty means the price factor is
	// disabled, so the default never drops an existing channel from routing.
	MaxChannelRatio string                              `json:"max_channel_ratio"`
	Groups          map[string]GatewayAdmissionOverride `json:"groups"`
	ChannelRates    map[int]GatewayProviderRateLimit    `json:"channel_rates"`
	CredentialRates map[string]GatewayProviderRateLimit `json:"credential_rates"`
}

func DefaultGatewayConfig() GatewayConfig {
	return GatewayConfig{Version: 1, UserConcurrency: 10, WaitMS: 5000, UserQueueSize: 20, ChannelQueueSize: 20, TotalQueueSize: 1024, RetryBudgetEnabled: true, RetryBudgetPercent: 10, RetryBudgetBurst: 20, MaxChannelRatio: "", Groups: map[string]GatewayAdmissionOverride{}, ChannelRates: map[int]GatewayProviderRateLimit{}, CredentialRates: map[string]GatewayProviderRateLimit{}}
}

// MaxRatioLimit returns the configured ceiling and whether the price factor is
// enabled. An empty value disables the factor rather than defaulting to a
// boundary that would silently exclude high-ratio channels.
func (config GatewayConfig) MaxRatioLimit() (decimal.Decimal, bool, error) {
	if strings.TrimSpace(config.MaxChannelRatio) == "" {
		return decimal.Zero, false, nil
	}
	ratio, err := ParseRouteMaxRatio(config.MaxChannelRatio)
	if err != nil {
		return decimal.Zero, false, err
	}
	return ratio, true, nil
}

func (config GatewayConfig) Validate() error {
	if config.TotalQueueSize < 0 || config.TotalQueueSize > 100000 || config.RetryBudgetPercent < 1 || config.RetryBudgetPercent > 100 || config.RetryBudgetBurst < 1 || config.RetryBudgetBurst > 10000 {
		return errors.New("invalid gateway queue or retry budget")
	}
	if config.UserConcurrency < 0 || config.UserConcurrency > 1000000 || config.WaitMS < 0 || config.WaitMS > GatewayMaxWaitMS || config.UserQueueSize < 0 || config.UserQueueSize > GatewayMaxUserQueueSize || config.ChannelQueueSize < 0 || config.ChannelQueueSize > 10000 {
		return errors.New("invalid gateway admission limits")
	}
	if len(config.Groups) > 10000 || len(config.ChannelRates) > 10000 || len(config.CredentialRates) > 10000 {
		return errors.New("too many gateway configuration overrides")
	}
	if _, _, err := config.MaxRatioLimit(); err != nil {
		return err
	}
	for name, override := range config.Groups {
		if strings.TrimSpace(name) != name || name == "" || len(name) > 64 {
			return errors.New("invalid gateway admission group")
		}
		if err := override.Validate(); err != nil {
			return err
		}
	}
	for channelID, limit := range config.ChannelRates {
		if channelID <= 0 || channelID > 2147483647 {
			return errors.New("invalid gateway rate channel id")
		}
		if err := limit.Validate(); err != nil {
			return err
		}
	}
	for credentialID, limit := range config.CredentialRates {
		identity, err := uuid.Parse(credentialID)
		if err != nil || identity.String() != credentialID {
			return errors.New("invalid gateway rate credential id")
		}
		if err := limit.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (override GatewayAdmissionOverride) Validate() error {
	for _, item := range []struct {
		value   *int
		maximum int
	}{{override.Concurrency, 1000000}, {override.WaitMS, GatewayMaxWaitMS}, {override.QueueSize, GatewayMaxUserQueueSize}} {
		if item.value != nil && (*item.value < 0 || *item.value > item.maximum) {
			return errors.New("invalid gateway admission override")
		}
	}
	return nil
}

func GetGatewayConfig() (GatewayConfig, error) {
	return GetGatewayConfigContext(context.Background())
}

func GetGatewayConfigContext(ctx context.Context) (GatewayConfig, error) {
	var option Option
	err := DB.WithContext(ctx).Where(map[string]any{"key": GatewayConfigKey}).First(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultGatewayConfig(), nil
	}
	if err != nil {
		return GatewayConfig{}, err
	}
	config := DefaultGatewayConfig()
	var fields map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(option.Value, &fields); err != nil {
		return config, err
	}
	if fields == nil {
		return config, errors.New("gateway configuration must be an object")
	}
	for _, field := range []string{"total_queue_size", "retry_budget_enabled", "retry_budget_percent", "retry_budget_burst"} {
		if raw, exists := fields[field]; exists && strings.TrimSpace(string(raw)) == "null" {
			return config, fmt.Errorf("%s cannot be null", field)
		}
	}
	if err := common.UnmarshalJsonStr(option.Value, &config); err != nil {
		return config, err
	}
	if config.Groups == nil {
		config.Groups = map[string]GatewayAdmissionOverride{}
	}
	if config.ChannelRates == nil {
		config.ChannelRates = map[int]GatewayProviderRateLimit{}
	}
	if config.CredentialRates == nil {
		config.CredentialRates = map[string]GatewayProviderRateLimit{}
	}
	return config, config.Validate()
}

func SaveGatewayConfig(config GatewayConfig) (resultErr error) {
	defer func() {
		if resultErr == nil {
			NotifyGatewayAdmissionChanged("config", 0)
		}
	}()
	if err := config.Validate(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var option Option
		if err := lockForUpdate(tx).Where(map[string]any{"key": GatewayConfigKey}).First(&option).Error; err != nil {
			return err
		}
		var previous GatewayConfig
		if err := common.UnmarshalJsonStr(option.Value, &previous); err != nil {
			return err
		}
		if config.Version != previous.Version {
			return ErrGatewayConfigConflict
		}
		channelIDs := make([]int, 0, len(config.ChannelRates))
		for channelID := range config.ChannelRates {
			channelIDs = append(channelIDs, channelID)
		}
		if len(channelIDs) > 0 {
			var count int64
			if err := tx.Model(&Channel{}).Where("id IN ?", channelIDs).Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(channelIDs)) {
				return ErrGatewayConfigReference
			}
		}
		credentialIDs := make([]string, 0, len(config.CredentialRates))
		for credentialID := range config.CredentialRates {
			credentialIDs = append(credentialIDs, credentialID)
		}
		if len(credentialIDs) > 0 {
			var count int64
			if err := tx.Model(&GatewayCredential{}).Where("id IN ?", credentialIDs).Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(credentialIDs)) {
				return ErrGatewayConfigReference
			}
		}
		if err := validateGatewayTPMConfiguration(tx, config); err != nil {
			return err
		}
		config.Version++
		encoded, err := common.Marshal(config)
		if err != nil {
			return err
		}
		result := tx.Model(&Option{}).Where(map[string]any{"key": GatewayConfigKey, "value": option.Value}).Update("value", string(encoded))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrGatewayConfigConflict
		}
		return nil
	})
}

func validateGatewayTPMConfiguration(tx *gorm.DB, config GatewayConfig) error {
	channelIDs := make(map[int]struct{})
	for id, limit := range config.ChannelRates {
		if limit.TPM > 0 {
			channelIDs[id] = struct{}{}
		}
	}
	credentialIDs := make([]string, 0)
	for id, limit := range config.CredentialRates {
		if limit.TPM > 0 {
			credentialIDs = append(credentialIDs, id)
		}
	}
	if len(credentialIDs) > 0 {
		var credentials []GatewayCredential
		if err := tx.Select("channel_id").Where("id IN ?", credentialIDs).Find(&credentials).Error; err != nil {
			return err
		}
		for _, credential := range credentials {
			channelIDs[credential.ChannelID] = struct{}{}
		}
	}
	if len(channelIDs) == 0 {
		return nil
	}
	ids := make([]int, 0, len(channelIDs))
	for id := range channelIDs {
		ids = append(ids, id)
	}
	var channels []Channel
	if err := tx.Select("id", "type", "models", "model_mapping", "settings").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return err
	}
	if len(channels) != len(ids) {
		return ErrGatewayConfigReference
	}
	for _, channel := range channels {
		apiType, known := common.ChannelType2APIType(channel.Type)
		if !known {
			return fmt.Errorf("%w (channel %d)", ErrGatewayTPMUnsupported, channel.Id)
		}
		supported := false
		var mapping map[string]string
		if raw := channel.GetModelMapping(); raw != "" {
			if err := common.Unmarshal([]byte(raw), &mapping); err != nil {
				return fmt.Errorf("%w (channel %d)", ErrGatewayTPMUnsupported, channel.Id)
			}
		}
		for _, model := range strings.Split(channel.Models, ",") {
			actualModel := strings.TrimSpace(model)
			if mapped := mapping[actualModel]; mapped != "" {
				actualModel = mapped
			}
			definition, _ := relaycapability.DescribeForCredentials(apiType, actualModel, nil, channel.GetOtherSettings())
			for _, surface := range definition.Surfaces {
				supported = supported || surface.SupportsTokenBudget()
			}
		}
		if !supported {
			return fmt.Errorf("%w (channel %d)", ErrGatewayTPMUnsupported, channel.Id)
		}
	}
	return nil
}

func migrateGatewayDefaults(db *gorm.DB) error {
	config, err := common.Marshal(DefaultGatewayConfig())
	if err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: GatewayConfigKey, Value: string(config)}).Error; err != nil {
		return err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		var option Option
		if err := lockForUpdate(tx).Where(map[string]any{"key": GatewayConfigKey}).First(&option).Error; err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err := common.UnmarshalJsonStr(option.Value, &fields); err != nil {
			return err
		}
		if fields == nil {
			return errors.New("gateway configuration must be an object")
		}
		changed := false
		for key, value := range map[string]string{"total_queue_size": "1024", "retry_budget_enabled": "true", "retry_budget_percent": "10", "retry_budget_burst": "20", "max_channel_ratio": "\"\""} {
			if _, exists := fields[key]; !exists {
				fields[key] = json.RawMessage(value)
				changed = true
			}
		}
		if !changed {
			return nil
		}
		current := DefaultGatewayConfig()
		if err := common.UnmarshalJsonStr(option.Value, &current); err != nil {
			return err
		}
		version, err := common.Marshal(current.Version + 1)
		if err != nil {
			return err
		}
		fields["version"] = version
		encoded, err := common.Marshal(fields)
		if err != nil {
			return err
		}
		result := tx.Model(&Option{}).Where(map[string]any{"key": GatewayConfigKey, "value": option.Value}).Update("value", string(encoded))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrGatewayConfigConflict
		}
		return nil
	}); err != nil {
		return err
	}
	if err := db.Model(&Token{}).Where("conversion_policy IS NULL OR conversion_policy = ?", "").Update("conversion_policy", "safe").Error; err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: "gateway_runtime_v2_migrated_at", Value: time.Now().UTC().Format(time.RFC3339)}).Error
}
