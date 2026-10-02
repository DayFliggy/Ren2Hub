package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

var (
	ErrTokenCountLimit        = errors.New("user token count limit reached")
	ErrTokenCreationRateLimit = errors.New("user token creation rate limit reached")
)

// checkTokenCreationLimits must run while the creating transaction holds the
// user's row lock. Database errors reject creation rather than skipping limits.
func checkTokenCreationLimits(tx *gorm.DB, userID int, now int64) error {
	settings := *operation_setting.GetTokenSetting()
	var active int64
	if err := tx.Model(&Token{}).Where("user_id = ?", userID).Count(&active).Error; err != nil {
		return err
	}
	if active >= int64(settings.MaxUserTokens) {
		return fmt.Errorf("%w (%d)", ErrTokenCountLimit, settings.MaxUserTokens)
	}
	if settings.MaxTokenCreatesPerHour < 0 {
		return errors.New("invalid token creation rate limit")
	}
	if settings.MaxTokenCreatesPerHour == 0 {
		return nil
	}
	var recent int64
	if err := tx.Unscoped().Model(&Token{}).Where("user_id = ? AND created_time > ?", userID, now-3600).Count(&recent).Error; err != nil {
		return err
	}
	if recent >= int64(settings.MaxTokenCreatesPerHour) {
		return fmt.Errorf("%w (%d/hour)", ErrTokenCreationRateLimit, settings.MaxTokenCreatesPerHour)
	}
	return nil
}
