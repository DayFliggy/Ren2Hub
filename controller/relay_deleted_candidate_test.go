package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelSkipsDeletedCandidateAndPreservesDatabaseErrors(t *testing.T) {
	for _, failQuery := range []bool{false, true} {
		name := "deleted candidate"
		if failQuery {
			name = "database failure"
		}
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelHealth{}))
			previousDB, previousCache := model.DB, common.MemoryCacheEnabled
			previousType := common.MainDatabaseType()
			model.DB, common.MemoryCacheEnabled = db, false
			common.SetMainDatabaseType(common.DatabaseTypeSQLite)
			t.Cleanup(func() {
				model.DB, common.MemoryCacheEnabled = previousDB, previousCache
				common.SetMainDatabaseType(previousType)
				connection, closeErr := db.DB()
				require.NoError(t, closeErr)
				require.NoError(t, connection.Close())
			})
			fallback := model.Channel{Id: 92002, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "fallback-fixture", ChannelRatio: "1"}
			require.NoError(t, db.Create(&fallback).Error)
			queryErr := errors.New("fixture database read failure")
			if failQuery {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:query-error", func(tx *gorm.DB) { tx.AddError(queryErr) }))
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("route_live_selection", service.LiveRouteSelection{
				Source:   service.RouteSourceAutoLab,
				Attempts: []service.RouteDecisionCandidate{{ChannelID: 92001}, {ChannelID: 92001}, {ChannelID: fallback.Id}},
			})
			param := &service.RetryParam{Ctx: c, Retry: common.GetPointer(1)}
			selected, apiErr := getChannel(c, &relaycommon.RelayInfo{OriginModelName: "fixture-model"}, param)
			if failQuery {
				require.NotNil(t, apiErr)
				assert.ErrorIs(t, apiErr, queryErr)
				assert.Nil(t, selected)
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, selected)
			assert.Equal(t, fallback.Id, selected.Id)
			assert.Equal(t, "fallback-fixture", common.GetContextKeyString(c, constant.ContextKeyChannelKey))
			assert.Equal(t, 2, param.GetRetry())
			assert.Zero(t, liveRouteRetryCounters(c).TotalAttempts, "a deleted candidate is not an upstream attempt")
			selection, _ := c.Get("route_live_selection")
			for _, candidate := range selection.(service.LiveRouteSelection).Attempts[:2] {
				assert.Equal(t, service.RouteFilterChannelDisabled, candidate.FilterReason)
			}
		})
	}
}
