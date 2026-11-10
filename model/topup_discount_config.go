package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Top-up discount configuration lives in its own append-only table, never in
// options, so generic option writes and SyncOptions cannot bypass validation.
// See docs/billing/TOPUP_DISCOUNT.md.

const (
	TopUpDiscountModeOff    = "off"
	TopUpDiscountModeShadow = "shadow"
	TopUpDiscountModeOn     = "on"

	topUpDiscountSchemaVersion = 1
	topUpDiscountMaxTiers      = 10
	topUpDiscountMaxExcluded   = 100
	topUpDiscountMaxCapRate    = "0.5"
)

var (
	ErrTopUpDiscountInvalidConfig     = errors.New("topup_discount_invalid_config")
	ErrTopUpDiscountSchemaUnsupported = errors.New("topup_discount_schema_unsupported")
	ErrTopUpDiscountVersionConflict   = errors.New("topup_discount_version_conflict")
)

var (
	topUpDiscountRatePattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,4})?$`)
	topUpDiscountQuotaPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})$`)
)

// TopUpDiscountConfig is the editable document. Rates and thresholds are
// decimal and integer strings so JSON numbers never round through float64.
type TopUpDiscountConfig struct {
	SchemaVersion   int                         `json:"schema_version"`
	Timezone        string                      `json:"timezone"`
	CapRate         string                      `json:"cap_rate"`
	FixedRate       string                      `json:"fixed_rate"`
	Amount          TopUpDiscountTableConfig    `json:"amount"`
	Site            TopUpDiscountTableConfig    `json:"site"`
	Personal        TopUpDiscountPersonalConfig `json:"personal"`
	ExcludedUserIDs []int                       `json:"excluded_user_ids"`
}

type TopUpDiscountPersonalConfig struct {
	Day   TopUpDiscountTableConfig `json:"day"`
	Week  TopUpDiscountTableConfig `json:"week"`
	Month TopUpDiscountTableConfig `json:"month"`
}

type TopUpDiscountTableConfig struct {
	Mode  string                    `json:"mode"`
	Tiers []TopUpDiscountTierConfig `json:"tiers"`
}

type TopUpDiscountTierConfig struct {
	ThresholdQuota string `json:"threshold_quota"`
	Rate           string `json:"rate"`
}

// TopUpDiscountTier and TopUpDiscountTable are the validated runtime form.
type TopUpDiscountTier struct {
	Threshold int64
	Rate      decimal.Decimal
}

type TopUpDiscountTable struct {
	Mode  string
	Tiers []TopUpDiscountTier
}

// TopUpDiscountSettings is one validated, immutable configuration version.
type TopUpDiscountSettings struct {
	Version   int64
	CreatedAt int64
	CreatedBy int
	Config    TopUpDiscountConfig
	Location  *time.Location
	CapRate   decimal.Decimal
	FixedRate decimal.Decimal
	Amount    TopUpDiscountTable
	Site      TopUpDiscountTable
	Day       TopUpDiscountTable
	Week      TopUpDiscountTable
	Month     TopUpDiscountTable
	excluded  map[int]struct{}
}

func (s *TopUpDiscountSettings) IsExcluded(userID int) bool {
	_, ok := s.excluded[userID]
	return ok
}

// Enabled reports whether any component is computed, including shadow ones.
func (s *TopUpDiscountSettings) Enabled() bool {
	return s.FixedRate.IsPositive() || s.Amount.Mode != TopUpDiscountModeOff || s.UsesSite() || s.UsesWindows()
}

// Active reports whether any component can change the price.
func (s *TopUpDiscountSettings) Active() bool {
	if s.FixedRate.IsPositive() {
		return true
	}
	for _, table := range []TopUpDiscountTable{s.Amount, s.Site, s.Day, s.Week, s.Month} {
		if table.Mode == TopUpDiscountModeOn {
			return true
		}
	}
	return false
}

func (s *TopUpDiscountSettings) UsesSite() bool {
	return s.Site.Mode != TopUpDiscountModeOff
}

// PersonalOn reports whether a personal window can change the price.
func (s *TopUpDiscountSettings) PersonalOn() bool {
	return s.Day.Mode == TopUpDiscountModeOn || s.Week.Mode == TopUpDiscountModeOn || s.Month.Mode == TopUpDiscountModeOn
}

func (s *TopUpDiscountSettings) UsesWindows() bool {
	return s.Day.Mode != TopUpDiscountModeOff || s.Week.Mode != TopUpDiscountModeOff || s.Month.Mode != TopUpDiscountModeOff
}

// DefaultTopUpDiscountConfig is version 0, used while the table is empty.
// Thresholds assume the standard 500000 quota per USD: $5/$15/$50/$150/$500
// per day, half the month value per week, and $25…$2,500 per month.
func DefaultTopUpDiscountConfig() TopUpDiscountConfig {
	personal := func(thresholds ...string) TopUpDiscountTableConfig {
		tiers := make([]TopUpDiscountTierConfig, len(thresholds))
		for i, threshold := range thresholds {
			tiers[i] = TopUpDiscountTierConfig{ThresholdQuota: threshold, Rate: fmt.Sprintf("0.0%d", i+1)}
		}
		return TopUpDiscountTableConfig{Mode: TopUpDiscountModeShadow, Tiers: tiers}
	}
	return TopUpDiscountConfig{
		SchemaVersion: topUpDiscountSchemaVersion,
		Timezone:      "Asia/Shanghai",
		CapRate:       "0.05",
		FixedRate:     "0",
		Amount:        TopUpDiscountTableConfig{Mode: TopUpDiscountModeOff, Tiers: []TopUpDiscountTierConfig{}},
		Site:          TopUpDiscountTableConfig{Mode: TopUpDiscountModeOff, Tiers: []TopUpDiscountTierConfig{}},
		Personal: TopUpDiscountPersonalConfig{
			Day:   personal("2500000", "7500000", "25000000", "75000000", "250000000"),
			Week:  personal("6250000", "18750000", "62500000", "187500000", "625000000"),
			Month: personal("12500000", "37500000", "125000000", "375000000", "1250000000"),
		},
		ExcludedUserIDs: []int{1},
	}
}

// ParseTopUpDiscountConfig strictly decodes and validates one document.
func ParseTopUpDiscountConfig(data []byte) (*TopUpDiscountSettings, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config TopUpDiscountConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTopUpDiscountInvalidConfig, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: expected a single JSON object", ErrTopUpDiscountInvalidConfig)
	}
	return BuildTopUpDiscountSettings(config)
}

// BuildTopUpDiscountSettings validates a decoded document and normalizes its
// decimal strings, so the stored form is canonical.
func BuildTopUpDiscountSettings(config TopUpDiscountConfig) (*TopUpDiscountSettings, error) {
	if config.SchemaVersion != topUpDiscountSchemaVersion {
		return nil, fmt.Errorf("%w: schema_version %d", ErrTopUpDiscountSchemaUnsupported, config.SchemaVersion)
	}
	location, err := loadTopUpDiscountLocation(config.Timezone)
	if err != nil {
		return nil, err
	}
	capRate, err := parseTopUpDiscountRate("cap_rate", config.CapRate, decimal.RequireFromString(topUpDiscountMaxCapRate))
	if err != nil {
		return nil, err
	}
	fixedRate, err := parseTopUpDiscountRate("fixed_rate", config.FixedRate, capRate)
	if err != nil {
		return nil, err
	}
	settings := &TopUpDiscountSettings{Location: location, CapRate: capRate, FixedRate: fixedRate}
	tables := []struct {
		name   string
		config *TopUpDiscountTableConfig
		target *TopUpDiscountTable
	}{
		{"amount", &config.Amount, &settings.Amount},
		{"site", &config.Site, &settings.Site},
		{"personal.day", &config.Personal.Day, &settings.Day},
		{"personal.week", &config.Personal.Week, &settings.Week},
		{"personal.month", &config.Personal.Month, &settings.Month},
	}
	for _, table := range tables {
		parsed, err := parseTopUpDiscountTable(table.name, table.config, capRate)
		if err != nil {
			return nil, err
		}
		*table.target = parsed
	}
	if settings.excluded, err = parseTopUpDiscountExcluded(config.ExcludedUserIDs); err != nil {
		return nil, err
	}
	config.CapRate, config.FixedRate = capRate.String(), fixedRate.String()
	if config.ExcludedUserIDs == nil {
		config.ExcludedUserIDs = []int{}
	}
	settings.Config = config
	return settings, nil
}

func topUpDiscountInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTopUpDiscountInvalidConfig, fmt.Sprintf(format, args...))
}

func parseTopUpDiscountRate(field string, value string, max decimal.Decimal) (decimal.Decimal, error) {
	if !topUpDiscountRatePattern.MatchString(value) {
		return decimal.Zero, topUpDiscountInvalid("%s must be a decimal string with at most 4 fraction digits", field)
	}
	rate := decimal.RequireFromString(value)
	if rate.GreaterThan(max) {
		return decimal.Zero, topUpDiscountInvalid("%s must not exceed %s", field, max.String())
	}
	return rate, nil
}

func parseTopUpDiscountTable(name string, config *TopUpDiscountTableConfig, capRate decimal.Decimal) (TopUpDiscountTable, error) {
	switch config.Mode {
	case TopUpDiscountModeOff, TopUpDiscountModeShadow, TopUpDiscountModeOn:
	default:
		return TopUpDiscountTable{}, topUpDiscountInvalid("%s.mode must be off, shadow or on", name)
	}
	if len(config.Tiers) > topUpDiscountMaxTiers {
		return TopUpDiscountTable{}, topUpDiscountInvalid("%s has more than %d tiers", name, topUpDiscountMaxTiers)
	}
	table := TopUpDiscountTable{Mode: config.Mode, Tiers: make([]TopUpDiscountTier, 0, len(config.Tiers))}
	normalized := make([]TopUpDiscountTierConfig, 0, len(config.Tiers))
	for i, tier := range config.Tiers {
		if !topUpDiscountQuotaPattern.MatchString(tier.ThresholdQuota) {
			return TopUpDiscountTable{}, topUpDiscountInvalid("%s tier %d threshold_quota must be a non-negative integer string", name, i+1)
		}
		// The pattern allows at most 18 digits, which always fits int64.
		threshold, _ := strconv.ParseInt(tier.ThresholdQuota, 10, 64)
		rate, err := parseTopUpDiscountRate(fmt.Sprintf("%s tier %d rate", name, i+1), tier.Rate, capRate)
		if err != nil {
			return TopUpDiscountTable{}, err
		}
		if i > 0 {
			previous := table.Tiers[i-1]
			if threshold <= previous.Threshold {
				return TopUpDiscountTable{}, topUpDiscountInvalid("%s thresholds must strictly increase", name)
			}
			if rate.LessThan(previous.Rate) {
				return TopUpDiscountTable{}, topUpDiscountInvalid("%s rates must not decrease", name)
			}
		}
		table.Tiers = append(table.Tiers, TopUpDiscountTier{Threshold: threshold, Rate: rate})
		normalized = append(normalized, TopUpDiscountTierConfig{ThresholdQuota: tier.ThresholdQuota, Rate: rate.String()})
	}
	config.Tiers = normalized
	return table, nil
}

func loadTopUpDiscountLocation(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, topUpDiscountInvalid("timezone must be an IANA zone name")
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, topUpDiscountInvalid("unknown timezone %q", name)
	}
	return location, nil
}

// validateTopUpDiscountZoneAhead rejects zones whose local midnight is not a
// whole UTC hour within the next 366 days; windows are summed from hourly rows.
func validateTopUpDiscountZoneAhead(location *time.Location, now time.Time) error {
	local := now.In(location)
	for day := 0; day <= 366; day++ {
		midnight := time.Date(local.Year(), local.Month(), local.Day()+day, 0, 0, 0, 0, location)
		if midnight.Unix()%3600 != 0 {
			return topUpDiscountInvalid("timezone %s has a non-whole-hour midnight on %s", location.String(), midnight.Format(time.DateOnly))
		}
	}
	return nil
}

func parseTopUpDiscountExcluded(ids []int) (map[int]struct{}, error) {
	if len(ids) > topUpDiscountMaxExcluded {
		return nil, topUpDiscountInvalid("excluded_user_ids has more than %d entries", topUpDiscountMaxExcluded)
	}
	excluded := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, topUpDiscountInvalid("excluded_user_ids must be positive")
		}
		if _, duplicate := excluded[id]; duplicate {
			return nil, topUpDiscountInvalid("excluded_user_ids contains %d twice", id)
		}
		excluded[id] = struct{}{}
	}
	return excluded, nil
}

// TopUpDiscountConfigVersion is one saved configuration. The highest version
// is current; an empty table means the built-in version 0.
type TopUpDiscountConfigVersion struct {
	Version   int64  `gorm:"column:version;primaryKey;autoIncrement:false"`
	Config    string `gorm:"column:config;type:text;not null"`
	CreatedBy int    `gorm:"column:created_by;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
}

func (TopUpDiscountConfigVersion) TableName() string {
	return "topup_discount_configs"
}

var (
	topUpDiscountSettings      atomic.Pointer[TopUpDiscountSettings]
	topUpDiscountSettingsMutex sync.Mutex
)

// ResetTopUpDiscountSettingsCache drops the cached settings so the next read
// reloads them, for callers that replace the database.
func ResetTopUpDiscountSettingsCache() {
	topUpDiscountSettings.Store(nil)
}

// CurrentTopUpDiscountSettings returns the cached settings, loading them once.
// A failed load is not cached, so the next caller retries.
func CurrentTopUpDiscountSettings() (*TopUpDiscountSettings, error) {
	if settings := topUpDiscountSettings.Load(); settings != nil {
		return settings, nil
	}
	topUpDiscountSettingsMutex.Lock()
	defer topUpDiscountSettingsMutex.Unlock()
	if settings := topUpDiscountSettings.Load(); settings != nil {
		return settings, nil
	}
	settings, err := loadTopUpDiscountSettings(DB)
	if err != nil {
		return nil, err
	}
	topUpDiscountSettings.Store(settings)
	return settings, nil
}

func loadTopUpDiscountSettings(db *gorm.DB) (*TopUpDiscountSettings, error) {
	if db == nil {
		return nil, errors.New("load top-up discount config: database is not initialized")
	}
	var rows []TopUpDiscountConfigVersion
	if err := db.Order("version DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load top-up discount config: %w", err)
	}
	if len(rows) == 0 {
		return BuildTopUpDiscountSettings(DefaultTopUpDiscountConfig())
	}
	settings, err := ParseTopUpDiscountConfig([]byte(rows[0].Config))
	if err != nil {
		return nil, fmt.Errorf("stored top-up discount config version %d: %w", rows[0].Version, err)
	}
	settings.Version, settings.CreatedAt, settings.CreatedBy = rows[0].Version, rows[0].CreatedAt, rows[0].CreatedBy
	return settings, nil
}

// ValidateTopUpDiscountDraft is the full check for a new document: structure
// plus the timezone's midnight alignment over the next 366 days.
func ValidateTopUpDiscountDraft(data []byte, now time.Time) (*TopUpDiscountSettings, error) {
	settings, err := ParseTopUpDiscountConfig(data)
	if err != nil {
		return nil, err
	}
	if err := validateTopUpDiscountZoneAhead(settings.Location, now); err != nil {
		return nil, err
	}
	return settings, nil
}

// SaveTopUpDiscountConfig validates data and stores it as expectedVersion+1.
func SaveTopUpDiscountConfig(data []byte, expectedVersion int64, operatorID int, now time.Time) (*TopUpDiscountSettings, error) {
	settings, err := ValidateTopUpDiscountDraft(data, now)
	if err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(settings.Config)
	if err != nil {
		return nil, err
	}
	topUpDiscountSettingsMutex.Lock()
	defer topUpDiscountSettingsMutex.Unlock()
	settings.Version, settings.CreatedAt, settings.CreatedBy = expectedVersion+1, now.Unix(), operatorID
	err = DB.Transaction(func(tx *gorm.DB) error {
		var current int64
		if err := tx.Model(&TopUpDiscountConfigVersion{}).Select("COALESCE(MAX(version), 0)").Scan(&current).Error; err != nil {
			return err
		}
		if current != expectedVersion {
			return ErrTopUpDiscountVersionConflict
		}
		// The primary key also rejects a concurrent writer that bypassed the mutex.
		return tx.Create(&TopUpDiscountConfigVersion{
			Version: settings.Version, Config: string(encoded), CreatedBy: operatorID, CreatedAt: settings.CreatedAt,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	topUpDiscountSettings.Store(settings)
	return settings, nil
}
