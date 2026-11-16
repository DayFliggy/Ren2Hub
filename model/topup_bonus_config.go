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

// Top-up bonus configuration lives in its own append-only table, never in
// options, so generic option writes and SyncOptions cannot bypass validation.
// See docs/billing/TOPUP_BONUS.md.

const (
	TopUpBonusModeOff    = "off"
	TopUpBonusModeShadow = "shadow"
	TopUpBonusModeOn     = "on"

	topUpBonusSchemaVersion = 1
	topUpBonusMaxTiers      = 10
	topUpBonusMaxExcluded   = 100
	topUpBonusMaxCapRate    = "0.5"
)

var (
	ErrTopUpBonusInvalidConfig     = errors.New("topup_bonus_invalid_config")
	ErrTopUpBonusSchemaUnsupported = errors.New("topup_bonus_schema_unsupported")
	ErrTopUpBonusVersionConflict   = errors.New("topup_bonus_version_conflict")
)

var (
	topUpBonusRatePattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,4})?$`)
	topUpBonusQuotaPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})$`)
)

// TopUpBonusConfig is the editable document. Rates and thresholds are
// decimal and integer strings so JSON numbers never round through float64.
type TopUpBonusConfig struct {
	SchemaVersion   int                      `json:"schema_version"`
	Timezone        string                   `json:"timezone"`
	CapRate         string                   `json:"cap_rate"`
	FixedRate       string                   `json:"fixed_rate"`
	Amount          TopUpBonusTableConfig    `json:"amount"`
	Site            TopUpBonusTableConfig    `json:"site"`
	Personal        TopUpBonusPersonalConfig `json:"personal"`
	ExcludedUserIDs []int                    `json:"excluded_user_ids"`
}

type TopUpBonusPersonalConfig struct {
	Day   TopUpBonusTableConfig `json:"day"`
	Week  TopUpBonusTableConfig `json:"week"`
	Month TopUpBonusTableConfig `json:"month"`
}

type TopUpBonusTableConfig struct {
	Mode  string                 `json:"mode"`
	Tiers []TopUpBonusTierConfig `json:"tiers"`
}

type TopUpBonusTierConfig struct {
	ThresholdQuota string `json:"threshold_quota"`
	Rate           string `json:"rate"`
}

// TopUpBonusTier and TopUpBonusTable are the validated runtime form.
type TopUpBonusTier struct {
	Threshold int64
	Rate      decimal.Decimal
}

type TopUpBonusTable struct {
	Mode  string
	Tiers []TopUpBonusTier
}

// TopUpBonusSettings is one validated, immutable configuration version.
type TopUpBonusSettings struct {
	Version   int64
	CreatedAt int64
	CreatedBy int
	Config    TopUpBonusConfig
	Location  *time.Location
	CapRate   decimal.Decimal
	FixedRate decimal.Decimal
	Amount    TopUpBonusTable
	Site      TopUpBonusTable
	Day       TopUpBonusTable
	Week      TopUpBonusTable
	Month     TopUpBonusTable
	excluded  map[int]struct{}
}

func (s *TopUpBonusSettings) IsExcluded(userID int) bool {
	_, ok := s.excluded[userID]
	return ok
}

// Enabled reports whether any component is computed, including shadow ones.
func (s *TopUpBonusSettings) Enabled() bool {
	return s.FixedRate.IsPositive() || s.Amount.Mode != TopUpBonusModeOff || s.UsesSite() || s.UsesWindows()
}

// Active reports whether any component can grant a bonus.
func (s *TopUpBonusSettings) Active() bool {
	if s.FixedRate.IsPositive() {
		return true
	}
	for _, table := range []TopUpBonusTable{s.Amount, s.Site, s.Day, s.Week, s.Month} {
		if table.Mode == TopUpBonusModeOn {
			return true
		}
	}
	return false
}

func (s *TopUpBonusSettings) UsesSite() bool {
	return s.Site.Mode != TopUpBonusModeOff
}

// PersonalOn reports whether a personal window can grant a bonus.
func (s *TopUpBonusSettings) PersonalOn() bool {
	return s.Day.Mode == TopUpBonusModeOn || s.Week.Mode == TopUpBonusModeOn || s.Month.Mode == TopUpBonusModeOn
}

func (s *TopUpBonusSettings) UsesWindows() bool {
	return s.Day.Mode != TopUpBonusModeOff || s.Week.Mode != TopUpBonusModeOff || s.Month.Mode != TopUpBonusModeOff
}

// DefaultTopUpBonusConfig is version 0, used while the table is empty.
// Thresholds assume the standard 500000 quota per USD: $5/$15/$50/$150/$500
// per day, half the month value per week, and $25…$2,500 per month.
func DefaultTopUpBonusConfig() TopUpBonusConfig {
	personal := func(thresholds ...string) TopUpBonusTableConfig {
		tiers := make([]TopUpBonusTierConfig, len(thresholds))
		for i, threshold := range thresholds {
			tiers[i] = TopUpBonusTierConfig{ThresholdQuota: threshold, Rate: fmt.Sprintf("0.0%d", i+1)}
		}
		return TopUpBonusTableConfig{Mode: TopUpBonusModeShadow, Tiers: tiers}
	}
	return TopUpBonusConfig{
		SchemaVersion: topUpBonusSchemaVersion,
		Timezone:      "Asia/Shanghai",
		CapRate:       "0.05",
		FixedRate:     "0",
		Amount:        TopUpBonusTableConfig{Mode: TopUpBonusModeOff, Tiers: []TopUpBonusTierConfig{}},
		Site:          TopUpBonusTableConfig{Mode: TopUpBonusModeOff, Tiers: []TopUpBonusTierConfig{}},
		Personal: TopUpBonusPersonalConfig{
			Day:   personal("2500000", "7500000", "25000000", "75000000", "250000000"),
			Week:  personal("6250000", "18750000", "62500000", "187500000", "625000000"),
			Month: personal("12500000", "37500000", "125000000", "375000000", "1250000000"),
		},
		ExcludedUserIDs: []int{1},
	}
}

// ParseTopUpBonusConfig strictly decodes and validates one document.
func ParseTopUpBonusConfig(data []byte) (*TopUpBonusSettings, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config TopUpBonusConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTopUpBonusInvalidConfig, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: expected a single JSON object", ErrTopUpBonusInvalidConfig)
	}
	return BuildTopUpBonusSettings(config)
}

// BuildTopUpBonusSettings validates a decoded document and normalizes its
// decimal strings, so the stored form is canonical.
func BuildTopUpBonusSettings(config TopUpBonusConfig) (*TopUpBonusSettings, error) {
	if config.SchemaVersion != topUpBonusSchemaVersion {
		return nil, fmt.Errorf("%w: schema_version %d", ErrTopUpBonusSchemaUnsupported, config.SchemaVersion)
	}
	location, err := loadTopUpBonusLocation(config.Timezone)
	if err != nil {
		return nil, err
	}
	capRate, err := parseTopUpBonusRate("cap_rate", config.CapRate, decimal.RequireFromString(topUpBonusMaxCapRate))
	if err != nil {
		return nil, err
	}
	fixedRate, err := parseTopUpBonusRate("fixed_rate", config.FixedRate, capRate)
	if err != nil {
		return nil, err
	}
	settings := &TopUpBonusSettings{Location: location, CapRate: capRate, FixedRate: fixedRate}
	tables := []struct {
		name   string
		config *TopUpBonusTableConfig
		target *TopUpBonusTable
	}{
		{"amount", &config.Amount, &settings.Amount},
		{"site", &config.Site, &settings.Site},
		{"personal.day", &config.Personal.Day, &settings.Day},
		{"personal.week", &config.Personal.Week, &settings.Week},
		{"personal.month", &config.Personal.Month, &settings.Month},
	}
	for _, table := range tables {
		parsed, err := parseTopUpBonusTable(table.name, table.config, capRate)
		if err != nil {
			return nil, err
		}
		*table.target = parsed
	}
	if settings.excluded, err = parseTopUpBonusExcluded(config.ExcludedUserIDs); err != nil {
		return nil, err
	}
	config.CapRate, config.FixedRate = capRate.String(), fixedRate.String()
	if config.ExcludedUserIDs == nil {
		config.ExcludedUserIDs = []int{}
	}
	settings.Config = config
	return settings, nil
}

func topUpBonusInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTopUpBonusInvalidConfig, fmt.Sprintf(format, args...))
}

func parseTopUpBonusRate(field string, value string, max decimal.Decimal) (decimal.Decimal, error) {
	if !topUpBonusRatePattern.MatchString(value) {
		return decimal.Zero, topUpBonusInvalid("%s must be a decimal string with at most 4 fraction digits", field)
	}
	rate := decimal.RequireFromString(value)
	if rate.GreaterThan(max) {
		return decimal.Zero, topUpBonusInvalid("%s must not exceed %s", field, max.String())
	}
	return rate, nil
}

func parseTopUpBonusTable(name string, config *TopUpBonusTableConfig, capRate decimal.Decimal) (TopUpBonusTable, error) {
	switch config.Mode {
	case TopUpBonusModeOff, TopUpBonusModeShadow, TopUpBonusModeOn:
	default:
		return TopUpBonusTable{}, topUpBonusInvalid("%s.mode must be off, shadow or on", name)
	}
	if len(config.Tiers) > topUpBonusMaxTiers {
		return TopUpBonusTable{}, topUpBonusInvalid("%s has more than %d tiers", name, topUpBonusMaxTiers)
	}
	table := TopUpBonusTable{Mode: config.Mode, Tiers: make([]TopUpBonusTier, 0, len(config.Tiers))}
	normalized := make([]TopUpBonusTierConfig, 0, len(config.Tiers))
	for i, tier := range config.Tiers {
		if !topUpBonusQuotaPattern.MatchString(tier.ThresholdQuota) {
			return TopUpBonusTable{}, topUpBonusInvalid("%s tier %d threshold_quota must be a non-negative integer string", name, i+1)
		}
		// The pattern allows at most 18 digits, which always fits int64.
		threshold, _ := strconv.ParseInt(tier.ThresholdQuota, 10, 64)
		rate, err := parseTopUpBonusRate(fmt.Sprintf("%s tier %d rate", name, i+1), tier.Rate, capRate)
		if err != nil {
			return TopUpBonusTable{}, err
		}
		if i > 0 {
			previous := table.Tiers[i-1]
			if threshold <= previous.Threshold {
				return TopUpBonusTable{}, topUpBonusInvalid("%s thresholds must strictly increase", name)
			}
			if rate.LessThan(previous.Rate) {
				return TopUpBonusTable{}, topUpBonusInvalid("%s rates must not decrease", name)
			}
		}
		table.Tiers = append(table.Tiers, TopUpBonusTier{Threshold: threshold, Rate: rate})
		normalized = append(normalized, TopUpBonusTierConfig{ThresholdQuota: tier.ThresholdQuota, Rate: rate.String()})
	}
	config.Tiers = normalized
	return table, nil
}

func loadTopUpBonusLocation(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, topUpBonusInvalid("timezone must be an IANA zone name")
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, topUpBonusInvalid("unknown timezone %q", name)
	}
	return location, nil
}

// validateTopUpBonusZoneAhead rejects zones whose local midnight is not a
// whole UTC hour within the next 366 days; windows are summed from hourly rows.
func validateTopUpBonusZoneAhead(location *time.Location, now time.Time) error {
	local := now.In(location)
	for day := 0; day <= 366; day++ {
		midnight := time.Date(local.Year(), local.Month(), local.Day()+day, 0, 0, 0, 0, location)
		if midnight.Unix()%3600 != 0 {
			return topUpBonusInvalid("timezone %s has a non-whole-hour midnight on %s", location.String(), midnight.Format(time.DateOnly))
		}
	}
	return nil
}

func parseTopUpBonusExcluded(ids []int) (map[int]struct{}, error) {
	if len(ids) > topUpBonusMaxExcluded {
		return nil, topUpBonusInvalid("excluded_user_ids has more than %d entries", topUpBonusMaxExcluded)
	}
	excluded := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, topUpBonusInvalid("excluded_user_ids must be positive")
		}
		if _, duplicate := excluded[id]; duplicate {
			return nil, topUpBonusInvalid("excluded_user_ids contains %d twice", id)
		}
		excluded[id] = struct{}{}
	}
	return excluded, nil
}

// TopUpBonusConfigVersion is one saved configuration. The highest version
// is current; an empty table means the built-in version 0.
type TopUpBonusConfigVersion struct {
	Version   int64  `gorm:"column:version;primaryKey;autoIncrement:false"`
	Config    string `gorm:"column:config;type:text;not null"`
	CreatedBy int    `gorm:"column:created_by;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
}

func (TopUpBonusConfigVersion) TableName() string {
	return "topup_bonus_configs"
}

var (
	topUpBonusSettings      atomic.Pointer[TopUpBonusSettings]
	topUpBonusSettingsMutex sync.Mutex
)

// ResetTopUpBonusSettingsCache drops the cached settings so the next read
// reloads them, for callers that replace the database.
func ResetTopUpBonusSettingsCache() {
	topUpBonusSettings.Store(nil)
}

// CurrentTopUpBonusSettings returns the cached settings, loading them once.
// A failed load is not cached, so the next caller retries.
func CurrentTopUpBonusSettings() (*TopUpBonusSettings, error) {
	if settings := topUpBonusSettings.Load(); settings != nil {
		return settings, nil
	}
	topUpBonusSettingsMutex.Lock()
	defer topUpBonusSettingsMutex.Unlock()
	if settings := topUpBonusSettings.Load(); settings != nil {
		return settings, nil
	}
	settings, err := loadTopUpBonusSettings(DB)
	if err != nil {
		return nil, err
	}
	topUpBonusSettings.Store(settings)
	return settings, nil
}

func loadTopUpBonusSettings(db *gorm.DB) (*TopUpBonusSettings, error) {
	if db == nil {
		return nil, errors.New("load top-up bonus config: database is not initialized")
	}
	var rows []TopUpBonusConfigVersion
	if err := db.Order("version DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load top-up bonus config: %w", err)
	}
	if len(rows) == 0 {
		return BuildTopUpBonusSettings(DefaultTopUpBonusConfig())
	}
	settings, err := ParseTopUpBonusConfig([]byte(rows[0].Config))
	if err != nil {
		return nil, fmt.Errorf("stored top-up bonus config version %d: %w", rows[0].Version, err)
	}
	settings.Version, settings.CreatedAt, settings.CreatedBy = rows[0].Version, rows[0].CreatedAt, rows[0].CreatedBy
	return settings, nil
}

// ValidateTopUpBonusDraft is the full check for a new document: structure
// plus the timezone's midnight alignment over the next 366 days.
func ValidateTopUpBonusDraft(data []byte, now time.Time) (*TopUpBonusSettings, error) {
	settings, err := ParseTopUpBonusConfig(data)
	if err != nil {
		return nil, err
	}
	if err := validateTopUpBonusZoneAhead(settings.Location, now); err != nil {
		return nil, err
	}
	return settings, nil
}

// SaveTopUpBonusConfig validates data and stores it as expectedVersion+1.
func SaveTopUpBonusConfig(data []byte, expectedVersion int64, operatorID int, now time.Time) (*TopUpBonusSettings, error) {
	settings, err := ValidateTopUpBonusDraft(data, now)
	if err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(settings.Config)
	if err != nil {
		return nil, err
	}
	topUpBonusSettingsMutex.Lock()
	defer topUpBonusSettingsMutex.Unlock()
	settings.Version, settings.CreatedAt, settings.CreatedBy = expectedVersion+1, now.Unix(), operatorID
	err = DB.Transaction(func(tx *gorm.DB) error {
		var current int64
		if err := tx.Model(&TopUpBonusConfigVersion{}).Select("COALESCE(MAX(version), 0)").Scan(&current).Error; err != nil {
			return err
		}
		if current != expectedVersion {
			return ErrTopUpBonusVersionConflict
		}
		// The primary key also rejects a concurrent writer that bypassed the mutex.
		return tx.Create(&TopUpBonusConfigVersion{
			Version: settings.Version, Config: string(encoded), CreatedBy: operatorID, CreatedAt: settings.CreatedAt,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	topUpBonusSettings.Store(settings)
	return settings, nil
}
