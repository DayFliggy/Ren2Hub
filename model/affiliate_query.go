package model

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

type AffiliateUserPolicy struct {
	UserID           int    `json:"user_id"`
	Code             string `json:"code"`
	CodeCustom       bool   `json:"code_custom"`
	Enabled          bool   `json:"enabled"`
	RateBps          *int   `json:"rate_bps"`
	EffectiveRateBps int    `json:"effective_rate_bps"`
	InviterID        int    `json:"inviter_id"`
	InvitedAt        int64  `json:"invited_at"`
}

func affiliateUserPolicy(user *User) AffiliateUserPolicy {
	rate := operation_setting.SnapshotAffiliateSetting().RateBps
	if user.AffRebateRateBps != nil {
		rate = *user.AffRebateRateBps
	}
	return AffiliateUserPolicy{user.Id, user.AffCode, user.AffCodeCustom, user.AffCodeEnabled, user.AffRebateRateBps, rate, user.InviterId, user.InvitedAt}
}
func GetAffiliateUserPolicy(id int, operatorRole int) (*AffiliateUserPolicy, error) {
	var user User
	if err := DB.First(&user, id).Error; err != nil {
		return nil, err
	}
	if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
		return nil, ErrUserQuotaPermission
	}
	result := affiliateUserPolicy(&user)
	return &result, nil
}

// AffiliateRateUpdate 区分未提交与显式 null：未提交保持原值，null 清除专属比例，数字设置专属比例。
type AffiliateRateUpdate struct {
	Set   bool
	Value *int
}

func (u *AffiliateRateUpdate) UnmarshalJSON(data []byte) error {
	u.Set = true
	return common.Unmarshal(data, &u.Value)
}

type AffiliatePolicyUpdate struct {
	RateBps   AffiliateRateUpdate `json:"rate_bps"`
	Code      *string             `json:"code"`
	ResetCode bool                `json:"reset_code"`
	Enabled   *bool               `json:"enabled"`
}

// UpdateAffiliateUserPolicy 只修改请求中提交的字段。
func UpdateAffiliateUserPolicy(id, operatorRole int, request AffiliatePolicyUpdate) (*AffiliateUserPolicy, error) {
	if rate := request.RateBps.Value; rate != nil && (*rate < 0 || *rate > 10000) {
		return nil, errors.New("invalid affiliate rate")
	}
	if request.ResetCode && request.Code != nil {
		return nil, errors.New("cannot set and reset code together")
	}
	var result AffiliateUserPolicy
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, id).Error; err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		if request.RateBps.Set {
			user.AffRebateRateBps = request.RateBps.Value
		}
		if request.Enabled != nil {
			user.AffCodeEnabled = *request.Enabled
		}
		if request.Code != nil {
			user.AffCode = strings.ToUpper(strings.TrimSpace(*request.Code))
			if !affiliateCodePattern.MatchString(user.AffCode) {
				return ErrAffiliateCodeInvalid
			}
			user.AffCodeCustom = true
		}
		if request.ResetCode {
			code, err := newAffiliateCode()
			if err != nil {
				return err
			}
			user.AffCode = code
			user.AffCodeCustom = false
		}
		if request.ResetCode || request.Code != nil {
			var count int64
			if err := tx.Unscoped().Model(&User{}).Where("aff_code = ? AND id <> ?", user.AffCode, id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("invitation code already exists")
			}
		}
		if err := tx.Model(&User{}).Where("id = ?", id).Updates(map[string]any{"aff_rebate_rate_bps": user.AffRebateRateBps, "aff_code": user.AffCode, "aff_code_enabled": user.AffCodeEnabled, "aff_code_custom": user.AffCodeCustom}).Error; err != nil {
			return err
		}
		result = affiliateUserPolicy(&user)
		return nil
	})
	return &result, err
}

type AffiliateMonth struct {
	Month      string `json:"month"`
	NewCount   int    `json:"new_count"`
	Cumulative int    `json:"cumulative"`
	Reward     int    `json:"reward"`
	Quota      int    `json:"quota"`
}
type AffiliateInfo struct {
	Code                 string                             `json:"code"`
	CodeEnabled          bool                               `json:"code_enabled"`
	Invited              int                                `json:"invited"`
	EffectiveRateBps     int                                `json:"effective_rate_bps"`
	EffectiveRatePercent float64                            `json:"effective_rate_percent"`
	AvailableReward      int                                `json:"available_reward"`
	FrozenReward         int                                `json:"frozen_reward"`
	TotalReward          int                                `json:"total_reward"`
	RewardTotal          int                                `json:"reward_total"`
	Transferable         int                                `json:"transferable"`
	Records              []AffiliateRecord                  `json:"records"`
	MonthlySeries        []AffiliateMonth                   `json:"monthly_series"`
	Policy               operation_setting.AffiliateSetting `json:"policy"`
}

func GetAffiliateInfo(userID int, loc *time.Location) (*AffiliateInfo, error) {
	var user User
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		locked, err := lockAffiliateParticipantsTx(tx, userID)
		if err != nil {
			return err
		}
		user = *locked
		return thawAffiliateTx(tx, &user, common.GetTimestamp())
	})
	if err != nil {
		return nil, err
	}
	policy := operation_setting.SnapshotAffiliateSetting()
	rate := policy.RateBps
	if user.AffRebateRateBps != nil {
		rate = *user.AffRebateRateBps
	}
	result := &AffiliateInfo{Code: user.AffCode, CodeEnabled: user.AffCodeEnabled, Invited: user.AffCount, EffectiveRateBps: rate, EffectiveRatePercent: float64(rate) / 100, AvailableReward: user.AffQuota, FrozenReward: user.AffFrozenQuota, TotalReward: user.AffHistoryQuota, RewardTotal: user.AffHistoryQuota, Transferable: user.AffQuota, Policy: policy, Records: []AffiliateRecord{}, MonthlySeries: []AffiliateMonth{}}
	if err = DB.Where("user_id = ?", userID).Order("id DESC").Limit(100).Find(&result.Records).Error; err != nil {
		return nil, err
	}
	// Chart has a bounded twelve-month window of the viewer's calendar months in
	// loc; history remains available separately.
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month()-11, 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 12, 0)
	var rows []AffiliateRecord
	if err = DB.Select("created_at", "reward_quota").Where("user_id = ? AND kind = ? AND created_at >= ? AND created_at < ?", userID, "accrue", start.Unix(), end.Unix()).Find(&rows).Error; err != nil {
		return nil, err
	}
	sums := map[string]int{}
	for _, r := range rows {
		sums[time.Unix(r.CreatedAt, 0).In(loc).Format("2006-01")] += r.RewardQuota
	}
	// Bindings remain historical facts after an invitee is soft-deleted. The
	// opening balance includes older bindings without loading lifetime users.
	var cumulative int64
	if err = DB.Unscoped().Model(&User{}).Where("inviter_id = ? AND invited_at > 0 AND invited_at < ?", userID, start.Unix()).Count(&cumulative).Error; err != nil {
		return nil, err
	}
	var invitedAt []int64
	if err = DB.Unscoped().Model(&User{}).Where("inviter_id = ? AND invited_at >= ? AND invited_at < ?", userID, start.Unix(), end.Unix()).Pluck("invited_at", &invitedAt).Error; err != nil {
		return nil, err
	}
	newCounts := map[string]int{}
	for _, timestamp := range invitedAt {
		newCounts[time.Unix(timestamp, 0).In(loc).Format("2006-01")]++
	}
	for i := 0; i < 12; i++ {
		month := start.AddDate(0, i, 0).Format("2006-01")
		cumulative += int64(newCounts[month])
		result.MonthlySeries = append(result.MonthlySeries, AffiliateMonth{Month: month, NewCount: newCounts[month], Cumulative: int(cumulative), Reward: sums[month], Quota: sums[month]})
	}
	return result, nil
}
func ListAffiliateRecords(userID int, kind string, page *common.PageInfo) ([]AffiliateRecord, int64, error) {
	query := DB.Model(&AffiliateRecord{})
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if kind != "" {
		if kind != "accrue" && kind != "transfer" {
			return nil, 0, errors.New("invalid referral record kind")
		}
		query = query.Where("kind = ?", kind)
	}
	var total int64
	var rows []AffiliateRecord
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id DESC").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&rows).Error
	return rows, total, err
}

// AffiliateInvitee is deliberately separate from User: invitees expose neither
// raw account names nor email, quota, login identities or credentials.
type AffiliateInvitee struct {
	MaskedName  string `json:"masked_name"`
	InvitedAt   int64  `json:"invited_at"`
	RewardQuota int    `json:"reward_quota"`
}

func maskAffiliateName(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return "***"
	}
	if len(runes) < 4 {
		return string(runes[:1]) + "***"
	}
	return string(runes[:1]) + "***" + string(runes[len(runes)-1:])
}

func ListAffiliateInvitees(userID int, order string, page *common.PageInfo) ([]AffiliateInvitee, int64, error) {
	if userID < 1 || page == nil || page.Page < 1 || page.PageSize < 1 || page.PageSize > 100 || page.Page > common.MaxWalletQuota/page.PageSize {
		return nil, 0, errors.New("invalid invitee pagination")
	}
	if order != "" && order != "asc" && order != "desc" {
		return nil, 0, errors.New("invalid invitee order")
	}
	query := DB.Model(&User{}).Where("inviter_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []User
	direction := "DESC"
	if order == "asc" {
		direction = "ASC"
	}
	if err := query.Select("id", "username", "invited_at").Order("invited_at " + direction).Order("id " + direction).Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	result := make([]AffiliateInvitee, 0, len(users))
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
	}
	if len(ids) == 0 {
		return result, total, nil
	}
	var rewards []struct {
		InviteeID   int
		RewardQuota int
	}
	if err := DB.Model(&AffiliateRecord{}).Select("invitee_id, SUM(reward_quota) AS reward_quota").Where("user_id = ? AND kind = ? AND invitee_id IN ?", userID, "accrue", ids).Group("invitee_id").Scan(&rewards).Error; err != nil {
		return nil, 0, err
	}
	amounts := make(map[int]int, len(rewards))
	for _, reward := range rewards {
		amounts[reward.InviteeID] = reward.RewardQuota
	}
	for _, user := range users {
		result = append(result, AffiliateInvitee{MaskedName: maskAffiliateName(user.Username), InvitedAt: user.InvitedAt, RewardQuota: amounts[user.Id]})
	}
	return result, total, nil
}
