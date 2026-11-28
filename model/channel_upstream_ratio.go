package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"gorm.io/gorm"
)

// 上游倍率用于展示，开启自动同步的绑定渠道通过系数将有效倍率写入 channel_ratio。
// 写回靠 version 做 CAS，读取时由服务层用 CredentialFP 判断记录是否仍对应当前渠道配置。
const (
	UpstreamRatioSourceSub2API = "sub2api"
	UpstreamRatioSourceNewAPI  = "newapi"

	UpstreamRatioModeAuto   = "auto"
	UpstreamRatioModeManual = "manual"

	UpstreamRatioStatusOK           = "ok"
	UpstreamRatioStatusNeedsMapping = "needs_mapping"
	UpstreamRatioStatusUnsupported  = "unsupported"
	UpstreamRatioStatusAuthFailed   = "auth_failed"
	UpstreamRatioStatusRateLimited  = "rate_limited"
	UpstreamRatioStatusFailed       = "failed"

	UpstreamRatioMappingNameMatch = "name_match"
	UpstreamRatioMappingManual    = "manual"
)

var ErrUpstreamRatioConflict = errors.New("channel upstream ratio changed concurrently")

type ChannelUpstreamRatio struct {
	ChannelId    int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	Source       string `json:"source" gorm:"type:varchar(16);not null;default:''"`
	Mode         string `json:"mode" gorm:"type:varchar(16);not null;default:'auto'"`
	Status       string `json:"status" gorm:"type:varchar(32);not null;default:''"`
	Ratio        string `json:"ratio" gorm:"type:varchar(64);not null;default:''"`
	GroupName    string `json:"group_name" gorm:"type:varchar(255);not null;default:''"`
	Mapping      string `json:"mapping" gorm:"type:varchar(16);not null;default:''"`
	TokenName    string `json:"token_name" gorm:"type:varchar(255);not null;default:''"`
	Detail       string `json:"detail" gorm:"type:text"`
	CredentialFP string `json:"-" gorm:"column:credential_fp;type:varchar(64);not null;default:''"`
	// ChannelCreatedTime 记录所属渠道的创建时间。渠道删除后可用同一 ID 重建，创建时间不同即为另一代渠道。
	ChannelCreatedTime int64  `json:"-" gorm:"bigint;not null;default:0"`
	Version            int64  `json:"version" gorm:"not null;default:0"`
	ObservedAt         int64  `json:"observed_at" gorm:"bigint;not null;default:0"`
	ReceivedAt         int64  `json:"received_at" gorm:"bigint;not null;default:0"`
	FreshUntil         int64  `json:"fresh_until" gorm:"bigint;not null;default:0"`
	NextProbeAt        int64  `json:"next_probe_at" gorm:"bigint;not null;default:0;index"`
	HTTPStatus         int    `json:"http_status" gorm:"not null;default:0"`
	LastError          string `json:"last_error" gorm:"type:varchar(64);not null;default:''"`
	FailureCount       int    `json:"failure_count" gorm:"not null;default:0"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint;not null;default:0"`
}

// BelongsTo 判断记录是否属于渠道的当前这一代。
func (record *ChannelUpstreamRatio) BelongsTo(channel *Channel) bool {
	return record.ChannelId == channel.Id && record.ChannelCreatedTime == channel.CreatedTime
}

func GetChannelUpstreamRatio(channelID int) (*ChannelUpstreamRatio, error) {
	var record ChannelUpstreamRatio
	err := DB.Where("channel_id = ?", channelID).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func GetChannelUpstreamRatios(channelIDs []int) ([]ChannelUpstreamRatio, error) {
	var records []ChannelUpstreamRatio
	if len(channelIDs) == 0 {
		return records, nil
	}
	err := DB.Where("channel_id IN ?", channelIDs).Order("channel_id").Find(&records).Error
	return records, err
}

// SaveChannelUpstreamRatio 以 expectedVersion 做 CAS 写回：expectedVersion 为 0 表示
// 读取时还没有当前这一代渠道的记录，此时先删除同 ID 上一代渠道留下的记录，再插入。
// verify 在同一事务内拿到最新渠道，返回错误即放弃写入，用来拦截探测期间渠道被删除或配置被修改的情况。
// record.ChannelCreatedTime 由调用方填入请求开始时读到的渠道代次；与事务内锁定的渠道不一致说明
// 渠道已被删除并用同一 ID 重建，此时拒绝写入，避免旧渠道的在途写回覆盖新渠道的记录。
func SaveChannelUpstreamRatio(record *ChannelUpstreamRatio, expectedVersion int64, verify func(*Channel) error) error {
	return channelBindingTransaction(DB, func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).Where("id = ?", record.ChannelId).Take(&channel).Error; err != nil {
			return err
		}
		if verify != nil {
			if err := verify(&channel); err != nil {
				return err
			}
		}
		if record.ChannelCreatedTime != channel.CreatedTime {
			return ErrUpstreamRatioConflict
		}
		record.Version = expectedVersion + 1
		if expectedVersion == 0 {
			if err := tx.Where("channel_id = ? AND channel_created_time <> ?", record.ChannelId, channel.CreatedTime).
				Delete(&ChannelUpstreamRatio{}).Error; err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&ChannelUpstreamRatio{}).Where("channel_id = ?", record.ChannelId).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrUpstreamRatioConflict
			}
			return tx.Create(record).Error
		}
		result := tx.Model(&ChannelUpstreamRatio{}).
			Where("channel_id = ? AND version = ? AND channel_created_time = ?", record.ChannelId, expectedVersion, channel.CreatedTime).
			Select("*").Omit("channel_id").Updates(record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrUpstreamRatioConflict
		}
		return nil
	})
}

// orphanUpstreamRatios 选出渠道已不存在、或渠道已用同一 ID 重建（创建时间不同）的记录。
func orphanUpstreamRatios(db *gorm.DB) *gorm.DB {
	current := DB.Model(&Channel{}).Select("id, COALESCE(created_time, 0)")
	return db.Where("(channel_id, channel_created_time) NOT IN (?)", current)
}

// DeleteOrphanChannelUpstreamRatios 删除孤儿记录。渠道删除走 NewAPI 原生路径，
// 不在那里级联，由调度器每轮清理。
func DeleteOrphanChannelUpstreamRatios() (int64, error) {
	result := orphanUpstreamRatios(DB).Delete(&ChannelUpstreamRatio{})
	return result.RowsAffected, result.Error
}

// GetDueUpstreamRatioChannels 返回到期需要探测的渠道：type 59/60 以及绑定到活跃 Sub2API 上游账号
// 且尚无记录的渠道视为到期；已有记录的只看自动模式、已确定来源且 next_probe_at 已过的。
func GetDueUpstreamRatioChannels(now int64, limit int) ([]*Channel, error) {
	var channels []*Channel
	recorded := DB.Model(&ChannelUpstreamRatio{}).Select("channel_id")
	due := DB.Model(&ChannelUpstreamRatio{}).Select("channel_id").
		Where("mode = ? AND source <> ? AND next_probe_at <= ?", UpstreamRatioModeAuto, "", now)
	newAPIAccounts := DB.Model(&UpstreamAccount{}).Select("id").Where("status = ? AND platform = ?", "active", UpstreamRatioSourceNewAPI)
	sub2APIAccounts := DB.Model(&UpstreamAccount{}).Select("id").Where("status = ? AND platform = ?", "active", UpstreamRatioSourceSub2API)
	if err := CheckUpstreamBindingIdentities(context.Background(), 0); err != nil {
		return nil, err
	}
	managedNewAPI := DB.Model(&UpstreamChannelBinding{}).Select("channel_id, channel_created_time").Where("stale = ? AND account_id IN (?)", false, newAPIAccounts)
	managedSub2API := DB.Model(&UpstreamChannelBinding{}).Select("channel_id, channel_created_time").Where("stale = ? AND account_id IN (?)", false, sub2APIAccounts)
	initialDue := DB.Where("id NOT IN (?)", recorded).Where(
		DB.Where("type IN ?", []int{constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI}).
			Or("(id, COALESCE(created_time,0)) IN (?)", managedSub2API),
	)
	err := DB.Where("(id, COALESCE(created_time,0)) NOT IN (?)", managedNewAPI).
		Where(DB.Where(initialDue).Or("id IN (?)", due)).
		Order("id").Limit(limit).Find(&channels).Error
	return channels, err
}

func HasOrphanChannelUpstreamRatios() (bool, error) {
	var count int64
	err := orphanUpstreamRatios(DB.Model(&ChannelUpstreamRatio{})).Count(&count).Error
	return count > 0, err
}

func HasDueUpstreamRatioChannels(now int64) (bool, error) {
	channels, err := GetDueUpstreamRatioChannels(now, 1)
	return len(channels) > 0, err
}

var ErrUpstreamRatioBaseURL = errors.New("invalid base url")
var ErrUpstreamRatioSetting = errors.New("invalid channel setting")

// UpstreamRatioTarget 是一次探测实际使用的请求要素，也是指纹的输入。
type UpstreamRatioTarget struct {
	Type    int    `json:"type"`
	BaseURL string `json:"base_url"`
	Key     string `json:"key"`
	Proxy   string `json:"proxy"`
}

// UpstreamProbeBaseURL 保留部署路径前缀，去掉末尾的协议或 antigravity 渠道路径。
func UpstreamProbeBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	// 走代理时目标主机不经过本地拨号检查，带 zone 的 IPv6 目标在这里统一拒绝。
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || strings.Contains(parsed.Hostname(), "%") {
		return "", ErrUpstreamRatioBaseURL
	}
	path := strings.TrimRight(parsed.Path, "/")
	for _, suffix := range []string{"/v1", "/v1beta", "/antigravity"} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			break
		}
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func NewUpstreamRatioTarget(channel *Channel) (UpstreamRatioTarget, error) {
	baseURL, err := UpstreamProbeBaseURL(channel.GetBaseURL())
	if err != nil {
		return UpstreamRatioTarget{}, err
	}
	proxy, err := upstreamRatioProxy(channel)
	if err != nil {
		return UpstreamRatioTarget{}, err
	}
	return UpstreamRatioTarget{
		Type:    channel.Type,
		BaseURL: baseURL,
		Key:     strings.TrimSpace(channel.Key),
		Proxy:   proxy,
	}, nil
}

// upstreamRatioProxy 只解析渠道设置。原生 GetSetting 遇到坏 JSON 会清空设置并写库，
// 读取倍率的路径不能有这种副作用。
func upstreamRatioProxy(channel *Channel) (string, error) {
	if channel.Setting == nil || *channel.Setting == "" {
		return "", nil
	}
	var setting dto.ChannelSettings
	if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
		return "", ErrUpstreamRatioSetting
	}
	return strings.TrimSpace(setting.Proxy), nil
}

// UpstreamRatioFingerprint 对类型、探测地址、Key、代理的结构化编码取 sha256。
// 渠道配置变化后指纹不同，旧记录在读取时显示为过期，写回时被拒绝。
func UpstreamRatioFingerprint(channel *Channel) string {
	target, err := NewUpstreamRatioTarget(channel)
	// Billing probes share the root endpoint, but /antigravity serves a different
	// relay path. Keep the configured path in its pre-management fingerprint.
	if raw, parseErr := url.Parse(channel.GetBaseURL()); err == nil && parseErr == nil && strings.HasSuffix(strings.TrimRight(raw.Path, "/"), "/antigravity") {
		target.BaseURL += "/antigravity"
	}
	if err != nil {
		target = UpstreamRatioTarget{Type: channel.Type, BaseURL: channel.GetBaseURL(), Key: channel.Key}
		if channel.Setting != nil {
			target.Proxy = *channel.Setting
		}
	}
	encoded, err := common.Marshal(target)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
