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

// Usage discount configuration lives in its own append-only table, never in
// options, so generic option writes and SyncOptions cannot bypass validation.
// See docs/billing/USAGE_DISCOUNT.md.

const (
	UsageDiscountModeOff    = "off"
	UsageDiscountModeShadow = "shadow"
	UsageDiscountModeOn     = "on"

	usageDiscountSchemaVersion = 1
	usageDiscountMaxTiers      = 10
	usageDiscountMaxExcluded   = 100
	usageDiscountMaxCapRate    = "0.5"
)

var (
	ErrUsageDiscountInvalidConfig     = errors.New("usage_discount_invalid_config")
	ErrUsageDiscountSchemaUnsupported = errors.New("usage_discount_schema_unsupported")
	ErrUsageDiscountVersionConflict   = errors.New("usage_discount_version_conflict")
)

var (
	usageDiscountRatePattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,4})?$`)
	usageDiscountQuotaPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})$`)
)

// UsageDiscountConfig is the editable document. Rates and thresholds are
// decimal and integer strings so JSON numbers never round through float64.
type UsageDiscountConfig struct {
	SchemaVersion   int                         `json:"schema_version"`
	Timezone        string                      `json:"timezone"`
	CapRate         string                      `json:"cap_rate"`
	FixedRate       string                      `json:"fixed_rate"`
	Site            UsageDiscountTableConfig    `json:"site"`
	Personal        UsageDiscountPersonalConfig `json:"personal"`
	ExcludedUserIDs []int                       `json:"excluded_user_ids"`
}

type UsageDiscountPersonalConfig struct {
	Day   UsageDiscountTableConfig `json:"day"`
	Week  UsageDiscountTableConfig `json:"week"`
	Month UsageDiscountTableConfig `json:"month"`
}

type UsageDiscountTableConfig struct {
	Mode  string                    `json:"mode"`
	Tiers []UsageDiscountTierConfig `json:"tiers"`
}

type UsageDiscountTierConfig struct {
	ThresholdQuota string `json:"threshold_quota"`
	Rate           string `json:"rate"`
}

// UsageDiscountTier and UsageDiscountTable are the validated runtime form.
type UsageDiscountTier struct {
	Threshold int64
	Rate      decimal.Decimal
}

type UsageDiscountTable struct {
	Mode  string
	Tiers []UsageDiscountTier
}

// UsageDiscountSettings is one validated, immutable configuration version.
type UsageDiscountSettings struct {
	Version   int64
	CreatedAt int64
	CreatedBy int
	Config    UsageDiscountConfig
	Location  *time.Location
	CapRate   decimal.Decimal
	FixedRate decimal.Decimal
	Site      UsageDiscountTable
	Day       UsageDiscountTable
	Week      UsageDiscountTable
	Month     UsageDiscountTable
	excluded  map[int]struct{}
}

func (s *UsageDiscountSettings) IsExcluded(userID int) bool {
	_, ok := s.excluded[userID]
	return ok
}

// Enabled reports whether any component is computed, including shadow ones.
func (s *UsageDiscountSettings) Enabled() bool {
	return s.FixedRate.IsPositive() || s.UsesSite() || s.UsesWindows()
}

// Active reports whether any component can change the price.
func (s *UsageDiscountSettings) Active() bool {
	if s.FixedRate.IsPositive() {
		return true
	}
	for _, table := range []UsageDiscountTable{s.Site, s.Day, s.Week, s.Month} {
		if table.Mode == UsageDiscountModeOn {
			return true
		}
	}
	return false
}

func (s *UsageDiscountSettings) UsesSite() bool {
	return s.Site.Mode != UsageDiscountModeOff
}

func (s *UsageDiscountSettings) UsesWindows() bool {
	return s.Day.Mode != UsageDiscountModeOff || s.Week.Mode != UsageDiscountModeOff || s.Month.Mode != UsageDiscountModeOff
}

// DefaultUsageDiscountConfig is version 0, used while the table is empty.
// Thresholds assume the standard 500000 quota per USD: $5/$15/$50/$150/$500
// per day, half the month value per week, and $25…$2,500 per month.
func DefaultUsageDiscountConfig() UsageDiscountConfig {
	personal := func(thresholds ...string) UsageDiscountTableConfig {
		tiers := make([]UsageDiscountTierConfig, len(thresholds))
		for i, threshold := range thresholds {
			tiers[i] = UsageDiscountTierConfig{ThresholdQuota: threshold, Rate: fmt.Sprintf("0.0%d", i+1)}
		}
		return UsageDiscountTableConfig{Mode: UsageDiscountModeShadow, Tiers: tiers}
	}
	return UsageDiscountConfig{
		SchemaVersion: usageDiscountSchemaVersion,
		Timezone:      "Asia/Shanghai",
		CapRate:       "0.05",
		FixedRate:     "0",
		Site:          UsageDiscountTableConfig{Mode: UsageDiscountModeOff, Tiers: []UsageDiscountTierConfig{}},
		Personal: UsageDiscountPersonalConfig{
			Day:   personal("2500000", "7500000", "25000000", "75000000", "250000000"),
			Week:  personal("6250000", "18750000", "62500000", "187500000", "625000000"),
			Month: personal("12500000", "37500000", "125000000", "375000000", "1250000000"),
		},
		ExcludedUserIDs: []int{1},
	}
}

// ParseUsageDiscountConfig strictly decodes and validates one document.
func ParseUsageDiscountConfig(data []byte) (*UsageDiscountSettings, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config UsageDiscountConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUsageDiscountInvalidConfig, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: expected a single JSON object", ErrUsageDiscountInvalidConfig)
	}
	return BuildUsageDiscountSettings(config)
}

// BuildUsageDiscountSettings validates a decoded document and normalizes its
// decimal strings, so the stored form is canonical.
func BuildUsageDiscountSettings(config UsageDiscountConfig) (*UsageDiscountSettings, error) {
	if config.SchemaVersion != usageDiscountSchemaVersion {
		return nil, fmt.Errorf("%w: schema_version %d", ErrUsageDiscountSchemaUnsupported, config.SchemaVersion)
	}
	location, err := loadUsageDiscountLocation(config.Timezone)
	if err != nil {
		return nil, err
	}
	capRate, err := parseUsageDiscountRate("cap_rate", config.CapRate, decimal.RequireFromString(usageDiscountMaxCapRate))
	if err != nil {
		return nil, err
	}
	fixedRate, err := parseUsageDiscountRate("fixed_rate", config.FixedRate, capRate)
	if err != nil {
		return nil, err
	}
	settings := &UsageDiscountSettings{Location: location, CapRate: capRate, FixedRate: fixedRate}
	tables := []struct {
		name   string
		config *UsageDiscountTableConfig
		target *UsageDiscountTable
	}{
		{"site", &config.Site, &settings.Site},
		{"personal.day", &config.Personal.Day, &settings.Day},
		{"personal.week", &config.Personal.Week, &settings.Week},
		{"personal.month", &config.Personal.Month, &settings.Month},
	}
	for _, table := range tables {
		parsed, err := parseUsageDiscountTable(table.name, table.config, capRate)
		if err != nil {
			return nil, err
		}
		*table.target = parsed
	}
	if settings.excluded, err = parseUsageDiscountExcluded(config.ExcludedUserIDs); err != nil {
		return nil, err
	}
	config.CapRate, config.FixedRate = capRate.String(), fixedRate.String()
	if config.ExcludedUserIDs == nil {
		config.ExcludedUserIDs = []int{}
	}
	settings.Config = config
	return settings, nil
}

func usageDiscountInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUsageDiscountInvalidConfig, fmt.Sprintf(format, args...))
}

func parseUsageDiscountRate(field string, value string, max decimal.Decimal) (decimal.Decimal, error) {
	if !usageDiscountRatePattern.MatchString(value) {
		return decimal.Zero, usageDiscountInvalid("%s must be a decimal string with at most 4 fraction digits", field)
	}
	rate := decimal.RequireFromString(value)
	if rate.GreaterThan(max) {
		return decimal.Zero, usageDiscountInvalid("%s must not exceed %s", field, max.String())
	}
	return rate, nil
}

func parseUsageDiscountTable(name string, config *UsageDiscountTableConfig, capRate decimal.Decimal) (UsageDiscountTable, error) {
	switch config.Mode {
	case UsageDiscountModeOff, UsageDiscountModeShadow, UsageDiscountModeOn:
	default:
		return UsageDiscountTable{}, usageDiscountInvalid("%s.mode must be off, shadow or on", name)
	}
	if len(config.Tiers) > usageDiscountMaxTiers {
		return UsageDiscountTable{}, usageDiscountInvalid("%s has more than %d tiers", name, usageDiscountMaxTiers)
	}
	table := UsageDiscountTable{Mode: config.Mode, Tiers: make([]UsageDiscountTier, 0, len(config.Tiers))}
	normalized := make([]UsageDiscountTierConfig, 0, len(config.Tiers))
	for i, tier := range config.Tiers {
		if !usageDiscountQuotaPattern.MatchString(tier.ThresholdQuota) {
			return UsageDiscountTable{}, usageDiscountInvalid("%s tier %d threshold_quota must be a non-negative integer string", name, i+1)
		}
		// The pattern allows at most 18 digits, which always fits int64.
		threshold, _ := strconv.ParseInt(tier.ThresholdQuota, 10, 64)
		rate, err := parseUsageDiscountRate(fmt.Sprintf("%s tier %d rate", name, i+1), tier.Rate, capRate)
		if err != nil {
			return UsageDiscountTable{}, err
		}
		if i > 0 {
			previous := table.Tiers[i-1]
			if threshold <= previous.Threshold {
				return UsageDiscountTable{}, usageDiscountInvalid("%s thresholds must strictly increase", name)
			}
			if rate.LessThan(previous.Rate) {
				return UsageDiscountTable{}, usageDiscountInvalid("%s rates must not decrease", name)
			}
		}
		table.Tiers = append(table.Tiers, UsageDiscountTier{Threshold: threshold, Rate: rate})
		normalized = append(normalized, UsageDiscountTierConfig{ThresholdQuota: tier.ThresholdQuota, Rate: rate.String()})
	}
	config.Tiers = normalized
	return table, nil
}

func loadUsageDiscountLocation(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, usageDiscountInvalid("timezone must be an IANA zone name")
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, usageDiscountInvalid("unknown timezone %q", name)
	}
	return location, nil
}

// validateUsageDiscountZoneAhead rejects zones whose local midnight is not a
// whole UTC hour within the next 366 days; windows are summed from hourly rows.
func validateUsageDiscountZoneAhead(location *time.Location, now time.Time) error {
	local := now.In(location)
	for day := 0; day <= 366; day++ {
		midnight := time.Date(local.Year(), local.Month(), local.Day()+day, 0, 0, 0, 0, location)
		if midnight.Unix()%3600 != 0 {
			return usageDiscountInvalid("timezone %s has a non-whole-hour midnight on %s", location.String(), midnight.Format(time.DateOnly))
		}
	}
	return nil
}

func parseUsageDiscountExcluded(ids []int) (map[int]struct{}, error) {
	if len(ids) > usageDiscountMaxExcluded {
		return nil, usageDiscountInvalid("excluded_user_ids has more than %d entries", usageDiscountMaxExcluded)
	}
	excluded := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, usageDiscountInvalid("excluded_user_ids must be positive")
		}
		if _, duplicate := excluded[id]; duplicate {
			return nil, usageDiscountInvalid("excluded_user_ids contains %d twice", id)
		}
		excluded[id] = struct{}{}
	}
	return excluded, nil
}

// UsageDiscountConfigVersion is one saved configuration. The highest version
// is current; an empty table means the built-in version 0.
type UsageDiscountConfigVersion struct {
	Version   int64  `gorm:"column:version;primaryKey;autoIncrement:false"`
	Config    string `gorm:"column:config;type:text;not null"`
	CreatedBy int    `gorm:"column:created_by;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
}

func (UsageDiscountConfigVersion) TableName() string {
	return "usage_discount_configs"
}

var (
	usageDiscountSettings      atomic.Pointer[UsageDiscountSettings]
	usageDiscountSettingsMutex sync.Mutex
)

// ResetUsageDiscountSettingsCache drops the cached settings so the next read
// reloads them, for callers that replace the database.
func ResetUsageDiscountSettingsCache() {
	usageDiscountSettings.Store(nil)
}

// CurrentUsageDiscountSettings returns the cached settings, loading them once.
// A failed load is not cached, so the next caller retries.
func CurrentUsageDiscountSettings() (*UsageDiscountSettings, error) {
	if settings := usageDiscountSettings.Load(); settings != nil {
		return settings, nil
	}
	usageDiscountSettingsMutex.Lock()
	defer usageDiscountSettingsMutex.Unlock()
	if settings := usageDiscountSettings.Load(); settings != nil {
		return settings, nil
	}
	settings, err := loadUsageDiscountSettings(DB)
	if err != nil {
		return nil, err
	}
	usageDiscountSettings.Store(settings)
	return settings, nil
}

func loadUsageDiscountSettings(db *gorm.DB) (*UsageDiscountSettings, error) {
	if db == nil {
		return nil, errors.New("load usage discount config: database is not initialized")
	}
	var rows []UsageDiscountConfigVersion
	if err := db.Order("version DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage discount config: %w", err)
	}
	if len(rows) == 0 {
		return BuildUsageDiscountSettings(DefaultUsageDiscountConfig())
	}
	settings, err := ParseUsageDiscountConfig([]byte(rows[0].Config))
	if err != nil {
		return nil, fmt.Errorf("stored usage discount config version %d: %w", rows[0].Version, err)
	}
	settings.Version, settings.CreatedAt, settings.CreatedBy = rows[0].Version, rows[0].CreatedAt, rows[0].CreatedBy
	return settings, nil
}

// ValidateUsageDiscountDraft is the full check for a new document: structure
// plus the timezone's midnight alignment over the next 366 days.
func ValidateUsageDiscountDraft(data []byte, now time.Time) (*UsageDiscountSettings, error) {
	settings, err := ParseUsageDiscountConfig(data)
	if err != nil {
		return nil, err
	}
	if err := validateUsageDiscountZoneAhead(settings.Location, now); err != nil {
		return nil, err
	}
	return settings, nil
}

// SaveUsageDiscountConfig validates data and stores it as expectedVersion+1.
func SaveUsageDiscountConfig(data []byte, expectedVersion int64, operatorID int, now time.Time) (*UsageDiscountSettings, error) {
	settings, err := ValidateUsageDiscountDraft(data, now)
	if err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(settings.Config)
	if err != nil {
		return nil, err
	}
	usageDiscountSettingsMutex.Lock()
	defer usageDiscountSettingsMutex.Unlock()
	settings.Version, settings.CreatedAt, settings.CreatedBy = expectedVersion+1, now.Unix(), operatorID
	err = DB.Transaction(func(tx *gorm.DB) error {
		var current int64
		if err := tx.Model(&UsageDiscountConfigVersion{}).Select("COALESCE(MAX(version), 0)").Scan(&current).Error; err != nil {
			return err
		}
		if current != expectedVersion {
			return ErrUsageDiscountVersionConflict
		}
		// The primary key also rejects a concurrent writer that bypassed the mutex.
		return tx.Create(&UsageDiscountConfigVersion{
			Version: settings.Version, Config: string(encoded), CreatedBy: operatorID, CreatedAt: settings.CreatedAt,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	usageDiscountSettings.Store(settings)
	return settings, nil
}
