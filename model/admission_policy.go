package model

import (
	"context"

	"gorm.io/gorm"
)

// Admission and policy edits take the same row locks. A queued request therefore
// observes committed limits at every admission attempt, independently of auth caches.
func WithUserAdmissionPolicy(ctx context.Context, userID int, admit func(EffectiveUserLimits) error) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id", "account_class", "concurrency_override", "rpm_override", "policy_version").First(&user, userID).Error; err != nil {
			return err
		}
		var class AccountClass
		if err := lockForUpdate(tx).First(&class, "code = ?", user.AccountClass).Error; err != nil {
			return err
		}
		if err := class.Validate(); err != nil {
			return err
		}
		if err := ValidateUserLimits(&user); err != nil {
			return err
		}
		return admit(effectiveUserLimits(user, class))
	})
}

func effectiveUserLimits(user User, class AccountClass) EffectiveUserLimits {
	limits := EffectiveUserLimits{AccountClass: class.Code, Concurrency: class.DefaultConcurrency, RPM: class.DefaultRPM,
		UserPolicyVersion: user.PolicyVersion, ClassPolicyVersion: class.PolicyVersion}
	if user.ConcurrencyOverride != nil {
		limits.Concurrency = *user.ConcurrencyOverride
	}
	if user.RPMOverride != nil {
		limits.RPM = *user.RPMOverride
	}
	return limits
}
