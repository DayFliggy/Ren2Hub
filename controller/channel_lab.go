package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// channelLabFilterIDs 把 lab 查询参数解析为渠道 id。nil 表示不按 lab 筛选，
// 空切片表示该 lab 没有任何渠道。
func channelLabFilterIDs(c *gin.Context) ([]int, error) {
	lab := c.Query("lab")
	if lab == "" {
		return nil, nil
	}
	labs, err := model.AllChannelLabs(c.Request.Context())
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0)
	for id, value := range labs {
		if value.ID == lab {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// applyChannelLabFilter 在渠道列表查询上应用 lab 筛选，取值语义见 channelLabFilterIDs。
func applyChannelLabFilter(query *gorm.DB, labIDs []int) *gorm.DB {
	if labIDs == nil {
		return query
	}
	if len(labIDs) == 0 {
		return query.Where("1 = 0")
	}
	return query.Where("id IN ?", labIDs)
}

// GetChannelLabs 返回各 lab 的渠道数量，供管理端渠道筛选使用。
func GetChannelLabs(c *gin.Context) {
	items, total, err := model.ChannelLabCounts(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"total": total, "items": items})
}
