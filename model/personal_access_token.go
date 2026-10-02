package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const PersonalAccessTokenLimit = 20

var (
	ErrPATInvalid  = errors.New("personal access token is invalid")
	ErrPATInput    = errors.New("invalid personal access token name, scopes or expiry")
	ErrPATLimit    = errors.New("personal access token limit reached")
	ErrPATNotFound = errors.New("personal access token was not found or is no longer active")
)

// PATScopeRoles is the complete scope vocabulary. Resource permissions are
// intersected with the owner's current authorization by the service layer.
var PATScopeRoles = map[string]int{
	"profile:read": common.RoleCommonUser, "tokens:read": common.RoleCommonUser,
	"tokens:write": common.RoleCommonUser, "routing:read": common.RoleCommonUser,
	"routing:write": common.RoleCommonUser, "billing:read": common.RoleCommonUser,
	"logs:read": common.RoleCommonUser, "verification:write": common.RoleCommonUser,
	"users:read": common.RoleAdminUser, "users:write": common.RoleAdminUser,
	"channels:read": common.RoleAdminUser, "channels:write": common.RoleAdminUser,
	"channels:operate": common.RoleAdminUser, "channels:sensitive_write": common.RoleAdminUser,
	"channels:key_read": common.RoleRootUser, "models:read": common.RoleAdminUser,
	"models:write": common.RoleAdminUser, "redemptions:read": common.RoleAdminUser,
	"redemptions:write": common.RoleAdminUser, "options:read": common.RoleRootUser,
	"options:write": common.RoleRootUser, "audit:read": common.RoleAdminUser,
	"ops:read": common.RoleAdminUser, "tickets:read": common.RoleAdminUser,
	"tickets:reply": common.RoleAdminUser, "tickets:manage": common.RoleAdminUser,
	"plugins:read": common.RoleRootUser, "plugins:write": common.RoleRootUser,
	"plugins:bind": common.RoleAdminUser,
}

type PersonalAccessToken struct {
	ID         int      `json:"id" gorm:"primaryKey"`
	UserID     int      `json:"-" gorm:"not null;index:idx_pat_owner_active,priority:1"`
	Name       string   `json:"name" gorm:"type:varchar(100);not null"`
	Digest     string   `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	Scopes     []string `json:"scopes" gorm:"serializer:json;type:text;not null"`
	CreatedAt  int64    `json:"created_at" gorm:"type:bigint;not null"`
	LastUsedAt int64    `json:"last_used_at" gorm:"type:bigint;not null"`
	ExpiresAt  int64    `json:"expires_at" gorm:"type:bigint;not null;index:idx_pat_owner_active,priority:3"`
	RevokedAt  int64    `json:"revoked_at" gorm:"type:bigint;not null;index:idx_pat_owner_active,priority:2"`
}

func PersonalAccessTokenDigest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func NormalizePATName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", ErrPATInput
	}
	return name, nil
}

func NormalizePATScopes(scopes []string, role int) ([]string, error) {
	if len(scopes) == 0 || len(scopes) > len(PATScopeRoles) || !common.IsValidateRole(role) {
		return nil, ErrPATInput
	}
	for _, scope := range scopes {
		minimum, known := PATScopeRoles[scope]
		if !known || role < minimum {
			return nil, ErrPATInput
		}
	}
	scopes = slices.Clone(scopes)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)
	return scopes, nil
}

func NormalizePATInput(name string, scopes []string, expiresAt int64, role int) (string, []string, int64, error) {
	name, err := NormalizePATName(name)
	if err != nil {
		return "", nil, 0, err
	}
	scopes, err = NormalizePATScopes(scopes, role)
	if err != nil {
		return "", nil, 0, err
	}
	now := time.Now()
	if expiresAt == 0 {
		expiresAt = now.Add(30 * 24 * time.Hour).Unix()
	}
	if expiresAt <= now.Unix() {
		return "", nil, 0, ErrPATInput
	}
	return name, scopes, expiresAt, nil
}

func CreatePersonalAccessToken(identity AuthSessionIdentity, name string, scopes []string, expiresAt int64, authorize ...func(*gorm.DB, *User, []string) error) (*PersonalAccessToken, string, error) {
	if identity.PersonalAccessTokenID != 0 {
		return nil, "", ErrPATInvalid
	}
	random, err := common.GenerateRandomCharsKey(48)
	if err != nil {
		return nil, "", err
	}
	raw := "pat_" + random
	token := &PersonalAccessToken{UserID: identity.UserID, Digest: PersonalAccessTokenDigest(raw), CreatedAt: time.Now().Unix()}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var owner User
		if err := tx.First(&owner, identity.UserID).Error; err != nil {
			return err
		}
		var err error
		token.Name, token.Scopes, token.ExpiresAt, err = NormalizePATInput(name, scopes, expiresAt, owner.Role)
		if err != nil {
			return err
		}
		for _, check := range authorize {
			if err := check(tx, &owner, token.Scopes); err != nil {
				return err
			}
		}
		var count int64
		if err := tx.Model(&PersonalAccessToken{}).Where("user_id = ? AND revoked_at = 0 AND expires_at > ?", identity.UserID, time.Now().Unix()).Count(&count).Error; err != nil {
			return err
		}
		if count >= PersonalAccessTokenLimit {
			return ErrPATLimit
		}
		return tx.Create(token).Error
	})
	if err != nil {
		return nil, "", err
	}
	return token, raw, nil
}

func ListPersonalAccessTokens(userID int) ([]PersonalAccessToken, error) {
	items := []PersonalAccessToken{}
	err := DB.Where("user_id = ?", userID).Order("id DESC").Find(&items).Error
	return items, err
}

func GetActivePersonalAccessToken(userID, tokenID int) (*PersonalAccessToken, error) {
	var token PersonalAccessToken
	err := DB.Where("id = ? AND user_id = ? AND revoked_at = 0 AND expires_at > ?", tokenID, userID, time.Now().Unix()).First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPATNotFound
	}
	return &token, err
}

// Omitted fields stay unchanged under the owner lock. Supplied grants are
// rechecked inside the same transaction, without extending the token's lifetime.
func UpdatePersonalAccessToken(identity AuthSessionIdentity, tokenID int, name *string, scopes []string, authorize func(*gorm.DB, *User, []string) error) (*PersonalAccessToken, error) {
	if identity.PersonalAccessTokenID != 0 || tokenID <= 0 {
		return nil, ErrPATInvalid
	}
	if name == nil && scopes == nil {
		return nil, ErrPATInput
	}
	if name != nil {
		normalized, err := NormalizePATName(*name)
		if err != nil {
			return nil, err
		}
		name = &normalized
	}
	var token PersonalAccessToken
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ? AND revoked_at = 0 AND expires_at > ?", tokenID, identity.UserID, time.Now().Unix()).First(&token).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPATNotFound
			}
			return err
		}
		changes := map[string]any{}
		if name != nil {
			changes["name"] = *name
		}
		if scopes != nil {
			var owner User
			if err := tx.First(&owner, identity.UserID).Error; err != nil {
				return err
			}
			normalized, err := NormalizePATScopes(scopes, owner.Role)
			if err != nil {
				return err
			}
			if authorize == nil {
				return ErrPATInvalid
			}
			if err := authorize(tx, &owner, normalized); err != nil {
				return err
			}
			encoded, err := common.Marshal(normalized)
			if err != nil {
				return err
			}
			changes["scopes"] = string(encoded)
			token.Scopes = normalized
		}
		if err := tx.Model(&token).Updates(changes).Error; err != nil {
			return err
		}
		if name != nil {
			token.Name = *name
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func RevokePersonalAccessToken(identity AuthSessionIdentity, tokenID int) error {
	if identity.PersonalAccessTokenID != 0 || tokenID <= 0 {
		return ErrPATInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var token PersonalAccessToken
		if err := tx.Where("id = ? AND user_id = ?", tokenID, identity.UserID).First(&token).Error; err != nil {
			return err
		}
		if token.RevokedAt != 0 {
			return nil
		}
		return tx.Model(&token).Update("revoked_at", time.Now().Unix()).Error
	})
}

func ValidatePersonalAccessToken(raw string) (*PersonalAccessToken, *User, error) {
	if !strings.HasPrefix(raw, "pat_") || len(raw) != 52 {
		return nil, nil, ErrPATInvalid
	}
	var token PersonalAccessToken
	if err := DB.Where("digest = ? AND revoked_at = 0 AND expires_at > ?", PersonalAccessTokenDigest(raw), time.Now().Unix()).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrPATInvalid
		}
		return nil, nil, err
	}
	var owner User
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&owner, token.UserID).Error; err != nil {
			return err
		}
		if owner.Status != common.UserStatusEnabled || owner.Role < common.RoleCommonUser || !common.IsValidateRole(owner.Role) {
			return ErrPATInvalid
		}
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ? AND digest = ? AND revoked_at = 0 AND expires_at > ?", token.ID, owner.Id, PersonalAccessTokenDigest(raw), time.Now().Unix()).First(&token).Error; err != nil {
			return err
		}
		return tx.Model(&token).Update("last_used_at", time.Now().Unix()).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrPATInvalid
	}
	if err != nil {
		return nil, nil, err
	}
	return &token, &owner, nil
}
