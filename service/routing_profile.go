package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var (
	ErrRouteProfileNotFound      = errors.New("route profile not found")
	ErrRouteProfileConflict      = errors.New("route profile version conflict")
	ErrRouteProfileForbidden     = errors.New("route profile access denied")
	ErrRouteProfileValidation    = errors.New("invalid route profile")
	ErrRouteProfileAlreadyExists = errors.New("route profile already exists for token")
)

type RouteProfileInput struct {
	UserID        int               `json:"-"`
	TokenID       int               `json:"token_id"`
	Mode          string            `json:"mode"`
	ActiveGroupID *int              `json:"active_group_id"`
	Version       int64             `json:"version"`
	Groups        []RouteGroupInput `json:"groups"`
}

type RouteGroupInput struct {
	ID       int               `json:"id"`
	Name     string            `json:"name"`
	Kind     string            `json:"kind"`
	Enabled  bool              `json:"enabled"`
	Position int               `json:"position"`
	Entries  []RouteEntryInput `json:"entries"`
	Policy   RoutePolicyInput  `json:"policy"`
}

type RouteEntryInput struct {
	ID        int    `json:"id"`
	ChannelID int    `json:"channel_id"`
	Source    string `json:"source"`
	Enabled   bool   `json:"enabled"`
	Position  int    `json:"position"`
	Weight    int    `json:"weight"`
}

type RoutePolicyInput struct {
	LoadBalance             bool    `json:"load_balance"`
	MaxRatio                float64 `json:"max_ratio"`
	RetryMode               string  `json:"retry_mode"`
	MaxSameResourceAttempts int     `json:"max_same_resource_attempts"`
	MaxFailoverAttempts     int     `json:"max_failover_attempts"`
	Sticky                  bool    `json:"sticky"`
}

type RouteProfileView struct {
	Profile model.UserRouteProfile `json:"profile"`
	Groups  []RouteGroupView       `json:"groups"`
}

type RouteGroupView struct {
	Group   model.UserRouteGroup   `json:"group"`
	Entries []model.UserRouteEntry `json:"entries"`
	Policy  model.RoutePolicy      `json:"policy"`
}

func ListUserRouteProfiles(userID int) ([]RouteProfileView, error) {
	if userID <= 0 {
		return nil, ErrRouteProfileForbidden
	}
	var profiles []model.UserRouteProfile
	if err := model.DB.Where("user_id = ?", userID).Order("id desc").Find(&profiles).Error; err != nil {
		return nil, err
	}
	views := make([]RouteProfileView, 0, len(profiles))
	for i := range profiles {
		view, err := loadRouteProfileView(model.DB, &profiles[i])
		if err != nil {
			return nil, err
		}
		views = append(views, *view)
	}
	return views, nil
}

func GetUserRouteProfile(userID, profileID int) (*RouteProfileView, error) {
	if userID <= 0 || profileID <= 0 {
		return nil, ErrRouteProfileForbidden
	}
	var profile model.UserRouteProfile
	if err := model.DB.Where("id = ? AND user_id = ?", profileID, userID).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRouteProfileNotFound
		}
		return nil, err
	}
	return loadRouteProfileView(model.DB, &profile)
}

func CreateUserRouteProfile(input RouteProfileInput) (*RouteProfileView, error) {
	if err := validateRouteProfileInput(input, false); err != nil {
		return nil, err
	}
	var view *RouteProfileView
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := ensureRouteTokenOwner(tx, input.UserID, input.TokenID); err != nil {
			return err
		}
		var existing model.UserRouteProfile
		if err := tx.Where("token_id = ?", input.TokenID).First(&existing).Error; err == nil {
			return ErrRouteProfileAlreadyExists
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		profile := model.UserRouteProfile{
			UserID:  input.UserID,
			TokenID: input.TokenID,
			Mode:    input.Mode,
		}
		profile.Normalize(time.Now())
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrRouteProfileValidation, err)
		}
		if err := tx.Create(&profile).Error; err != nil {
			return err
		}
		groupIDs, err := replaceGroups(tx, input.UserID, &profile, input.Groups, nil)
		if err != nil {
			return err
		}
		activeGroupID, err := resolveActiveGroupID(input.ActiveGroupID, groupIDs)
		if err != nil {
			return err
		}
		profile.ActiveGroupID = activeGroupID
		if err := tx.Model(&profile).Updates(map[string]any{"active_group_id": activeGroupID}).Error; err != nil {
			return err
		}
		view, err = loadRouteProfileView(tx, &profile)
		return err
	})
	return view, err
}

func UpdateUserRouteProfile(profileID int, input RouteProfileInput) (*RouteProfileView, error) {
	if profileID <= 0 {
		return nil, ErrRouteProfileNotFound
	}
	if err := validateRouteProfileInput(input, true); err != nil {
		return nil, err
	}
	var view *RouteProfileView
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var profile model.UserRouteProfile
		if err := tx.Where("id = ? AND user_id = ?", profileID, input.UserID).First(&profile).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRouteProfileNotFound
			}
			return err
		}
		if input.TokenID != 0 && input.TokenID != profile.TokenID {
			return ErrRouteProfileForbidden
		}
		if input.Version != profile.Version {
			return ErrRouteProfileConflict
		}
		if err := ensureRouteTokenOwner(tx, input.UserID, profile.TokenID); err != nil {
			return err
		}

		now := time.Now().Unix()
		nextVersion := profile.Version + 1
		result := tx.Model(&model.UserRouteProfile{}).
			Where("id = ? AND user_id = ? AND version = ?", profile.ID, input.UserID, profile.Version).
			Updates(map[string]any{
				"mode":       input.Mode,
				"version":    nextVersion,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRouteProfileConflict
		}
		profile.Mode = input.Mode
		profile.Version = nextVersion
		profile.UpdatedAt = now
		groupIDs, err := replaceGroups(tx, input.UserID, &profile, input.Groups, &profile)
		if err != nil {
			return err
		}
		activeGroupID, err := resolveActiveGroupID(input.ActiveGroupID, groupIDs)
		if err != nil {
			return err
		}
		profile.ActiveGroupID = activeGroupID
		if err := tx.Model(&profile).Updates(map[string]any{"active_group_id": activeGroupID}).Error; err != nil {
			return err
		}
		view, err = loadRouteProfileView(tx, &profile)
		return err
	})
	return view, err
}

func DeleteUserRouteProfile(userID, profileID int) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var profile model.UserRouteProfile
		if err := tx.Where("id = ? AND user_id = ?", profileID, userID).First(&profile).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRouteProfileNotFound
			}
			return err
		}
		var groups []model.UserRouteGroup
		if err := tx.Where("profile_id = ?", profile.ID).Find(&groups).Error; err != nil {
			return err
		}
		groupIDs := make([]int, 0, len(groups))
		for _, group := range groups {
			groupIDs = append(groupIDs, group.ID)
		}
		if len(groupIDs) > 0 {
			if err := tx.Where("group_id IN ?", groupIDs).Delete(&model.UserRouteEntry{}).Error; err != nil {
				return err
			}
			if err := tx.Where("group_id IN ?", groupIDs).Delete(&model.RoutePolicy{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("profile_id = ?", profile.ID).Delete(&model.UserRouteGroup{}).Error; err != nil {
			return err
		}
		return tx.Delete(&profile).Error
	})
}

func loadRouteProfileView(db *gorm.DB, profile *model.UserRouteProfile) (*RouteProfileView, error) {
	var groups []model.UserRouteGroup
	if err := db.Where("profile_id = ?", profile.ID).Order("position asc, id asc").Find(&groups).Error; err != nil {
		return nil, err
	}
	views := make([]RouteGroupView, 0, len(groups))
	for _, group := range groups {
		var entries []model.UserRouteEntry
		if err := db.Where("group_id = ?", group.ID).Order("position asc, id asc").Find(&entries).Error; err != nil {
			return nil, err
		}
		var policy model.RoutePolicy
		if err := db.Where("group_id = ?", group.ID).First(&policy).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			policy = defaultRoutePolicy(group.ID)
		}
		views = append(views, RouteGroupView{Group: group, Entries: entries, Policy: policy})
	}
	return &RouteProfileView{Profile: *profile, Groups: views}, nil
}

func replaceGroups(tx *gorm.DB, userID int, profile *model.UserRouteProfile, inputs []RouteGroupInput, existingProfile *model.UserRouteProfile) (map[int]int, error) {
	if profile.Mode != model.RouteModeManual && len(inputs) > 0 {
		return nil, fmt.Errorf("%w: only manual profiles can contain user groups", ErrRouteProfileValidation)
	}
	existing := make(map[int]model.UserRouteGroup)
	if existingProfile != nil {
		var groups []model.UserRouteGroup
		if err := tx.Where("profile_id = ?", profile.ID).Find(&groups).Error; err != nil {
			return nil, err
		}
		for _, group := range groups {
			existing[group.ID] = group
		}
	}
	seenIDs := make(map[int]struct{}, len(inputs))
	seenPositions := make(map[int]struct{}, len(inputs))
	groupIDs := make(map[int]int, len(inputs))
	for index, input := range inputs {
		if input.Kind != "" && input.Kind != model.RouteGroupKindManual {
			return nil, fmt.Errorf("%w: auto lab groups are system-owned", ErrRouteProfileValidation)
		}
		group := model.UserRouteGroup{
			ID:        input.ID,
			ProfileID: profile.ID,
			Name:      strings.TrimSpace(input.Name),
			Kind:      model.RouteGroupKindManual,
			Enabled:   input.Enabled,
			Position:  input.Position,
		}
		if _, duplicate := seenPositions[group.Position]; duplicate {
			return nil, fmt.Errorf("%w: duplicate group position", ErrRouteProfileValidation)
		}
		seenPositions[group.Position] = struct{}{}
		if err := group.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrRouteProfileValidation, err)
		}
		if input.ID > 0 {
			if _, ok := existing[input.ID]; !ok {
				return nil, ErrRouteProfileForbidden
			}
			if _, duplicate := seenIDs[input.ID]; duplicate {
				return nil, fmt.Errorf("%w: duplicate route group", ErrRouteProfileValidation)
			}
			seenIDs[input.ID] = struct{}{}
			if err := tx.Model(&model.UserRouteGroup{}).Where("id = ? AND profile_id = ?", input.ID, profile.ID).Updates(&group).Error; err != nil {
				return nil, err
			}
		} else if err := tx.Create(&group).Error; err != nil {
			return nil, err
		}
		groupKey := input.ID
		if groupKey <= 0 {
			// New groups have no client ID. Use a private negative key so every
			// newly created group remains available when resolving the default.
			groupKey = -index - 1
		}
		groupIDs[groupKey] = group.ID
		if err := replaceGroupChildren(tx, userID, &group, input); err != nil {
			return nil, err
		}
	}
	if existingProfile != nil {
		for id := range existing {
			if _, keep := seenIDs[id]; keep {
				continue
			}
			if err := deleteRouteGroup(tx, id); err != nil {
				return nil, err
			}
		}
	}
	return groupIDs, nil
}

func replaceGroupChildren(tx *gorm.DB, userID int, group *model.UserRouteGroup, input RouteGroupInput) error {
	if err := tx.Where("group_id = ?", group.ID).Delete(&model.UserRouteEntry{}).Error; err != nil {
		return err
	}
	if err := tx.Where("group_id = ?", group.ID).Delete(&model.RoutePolicy{}).Error; err != nil {
		return err
	}
	policy := model.RoutePolicy{
		GroupID:                 group.ID,
		LoadBalance:             input.Policy.LoadBalance,
		MaxRatio:                input.Policy.MaxRatio,
		RetryMode:               input.Policy.RetryMode,
		MaxSameResourceAttempts: input.Policy.MaxSameResourceAttempts,
		MaxFailoverAttempts:     input.Policy.MaxFailoverAttempts,
		Sticky:                  input.Policy.Sticky,
	}
	if input.Policy.RetryMode == "" && input.Policy.MaxFailoverAttempts == 0 {
		policy.MaxFailoverAttempts = 1
	}
	policy.Normalize()
	if err := policy.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrRouteProfileValidation, err)
	}
	if err := tx.Create(&policy).Error; err != nil {
		return err
	}
	seenChannels := make(map[int]struct{}, len(input.Entries))
	seenPositions := make(map[int]struct{}, len(input.Entries))
	for _, item := range input.Entries {
		entry := model.UserRouteEntry{
			GroupID:   group.ID,
			ChannelID: item.ChannelID,
			Source:    item.Source,
			Enabled:   item.Enabled,
			Position:  item.Position,
			Weight:    item.Weight,
		}
		if _, duplicate := seenChannels[entry.ChannelID]; duplicate {
			return fmt.Errorf("%w: duplicate channel in route group", ErrRouteProfileValidation)
		}
		if _, duplicate := seenPositions[entry.Position]; duplicate {
			return fmt.Errorf("%w: duplicate entry position", ErrRouteProfileValidation)
		}
		seenChannels[entry.ChannelID] = struct{}{}
		seenPositions[entry.Position] = struct{}{}
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrRouteProfileValidation, err)
		}
		if err := ensureRouteChannelEntitlement(tx, userID, entry.ChannelID, entry.Source); err != nil {
			return err
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
	}
	return nil
}

func deleteRouteGroup(tx *gorm.DB, groupID int) error {
	if err := tx.Where("group_id = ?", groupID).Delete(&model.UserRouteEntry{}).Error; err != nil {
		return err
	}
	if err := tx.Where("group_id = ?", groupID).Delete(&model.RoutePolicy{}).Error; err != nil {
		return err
	}
	return tx.Delete(&model.UserRouteGroup{}, groupID).Error
}

func resolveActiveGroupID(requested *int, groupIDs map[int]int) (*int, error) {
	if requested != nil && *requested > 0 {
		if resolved, ok := groupIDs[*requested]; ok {
			return &resolved, nil
		}
		return nil, ErrRouteProfileForbidden
	}
	if len(groupIDs) == 0 {
		return nil, nil
	}
	ids := make([]int, 0, len(groupIDs))
	for _, id := range groupIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return &ids[0], nil
}

func validateRouteProfileInput(input RouteProfileInput, update bool) error {
	if input.UserID <= 0 || (!update && input.TokenID <= 0) {
		return ErrRouteProfileForbidden
	}
	if input.Mode != model.RouteModeManual && input.Mode != model.RouteModeAutoLab && input.Mode != model.RouteModeLegacy {
		return fmt.Errorf("%w: invalid route profile mode", ErrRouteProfileValidation)
	}
	if update && input.Version <= 0 {
		return fmt.Errorf("%w: version is required", ErrRouteProfileValidation)
	}
	return nil
}

func ensureRouteTokenOwner(tx *gorm.DB, userID, tokenID int) error {
	var token model.Token
	if err := tx.Select("id", "user_id").Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRouteProfileForbidden
		}
		return err
	}
	return nil
}

func ensureRouteChannelEntitlement(tx *gorm.DB, userID, channelID int, source string) error {
	if source != model.RouteSourcePlatform {
		return fmt.Errorf("%w: unsupported channel source", ErrRouteProfileValidation)
	}
	var channel model.Channel
	if err := tx.Select("id", "status").Where("id = ?", channelID).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRouteProfileForbidden
		}
		return err
	}
	if channel.Status != common.ChannelStatusEnabled {
		return fmt.Errorf("%w: channel is disabled", ErrRouteProfileValidation)
	}
	var userGroup string
	if err := tx.Model(&model.User{}).Where("id = ?", userID).Pluck("group", &userGroup).Error; err != nil {
		return err
	}
	var abilities []model.Ability
	if err := tx.Where("channel_id = ? AND enabled = ?", channelID, true).Find(&abilities).Error; err != nil {
		return err
	}
	abilityAllowed := false
	for _, ability := range abilities {
		if ability.Group == userGroup || IsUserSelectableGroup(userGroup, ability.Group) {
			abilityAllowed = true
			break
		}
	}
	if !abilityAllowed {
		return fmt.Errorf("%w: channel has no model ability for user group", ErrRouteProfileValidation)
	}
	var entitlement model.UserChannelEntitlement
	err := tx.Where("user_id = ? AND channel_id = ? AND source = ?", userID, channelID, source).First(&entitlement).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if entitlement.Status != model.RouteEntitlementStatusEnabled || entitlement.RevokedAt > 0 || (entitlement.ExpiresAt > 0 && entitlement.ExpiresAt <= time.Now().Unix()) {
		return fmt.Errorf("%w: channel entitlement is not active", ErrRouteProfileValidation)
	}
	return nil
}

func ValidatePlatformChannelEntitlement(userID, channelID int) error {
	if userID <= 0 {
		return ErrRouteProfileForbidden
	}
	return ensureRouteChannelEntitlement(model.DB, userID, channelID, model.RouteSourcePlatform)
}

func defaultRoutePolicy(groupID int) model.RoutePolicy {
	return model.RoutePolicy{
		GroupID:             groupID,
		MaxRatio:            1,
		RetryMode:           model.RoutePolicyRetryNextChannel,
		MaxFailoverAttempts: 1,
	}
}
