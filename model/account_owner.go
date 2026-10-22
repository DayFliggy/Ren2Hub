package model

import (
	"errors"

	"gorm.io/gorm"
)

// GetOwnerIncludingDeleted reads the account behind a ticket or an order,
// including soft-deleted accounts. A nil user with a nil error means the
// account was hard-deleted; callers keep showing the stored user ID.
func GetOwnerIncludingDeleted(userID int) (*User, error) {
	var user User
	err := DB.Unscoped().
		Select("id", "username", "display_name", "email", "deleted_at").
		First(&user, "id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// OwnerDeleted reports whether an owner read by GetOwnerIncludingDeleted is
// gone, either hard-deleted (nil) or soft-deleted.
func OwnerDeleted(user *User) bool {
	return user == nil || user.DeletedAt.Valid
}
