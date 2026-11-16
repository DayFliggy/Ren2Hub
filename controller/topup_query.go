package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func topUpRequestFilter(c *gin.Context, userID int) (model.TopUpFilter, error) {
	filter := model.TopUpFilter{UserID: userID, SelfView: userID > 0, Keyword: c.Query("keyword"), Status: c.Query("status"), Method: c.Query("method"), Currency: c.Query("currency")}
	if userID == 0 && c.Query("user_id") != "" {
		id, err := strconv.Atoi(c.Query("user_id"))
		if err != nil || id < 1 {
			return filter, errors.New("invalid user_id")
		}
		filter.UserID = id
	}
	for key, target := range map[string]*int64{"start_time": &filter.StartTime, "end_time": &filter.EndTime} {
		if value := c.Query(key); value != "" {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				return filter, errors.New("invalid time range")
			}
			*target = n
		}
	}
	return filter, nil
}
func listTopUps(c *gin.Context, userID int) {
	filter, err := topUpRequestFilter(c, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page := common.GetPageQuery(c)
	rows, total, err := model.ListTopUps(filter, page)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetItems(rows)
	page.SetTotal(int(total))
	common.ApiSuccess(c, page)
}
func GetTopUpDetail(c *gin.Context) {
	// This user endpoint always enforces ownership. Admin detail has its own route
	// and permission middleware, rather than trusting a numeric role here.
	result, err := model.GetTopUpDetail(c.Param("trade_no"), c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// adminTopUpDetail adds the frozen bonus decision, which may carry shadow
// rates and reasons, so it is never part of the owner's detail.
type adminTopUpDetail struct {
	*model.TopUpDetail
	Bonus *model.TopUpBonusDecisionRecord `json:"bonus"`
}

func GetAdminTopUpDetail(c *gin.Context) {
	result, err := model.GetTopUpDetail(c.Param("trade_no"), 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	bonus, err := model.GetTopUpBonusDecision(model.TopUpBonusSourceTopUp, result.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, adminTopUpDetail{TopUpDetail: result, Bonus: bonus})
}
func GetTopUpStatistics(c *gin.Context) {
	filter, err := topUpRequestFilter(c, 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	loc, ok := viewerLocation(c)
	if !ok {
		return
	}
	result, err := model.GetTopUpStatistics(filter, loc)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetTopUpRanking(c *gin.Context) {
	filter, err := topUpRequestFilter(c, 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	limit := model.TopUpRankingDefaultLimit
	if value := c.Query("limit"); value != "" {
		if limit, err = strconv.Atoi(value); err != nil {
			common.ApiError(c, errors.New("invalid limit"))
			return
		}
	}
	result, err := model.GetTopUpRanking(filter, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
