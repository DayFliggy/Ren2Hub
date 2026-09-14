package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelBoundsDistinctScansWhenCandidatesBecomeDisabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	selection := service.LiveRouteSelection{Source: service.RouteSourceAutoLab}
	for id := 1; id <= service.MaxRouteCandidateChannels+1; id++ {
		require.NoError(t, db.Create(&model.Channel{Id: id, Status: common.ChannelStatusManuallyDisabled}).Error)
		selection.Attempts = append(selection.Attempts, service.RouteDecisionCandidate{ChannelID: id}, service.RouteDecisionCandidate{ChannelID: id})
	}
	c.Set("route_live_selection", selection)
	selected, apiErr := getChannel(c, &relaycommon.RelayInfo{}, &service.RetryParam{Ctx: c, Retry: common.GetPointer(0)})
	assert.Nil(t, selected)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.ErrorContains(t, apiErr, "scan limit")
	assert.Equal(t, service.MaxRouteCandidateChannels, c.GetInt("gateway_scanned_candidates"))
	assert.Zero(t, liveRouteRetryCounters(c).TotalAttempts)
}
