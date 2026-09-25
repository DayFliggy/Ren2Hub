package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// ChangeAccessTokenForSession checks the session again in the business
// transaction. Consuming a proof earlier never authorizes a revoked session.
func ChangeAccessTokenForSession(identity AuthSessionIdentity, token *string) (string, error) {
	if token != nil && (strings.TrimSpace(*token) != *token || len(*token) < 29 || len(*token) > 32) {
		return "", ErrAuthFlowInvalid
	}
	var previous string
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var user User
		if err := tx.First(&user, identity.UserID).Error; err != nil {
			return err
		}
		previous = AccessTokenFingerprint(user.GetAccessToken())
		var createdAt *int64
		if token != nil {
			now := time.Now().Unix()
			createdAt = &now
		}
		return tx.Model(&User{}).Where("id = ?", identity.UserID).
			Updates(map[string]any{"access_token": token, "access_token_created_at": createdAt}).Error
	})
	return previous, err
}
