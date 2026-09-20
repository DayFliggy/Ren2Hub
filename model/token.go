package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

type Token struct {
	Id                 int            `json:"id"`
	UserId             int            `json:"user_id" gorm:"index"`
	Key                string         `json:"key" gorm:"type:varchar(128);uniqueIndex"`
	Status             int            `json:"status" gorm:"default:1"`
	Name               string         `json:"name" gorm:"index" `
	CreatedTime        int64          `json:"created_time" gorm:"bigint"`
	AccessedTime       int64          `json:"accessed_time" gorm:"bigint"`
	ExpiredTime        int64          `json:"expired_time" gorm:"bigint;default:-1"` // -1 means never expired
	RemainQuota        common.Quota   `json:"available_quota" gorm:"column:available_quota;type:bigint;default:0"`
	ReservedQuota      common.Quota   `json:"reserved_quota" gorm:"type:bigint;not null;default:0"`
	QuotaVersion       int64          `json:"quota_version" gorm:"type:bigint;not null;default:0"`
	UnlimitedQuota     bool           `json:"unlimited_quota"`
	ModelLimitsEnabled bool           `json:"model_limits_enabled"`
	ModelLimits        string         `json:"model_limits" gorm:"type:text"`
	AllowIps           *string        `json:"allow_ips" gorm:"default:''"`
	UsedQuota          common.Quota   `json:"used_quota" gorm:"type:bigint;default:0"` // used quota
	DeletedAt          gorm.DeletedAt `gorm:"index"`
	ConversionPolicy   string         `json:"conversion_policy" gorm:"type:varchar(16);default:'safe'"`
}

func (token *Token) Clean() {
	token.Key = ""
}

func MaskTokenKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return strings.Repeat("*", len(key))
	}
	if len(key) <= 8 {
		return key[:2] + "****" + key[len(key)-2:]
	}
	return key[:4] + "**********" + key[len(key)-4:]
}

func (token *Token) GetFullKey() string {
	return token.Key
}

func (token *Token) GetMaskedKey() string {
	return MaskTokenKey(token.Key)
}

func (token *Token) GetIpLimits() []string {
	// delete empty spaces
	//split with \n
	ipLimits := make([]string, 0)
	if token.AllowIps == nil {
		return ipLimits
	}
	cleanIps := strings.ReplaceAll(*token.AllowIps, " ", "")
	if cleanIps == "" {
		return ipLimits
	}
	ips := strings.Split(cleanIps, "\n")
	for _, ip := range ips {
		ip = strings.TrimSpace(ip)
		ip = strings.ReplaceAll(ip, ",", "")
		if ip != "" {
			ipLimits = append(ipLimits, ip)
		}
	}
	return ipLimits
}

func GetAllUserTokens(userId int, startIdx int, num int) ([]*Token, error) {
	var tokens []*Token
	var err error
	err = DB.Where("user_id = ?", userId).Order("id desc").Limit(num).Offset(startIdx).Find(&tokens).Error
	return tokens, err
}

// sanitizeLikePattern 校验并清洗用户输入的 LIKE 搜索模式。
// 规则：
//  1. 转义 ! 和 _（使用 ! 作为 ESCAPE 字符，兼容 MySQL/PostgreSQL/SQLite）
//  2. 连续的 % 合并为单个 %
//  3. 最多允许 2 个 %
//  4. 含 % 时（模糊搜索），去掉 % 后关键词长度必须 >= 2
//  5. 不含 % 时按精确匹配
func sanitizeLikePattern(input string) (string, error) {
	// 1. 先转义 ESCAPE 字符 ! 自身，再转义 _
	//    使用 ! 而非 \ 作为 ESCAPE 字符，避免 MySQL 中反斜杠的字符串转义问题
	input = strings.ReplaceAll(input, "!", "!!")
	input = strings.ReplaceAll(input, `_`, `!_`)

	if err := validateLikePattern(input); err != nil {
		return "", err
	}

	// 5. 无 % 时，精确全匹配
	return input, nil
}

func validateLikePattern(input string) error {
	// 1. 连续的 % 直接拒绝
	if strings.Contains(input, "%%") {
		return errors.New("搜索模式中不允许包含连续的 % 通配符")
	}

	// 2. 统计 % 数量，不得超过 2
	count := strings.Count(input, "%")
	if count > 2 {
		return errors.New("搜索模式中最多允许包含 2 个 % 通配符")
	}

	// 3. 含 % 时，去掉 % 后关键词长度必须 >= 2
	if count > 0 {
		stripped := strings.ReplaceAll(input, "%", "")
		if len(stripped) < 2 {
			return errors.New("使用模糊搜索时，关键词长度至少为 2 个字符")
		}
	}

	return nil
}

const searchHardLimit = 100

func SearchUserTokens(userId int, keyword string, token string, offset int, limit int) (tokens []*Token, total int64, err error) {
	// model 层强制截断
	if limit <= 0 || limit > searchHardLimit {
		limit = searchHardLimit
	}
	if offset < 0 {
		offset = 0
	}

	if token != "" {
		token = strings.TrimPrefix(token, "sk-")
	}

	// 超量用户（令牌数超过上限）只允许精确搜索，禁止模糊搜索
	maxTokens := operation_setting.GetMaxUserTokens()
	hasFuzzy := strings.Contains(keyword, "%") || strings.Contains(token, "%")
	if hasFuzzy {
		count, err := CountUserTokens(userId)
		if err != nil {
			common.SysLog("failed to count user tokens: " + err.Error())
			return nil, 0, errors.New("获取令牌数量失败")
		}
		if int(count) > maxTokens {
			return nil, 0, errors.New("令牌数量超过上限，仅允许精确搜索，请勿使用 % 通配符")
		}
	}

	baseQuery := DB.Model(&Token{}).Where("user_id = ?", userId)

	// 非空才加 LIKE 条件，空则跳过（不过滤该字段）
	if keyword != "" {
		keywordPattern, err := sanitizeLikePattern(keyword)
		if err != nil {
			return nil, 0, err
		}
		baseQuery = baseQuery.Where("name LIKE ? ESCAPE '!'", keywordPattern)
	}
	if token != "" {
		tokenPattern, err := sanitizeLikePattern(token)
		if err != nil {
			return nil, 0, err
		}
		baseQuery = baseQuery.Where(commonKeyCol+" LIKE ? ESCAPE '!'", tokenPattern)
	}

	// 先查匹配总数（用于分页，受 maxTokens 上限保护，避免全表 COUNT）
	err = baseQuery.Limit(maxTokens).Count(&total).Error
	if err != nil {
		common.SysError("failed to count search tokens: " + err.Error())
		return nil, 0, errors.New("搜索令牌失败")
	}

	// 再分页查数据
	err = baseQuery.Order("id desc").Offset(offset).Limit(limit).Find(&tokens).Error
	if err != nil {
		common.SysError("failed to search tokens: " + err.Error())
		return nil, 0, errors.New("搜索令牌失败")
	}
	return tokens, total, nil
}

func ValidateUserToken(key string) (token *Token, err error) {
	if key == "" {
		return nil, ErrTokenNotProvided
	}
	token, err = GetTokenByKey(key, false)
	if err == nil {
		if token.Status == common.TokenStatusExhausted ||
			token.Status == common.TokenStatusExpired ||
			token.Status != common.TokenStatusEnabled {
			return token, ErrTokenInvalid
		}
		if token.ExpiredTime != -1 && token.ExpiredTime < common.GetTimestamp() {
			if !common.RedisEnabled {
				token.Status = common.TokenStatusExpired
				err := token.SelectUpdate()
				if err != nil {
					common.SysLog("failed to update token status" + err.Error())
				}
			}
			return token, ErrTokenInvalid
		}
		// Budget admission happens after pricing, in the atomic reservation. A zero
		// available budget can still authorize a request with an explicit free quote.

		return token, nil
	}
	common.SysLog("ValidateUserToken: failed to get token: " + err.Error())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTokenInvalid
	}
	return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
}

func GetTokenByIds(id int, userId int) (*Token, error) {
	if id == 0 || userId == 0 {
		return nil, errors.New("id 或 userId 为空！")
	}
	token := Token{Id: id, UserId: userId}
	var err error = nil
	err = DB.First(&token, "id = ? and user_id = ?", id, userId).Error
	return &token, err
}

func GetTokenById(id int) (*Token, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	token := Token{Id: id}
	var err error = nil
	err = DB.First(&token, "id = ?", id).Error
	return &token, err
}

func GetTokenByKey(key string, fromDB bool) (token *Token, err error) {
	if !fromDB && common.RedisEnabled {
		// Try Redis first
		token, err := cacheGetTokenByKey(key)
		if err == nil {
			var balances Token
			if err := DB.Select("id", "available_quota", "reserved_quota", "used_quota", "quota_version", "unlimited_quota").First(&balances, token.Id).Error; err != nil {
				return nil, err
			}
			token.RemainQuota, token.ReservedQuota, token.UsedQuota, token.QuotaVersion, token.UnlimitedQuota = balances.RemainQuota, balances.ReservedQuota, balances.UsedQuota, balances.QuotaVersion, balances.UnlimitedQuota
			return token, nil
		}
		// Don't return error - fall through to DB
	}
	token = &Token{}
	if err = DB.Where(commonKeyCol+" = ?", key).First(token).Error; err != nil {
		return nil, err
	}
	if common.RedisEnabled {
		// 冷缓存时用数据库快照初始化；已存在的哈希只刷新 TTL，
		// 避免快照覆盖 Redis 中已被原子预扣的余额。初始化失败不影响本次读取。
		if _, cacheErr := cacheInitToken(*token); cacheErr != nil {
			common.SysLog("failed to init token cache: " + cacheErr.Error())
		}
	}
	return token, nil
}

func (token Token) MarshalJSON() ([]byte, error) {
	type tokenJSON Token
	var available *common.Quota
	if !token.UnlimitedQuota {
		available = &token.RemainQuota
	}
	return common.Marshal(struct {
		tokenJSON
		Available *common.Quota `json:"available_quota"`
	}{tokenJSON: tokenJSON(token), Available: available})
}

func (token *Token) Insert() error {
	return DB.Transaction(func(tx *gorm.DB) error { return token.insertWithBudgetEventTx(tx) })
}

func (token *Token) insertWithBudgetEventTx(tx *gorm.DB) error {
	if token.RemainQuota < 0 || token.ReservedQuota != 0 || token.UsedQuota != 0 {
		return errors.New("invalid initial token budget")
	}
	var user User
	if err := lockForUpdate(tx).First(&user, token.UserId).Error; err != nil {
		return err
	}
	initial := token.RemainQuota
	token.RemainQuota = 0
	if token.UnlimitedQuota {
		initial = 0
	}
	if err := tx.Create(token).Error; err != nil {
		return err
	}
	fingerprint, err := billingEventFingerprint(struct {
		TokenID   int
		Available common.Quota
		Unlimited bool
	}{token.Id, initial, token.UnlimitedQuota})
	if err != nil {
		return err
	}
	_, err = applyAccountEventTx(tx, &BillingEvent{EventID: fmt.Sprintf("token-create:%d", token.Id), UserID: token.UserId, TokenID: token.Id, Operation: "token_budget", SourceType: "token_create", SourceID: fmt.Sprint(token.Id), Fingerprint: fingerprint, TokenAvailableDelta: initial, ActorID: token.UserId, Reason: "token budget created"}, &user, false)
	if err != nil {
		return err
	}
	return tx.First(token, token.Id).Error
}

func (token *Token) InsertWithRouteMode(mode string) error {
	if mode != RouteModeManual && mode != RouteModeAutoLab {
		return errors.New("invalid token route mode")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := token.insertWithBudgetEventTx(tx); err != nil {
			return err
		}
		profile := UserRouteProfile{UserID: token.UserId, TokenID: token.Id, Mode: mode}
		profile.Normalize(time.Now())
		return tx.Create(&profile).Error
	})
}

// Update changes metadata only. Budget edits require a stable operation and version.
func (token *Token) Update() error { return token.UpdateWithBudget("", nil) }

func (token *Token) UpdateWithBudget(operationID string, expectedVersion *int64) error {
	return token.updateWithBudget(operationID, expectedVersion, "snapshot")
}

// UpdateFromRequest fingerprints only fields controlled by the HTTP command.
// Later changes to untouched metadata or balances cannot invalidate its replay.
func (token *Token) UpdateFromRequest(operationID string, expectedVersion *int64, statusOnly bool) error {
	mode := "settings"
	if statusOnly {
		mode = "status"
	}
	return token.updateWithBudget(operationID, expectedVersion, mode)
}

func (token *Token) updateWithBudget(operationID string, expectedVersion *int64, mode string) error {
	metadata := map[string]any{
		"name": token.Name, "status": token.Status, "expired_time": token.ExpiredTime,
		"model_limits_enabled": token.ModelLimitsEnabled, "model_limits": token.ModelLimits,
		"allow_ips": token.AllowIps, "conversion_policy": token.ConversionPolicy,
	}
	available, unlimited := token.RemainQuota, token.UnlimitedQuota
	if mode == "settings" {
		delete(metadata, "status")
		if token.ConversionPolicy == "" {
			delete(metadata, "conversion_policy")
		}
	} else if mode == "status" {
		metadata = map[string]any{"status": token.Status}
		available, unlimited = 0, false
	}
	err := BillingTransaction(nil, func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, token.UserId).Error; err != nil {
			return err
		}
		var current Token
		if err := lockForUpdate(tx).First(&current, token.Id).Error; err != nil {
			return err
		}
		if current.UserId != token.UserId {
			return errors.New("token owner mismatch")
		}
		token.Key = current.Key
		targetAvailable, targetUnlimited := token.RemainQuota, token.UnlimitedQuota
		if mode == "status" {
			targetAvailable, targetUnlimited = current.RemainQuota, current.UnlimitedQuota
		}
		budgetChanged := current.RemainQuota != targetAvailable || current.UnlimitedQuota != targetUnlimited
		if operationID != "" {
			if len(operationID) > 96 || expectedVersion == nil {
				return errors.New("expected_version is required")
			}
			fingerprint, err := billingEventFingerprint(struct {
				TokenID   int
				Available common.Quota
				Unlimited bool
				Version   int64
				Metadata  map[string]any
				Mode      string
			}{token.Id, available, unlimited, *expectedVersion, metadata, mode})
			if err != nil {
				return err
			}
			eventID := fmt.Sprintf("token-budget:%d:%s", token.Id, operationID)
			existing, err := findBillingEvent(tx, eventID, fingerprint)
			if err != nil {
				return err
			}
			if existing != nil {
				return nil
			}
			if current.ReservedQuota > 0 && current.UnlimitedQuota != targetUnlimited {
				return errors.New("cannot change token budget policy while reservations are active")
			}
			if current.QuotaVersion != *expectedVersion {
				return ErrBillingEventConflict
			}
			if targetAvailable < 0 && targetAvailable != current.RemainQuota {
				return errors.New("invalid token budget")
			}
			negative, err := common.NegateQuota(current.RemainQuota)
			if err != nil {
				return err
			}
			delta, err := common.AddQuota(targetAvailable, negative)
			if err != nil {
				return err
			}
			if err := tx.Model(&Token{}).Where("id = ?", token.Id).Update("unlimited_quota", targetUnlimited).Error; err != nil {
				return err
			}
			_, err = applyAccountEventTx(tx, &BillingEvent{EventID: eventID, UserID: token.UserId, TokenID: token.Id, Operation: "token_budget", SourceType: "token_adjustment", SourceID: operationID, Fingerprint: fingerprint, TokenAvailableDelta: delta, ActorID: token.UserId, Reason: "token budget updated"}, &user, false)
			if err != nil {
				return err
			}
		} else if budgetChanged {
			return errors.New("budget updates require operation_id and expected_version")
		}
		if mode == "status" && token.Status == common.TokenStatusEnabled {
			if current.ExpiredTime != -1 && current.ExpiredTime <= common.GetTimestamp() {
				return errors.New("expired token cannot be enabled")
			}
			if current.Status == common.TokenStatusExhausted && current.RemainQuota <= 0 && !current.UnlimitedQuota {
				return errors.New("exhausted token cannot be enabled")
			}
		}
		return tx.Model(token).Updates(metadata).Error
	})
	if err != nil {
		return err
	}
	if err := invalidateTokenCacheForMutation(token.Key); err != nil {
		common.SysError("token cache invalidation failed: " + err.Error())
	}
	return DB.First(token, token.Id).Error
}

func (token *Token) SelectUpdate() (err error) {
	if cacheErr := invalidateTokenCacheForMutation(token.Key); cacheErr != nil {
		common.SysLog("failed to invalidate token cache before status update: " + cacheErr.Error())
	}
	// This can update zero values
	return DB.Model(token).Select("accessed_time", "status").Updates(token).Error
}

func (token *Token) Delete() error {
	err := BillingTransaction(nil, func(tx *gorm.DB) error {
		var owner User
		if err := lockForUpdate(tx).First(&owner, token.UserId).Error; err != nil {
			return err
		}
		var current Token
		if err := lockForUpdate(tx).First(&current, token.Id).Error; err != nil {
			return err
		}
		if current.UserId != token.UserId {
			return errors.New("token owner mismatch")
		}
		if current.ReservedQuota != 0 {
			return errors.New("token has active reservations")
		}
		if err := ensureNoActiveAccountBillingTx(tx, token.UserId, token.Id); err != nil {
			return err
		}
		return tx.Delete(&current).Error
	})
	if err != nil {
		return err
	}
	return invalidateTokenCacheForMutation(token.Key)
}

func (token *Token) IsModelLimitsEnabled() bool {
	return token.ModelLimitsEnabled
}

func (token *Token) GetModelLimits() []string {
	if token.ModelLimits == "" {
		return []string{}
	}
	return strings.Split(token.ModelLimits, ",")
}

func (token *Token) GetModelLimitsMap() map[string]bool {
	limits := token.GetModelLimits()
	limitsMap := make(map[string]bool)
	for _, limit := range limits {
		limitsMap[limit] = true
	}
	return limitsMap
}

func DisableModelLimits(tokenId int) error {
	token, err := GetTokenById(tokenId)
	if err != nil {
		return err
	}
	token.ModelLimitsEnabled = false
	token.ModelLimits = ""
	return token.Update()
}

func DeleteTokenById(id int, userId int) (err error) {
	// Why we need userId here? In case user want to delete other's token.
	if id == 0 || userId == 0 {
		return errors.New("id 或 userId 为空！")
	}
	token := Token{Id: id, UserId: userId}
	err = DB.Where(token).First(&token).Error
	if err != nil {
		return err
	}
	return token.Delete()
}

// CountUserTokens returns total number of tokens for the given user, used for pagination
func CountUserTokens(userId int) (int64, error) {
	var total int64
	err := DB.Model(&Token{}).Where("user_id = ?", userId).Count(&total).Error
	return total, err
}

// BatchDeleteTokens 删除指定用户的一组令牌，返回成功删除数量
func BatchDeleteTokens(ids []int, userId int) (int, error) {
	if len(ids) == 0 {
		return 0, errors.New("ids 不能为空！")
	}

	tx := DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	var owner User
	if err := lockForUpdate(tx).First(&owner, userId).Error; err != nil {
		tx.Rollback()
		return 0, err
	}

	var tokens []Token
	if err := lockForUpdate(tx).Where("user_id = ? AND id IN (?)", userId, ids).Order("id ASC").Find(&tokens).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	for _, token := range tokens {
		if err := ensureNoActiveAccountBillingTx(tx, userId, token.Id); err != nil {
			tx.Rollback()
			return 0, err
		}
		if token.ReservedQuota != 0 {
			tx.Rollback()
			return 0, errors.New("token has active reservations")
		}
	}
	if err := invalidateTokensCache(tokens); err != nil {
		common.SysLog("failed to invalidate token cache before batch delete: " + err.Error())
	}

	if err := tx.Where("user_id = ? AND id IN (?)", userId, ids).Delete(&Token{}).Error; err != nil {
		tx.Rollback()
		return 0, err
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}

	return len(tokens), nil
}

func GetTokenKeysByIds(ids []int, userId int) ([]Token, error) {
	var tokens []Token
	err := DB.Select("id", commonKeyCol).
		Where("user_id = ? AND id IN (?)", userId, ids).
		Find(&tokens).Error
	return tokens, err
}

// InvalidateUserTokensCache 清理指定用户所有令牌在 Redis 中的缓存，
// 配合 InvalidateUserCache 使用，可在用户被禁用/删除时立即阻断其令牌的请求。
// 下一次请求将从数据库重新加载令牌及用户状态，从而立即识别出被禁用的用户。
func InvalidateUserTokensCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	if userId <= 0 {
		return errors.New("userId 无效")
	}
	var tokens []Token
	if err := DB.Unscoped().
		Select("id", commonKeyCol).
		Where("user_id = ?", userId).
		Find(&tokens).Error; err != nil {
		return err
	}
	return invalidateTokensCache(tokens)
}

func invalidateTokensCache(tokens []Token) error {
	if !common.RedisEnabled {
		return nil
	}
	var firstErr error
	for _, t := range tokens {
		if t.Key == "" {
			continue
		}
		if err := invalidateTokenCacheForMutation(t.Key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
