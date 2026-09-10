package model

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index"`
	Username  string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	TokenID   int    `json:"token_id" gorm:"index;default:0"`
	ChannelID int    `json:"channel_id" gorm:"index;default:0"`
	NodeName  string `json:"node_name" gorm:"index;size:64;default:''"`
	TokenUsed int    `json:"token_used" gorm:"default:0"`
	Count     int    `json:"count" gorm:"default:0"`
	Quota     int    `json:"quota" gorm:"default:0"`
}

type QuotaDataLogParams struct {
	UserID    int
	Username  string
	ModelName string
	Quota     int
	CreatedAt int64
	TokenUsed int
	TokenID   int
	ChannelID int
	NodeName  string
}

func UpdateQuotaData() {
	modelBackground.Go(func(ctx context.Context) {
		interval := time.Duration(common.DataExportInterval) * time.Minute
		if interval <= 0 {
			interval = time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if common.DataExportEnabled {
				if err := SaveQuotaDataCacheContext(ctx); err != nil {
					common.SysError("quota data flush failed: " + err.Error())
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}

func logQuotaDataCache(quotaData *QuotaData) {
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%d\x00%d\x00%d\x00%s",
		quotaData.UserID,
		quotaData.Username,
		quotaData.ModelName,
		quotaData.CreatedAt,
		quotaData.TokenID,
		quotaData.ChannelID,
		quotaData.NodeName,
	)
	count := quotaData.Count
	quota := quotaData.Quota
	tokenUsed := quotaData.TokenUsed
	cachedQuotaData, ok := CacheQuotaData[key]
	if ok {
		cachedQuotaData.Count += count
		cachedQuotaData.Quota += quota
		cachedQuotaData.TokenUsed += tokenUsed
		quotaData = cachedQuotaData
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(params QuotaDataLogParams) {
	// 只精确到小时
	createdAt := params.CreatedAt - (params.CreatedAt % 3600)
	quotaData := &QuotaData{
		UserID:    params.UserID,
		Username:  params.Username,
		ModelName: params.ModelName,
		CreatedAt: createdAt,
		TokenID:   params.TokenID,
		ChannelID: params.ChannelID,
		NodeName:  params.NodeName,
		Count:     1,
		Quota:     params.Quota,
		TokenUsed: params.TokenUsed,
	}

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(quotaData)
}

var quotaDataFlushLock sync.Mutex
var pendingQuotaData map[string]*QuotaData
var pendingQuotaDataID string

func SaveQuotaDataCache() error { return SaveQuotaDataCacheContext(context.Background()) }
func SaveQuotaDataCacheContext(ctx context.Context) error {
	quotaDataFlushLock.Lock()
	defer quotaDataFlushLock.Unlock()
	// Resolve a frozen batch before taking new arrivals: an operation ID always
	// denotes exactly the same data, including after an uncertain commit.
	rounds := 1
	if pendingQuotaData != nil {
		rounds = 2
	}
	for range rounds {
		if pendingQuotaData == nil {
			CacheQuotaDataLock.Lock()
			if len(CacheQuotaData) == 0 {
				CacheQuotaDataLock.Unlock()
				return nil
			}
			pendingQuotaData = CacheQuotaData
			CacheQuotaData = make(map[string]*QuotaData)
			CacheQuotaDataLock.Unlock()
			pendingQuotaDataID = common.GetUUID()
		}
		err := ApplyFlushOperation(ctx, pendingQuotaDataID, func(tx *gorm.DB) error {
			for _, data := range pendingQuotaData {
				var row QuotaData
				query := tx.Where("user_id = ? and username = ? and model_name = ? and created_at = ? and token_id = ? and channel_id = ? and node_name = ?", data.UserID, data.Username, data.ModelName, data.CreatedAt, data.TokenID, data.ChannelID, data.NodeName)
				err := query.First(&row).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					copy := *data
					copy.Id = 0
					if err := tx.Create(&copy).Error; err != nil {
						return err
					}
					continue
				}
				if err != nil {
					return err
				}
				if err := tx.Model(&QuotaData{}).Where("id = ?", row.Id).Updates(map[string]any{"count": gorm.Expr("count + ?", data.Count), "quota": gorm.Expr("quota + ?", data.Quota), "token_used": gorm.Expr("token_used + ?", data.TokenUsed)}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		pendingQuotaData = nil
		pendingQuotaDataID = ""
	}
	return nil
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	// only select model_name, sum(count) as count, sum(quota) as quota, model_name, created_at from quota_data group by model_name, created_at;
	//err = DB.Table("quota_data").Where("created_at >= ? and created_at <= ?", startTime, endTime).Find(&quotaDatas).Error
	err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}
