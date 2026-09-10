package model

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"unicode"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const DefaultAccountClass = "default"

type AccountClass struct {
	Code               string  `json:"code" gorm:"primaryKey;type:varchar(64)"`
	Name               string  `json:"name" gorm:"type:varchar(128);not null"`
	DefaultConcurrency int     `json:"default_concurrency" gorm:"not null"`
	DefaultRPM         int     `json:"default_rpm" gorm:"not null"`
	TopupMultiplier    float64 `json:"topup_multiplier" gorm:"not null"`
	PolicyVersion      int64   `json:"policy_version" gorm:"not null"`
}

func (a AccountClass) Validate() error {
	if a.Code == "" || len(a.Code) > 64 || strings.TrimSpace(a.Code) != a.Code || strings.IndexFunc(a.Code, unicode.IsControl) >= 0 {
		return errors.New("invalid account class code")
	}
	if strings.TrimSpace(a.Name) == "" || len(a.Name) > 128 {
		return errors.New("invalid account class name")
	}
	if a.DefaultConcurrency < 0 || a.DefaultConcurrency > RouteMaxConcurrency || a.DefaultRPM < 0 || a.DefaultRPM > 1_000_000_000 {
		return errors.New("invalid account class limit")
	}
	if a.TopupMultiplier <= 0 || math.IsNaN(a.TopupMultiplier) || math.IsInf(a.TopupMultiplier, 0) {
		return errors.New("invalid topup multiplier")
	}
	return nil
}

func InitializeAccountClasses(db *gorm.DB) error {
	if err := db.AutoMigrate(&AccountClass{}); err != nil {
		return err
	}
	a := AccountClass{Code: DefaultAccountClass, Name: "默认", DefaultConcurrency: 5, TopupMultiplier: 1, PolicyVersion: 1}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&a).Error
}

func GetAccountClass(ctx context.Context, code string) (AccountClass, error) {
	var a AccountClass
	if DB == nil {
		return a, errors.New("account class database unavailable")
	}
	err := DB.WithContext(ctx).First(&a, "code = ?", code).Error
	return a, err
}

func ListAccountClasses(ctx context.Context) ([]AccountClass, error) {
	var classes []AccountClass
	err := DB.WithContext(ctx).Order("code ASC").Find(&classes).Error
	return classes, err
}

func CreateAccountClass(ctx context.Context, a AccountClass) error {
	if err := a.Validate(); err != nil {
		return err
	}
	a.PolicyVersion = 1
	return DB.WithContext(ctx).Create(&a).Error
}

func UpdateAccountClass(ctx context.Context, a AccountClass) error {
	if err := a.Validate(); err != nil {
		return err
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current AccountClass
		if err := lockForUpdate(tx).First(&current, "code = ?", a.Code).Error; err != nil {
			return err
		}
		if a.PolicyVersion != current.PolicyVersion {
			return errors.New("account class version conflict")
		}
		a.PolicyVersion++
		return tx.Model(&current).Select("name", "default_concurrency", "default_rpm", "topup_multiplier", "policy_version").Updates(&a).Error
	})
}

func DeleteAccountClass(ctx context.Context, code string) error {
	if code == DefaultAccountClass {
		return errors.New("default account class cannot be deleted")
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var a AccountClass
		if err := lockForUpdate(tx).First(&a, "code = ?", code).Error; err != nil {
			return err
		}
		for _, ref := range []struct{ table, condition string }{
			{"users", "account_class = ?"},
			{"subscription_plans", "upgrade_account_class = ? OR downgrade_account_class = ?"},
			{"user_subscriptions", "upgrade_account_class = ? OR downgrade_account_class = ? OR previous_account_class = ?"},
		} {
			args := make([]any, strings.Count(ref.condition, "?"))
			for i := range args {
				args[i] = code
			}
			var count int64
			if err := tx.Table(ref.table).Where(ref.condition, args...).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("account class is still referenced")
			}
		}
		return tx.Delete(&a).Error
	})
}

type EffectiveUserLimits struct {
	AccountClass       string `json:"account_class"`
	Concurrency        int    `json:"effective_concurrency"`
	RPM                int    `json:"effective_rpm"`
	UserPolicyVersion  int64  `json:"policy_version"`
	ClassPolicyVersion int64  `json:"class_policy_version"`
}

func ResolveUserLimits(ctx context.Context, userID int) (EffectiveUserLimits, error) {
	var user User
	if err := DB.WithContext(ctx).Select("id", "account_class", "concurrency_override", "rpm_override", "policy_version").First(&user, userID).Error; err != nil {
		return EffectiveUserLimits{}, err
	}
	a, err := GetAccountClass(ctx, user.AccountClass)
	if err != nil {
		return EffectiveUserLimits{}, err
	}
	return effectiveUserLimits(user, a), nil
}

func ValidateUserLimits(user *User) error {
	if user.ConcurrencyOverride != nil && (*user.ConcurrencyOverride < 0 || *user.ConcurrencyOverride > RouteMaxConcurrency) {
		return errors.New("invalid concurrency override")
	}
	if user.RPMOverride != nil && (*user.RPMOverride < 0 || *user.RPMOverride > 1_000_000_000) {
		return errors.New("invalid RPM override")
	}
	return nil
}

func GetUserTopupMultiplier(userID int) (float64, error) {
	code, err := GetUserAccountClass(userID, true)
	if err != nil {
		return 0, err
	}
	a, err := GetAccountClass(context.Background(), code)
	if err != nil {
		return 0, err
	}
	if err := a.Validate(); err != nil {
		return 0, err
	}
	return a.TopupMultiplier, nil
}

func lockUserAccountClass(tx *gorm.DB, user *User) error {
	if user.AccountClass == "" {
		user.AccountClass = DefaultAccountClass
	}
	if err := ValidateUserLimits(user); err != nil {
		return err
	}
	return LockAccountClassReferences(tx, user.AccountClass)
}

// All reference writers and deletion lock the catalog row. Read-before-write
// validation alone allows a concurrent delete to leave dangling references.
func LockAccountClassReferences(tx *gorm.DB, codes ...string) error {
	ordered := slices.Clone(codes)
	slices.Sort(ordered)
	for _, code := range slices.Compact(ordered) {
		if code == "" {
			continue
		}
		var a AccountClass
		if err := lockForUpdate(tx).First(&a, "code = ?", code).Error; err != nil {
			return err
		}
	}
	return nil
}

func updateUserAccountClassTx(tx *gorm.DB, userID int, code string) error {
	user := User{AccountClass: code}
	if err := lockUserAccountClass(tx, &user); err != nil {
		return err
	}
	return tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"account_class": code, "policy_version": gorm.Expr("policy_version + ?", 1),
	}).Error
}
