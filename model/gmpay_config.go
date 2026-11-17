package model

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

// Keep a blank secret update and concurrent credential rotation atomic. SQL
// diagnostics must not render this private option's credential payload.
func SaveGmpayConfig(ctx context.Context, config setting.GmpayConfig) error {
	return DB.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard}).Transaction(func(tx *gorm.DB) error {
		option := Option{Key: setting.GmpayConfigKey}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&option).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where(&Option{Key: setting.GmpayConfigKey}).First(&option).Error; err != nil {
			return err
		}
		// Surrounding whitespace is never part of a secret, and a blank
		// submission keeps the stored secret like an empty one.
		config.SecretKey = strings.TrimSpace(config.SecretKey)
		if config.SecretKey == "" && option.Value != "" {
			var old setting.GmpayConfig
			if err := common.UnmarshalJsonStr(option.Value, &old); err != nil {
				return err
			}
			config.SecretKey = old.SecretKey
		}
		if err := config.Validate(); err != nil {
			return err
		}
		value, err := common.Marshal(config)
		if err != nil {
			return err
		}
		return tx.Model(&option).Update("value", string(value)).Error
	})
}
