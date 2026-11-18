package service

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAutomaticRouteScanSummaries(t *testing.T) {
	db := setupAffinityChannels(t)
	require.NoError(t, db.Model(&model.Channel{}).Where("id IN ?", []int{11, 22}).Update("max_concurrency", 1).Error)
	first := routeTestContext(t)
	_, selectErr := SelectChannelForRequest(first, "route-model", nil)
	require.Nil(t, selectErr)
	c := routeTestContext(t)
	requestID := c.GetString(common.RequestIdKey)
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		defer common.LogWriterMu.Unlock()
		gin.DefaultErrorWriter = oldWriter
	})

	channel, selectErr := SelectChannelForRequest(c, "route-model", nil)
	require.Nil(t, selectErr)
	require.Equal(t, 22, channel.Id)
	full := routeTestContext(t)
	_, selectErr = SelectChannelForRequest(full, "route-model", nil)
	require.Equal(t, http.StatusTooManyRequests, selectErr.StatusCode)
	require.Contains(t, output.String(), full.GetString(common.RequestIdKey))
	require.Contains(t, output.String(), "round=1 candidates=2 full_skips=2 result=full")

	AppendUsedChannel(c, 22)
	AppendUsedChannel(c, 11)
	_, selectErr = SelectChannelForRequest(c, "route-model", nil)
	require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
	require.Contains(t, output.String(), requestID)
	require.Contains(t, output.String(), "round=2 candidates=2 full_skips=0 result=no_candidate")
	require.Equal(t, 2, strings.Count(output.String(), "route_scan "))
	other := model.NewLogOther()
	AppendRelayLogAdminInfo(c, nil, other)
	type scan struct {
		Round      int
		Candidates int
		FullSkips  int    `json:"full_skips"`
		DurationUS *int64 `json:"duration_us"`
	}
	var payload struct {
		Admin struct {
			Events []struct {
				Scan     *scan
				Decision PolicyDecision
			} `json:"request_policy"`
		} `json:"admin_info"`
	}
	require.NoError(t, common.Unmarshal([]byte(other.JSONString()), &payload))
	require.Len(t, payload.Admin.Events, 2)
	for i, event := range payload.Admin.Events {
		require.NotNil(t, event.Scan)
		require.Equal(t, i+1, event.Scan.Round)
		require.Equal(t, 2, event.Scan.Candidates)
		require.NotNil(t, event.Scan.DurationUS)
		require.GreaterOrEqual(t, *event.Scan.DurationUS, int64(0))
	}
	require.Equal(t, 1, payload.Admin.Events[0].Scan.FullSkips)
	require.Equal(t, "selected", payload.Admin.Events[0].Decision.Reason)
	require.Equal(t, 0, payload.Admin.Events[1].Scan.FullSkips)
	require.Equal(t, "no_candidate", payload.Admin.Events[1].Decision.Reason)
}

func TestBoundAndManualRoutesDoNotEmitScan(t *testing.T) {
	setupAffinityChannels(t)
	affinity, _ := enableRouteAffinity(t, "prefer", 22)
	pinned := routeTestContext(t)
	GetChannelConstraints(pinned).AddPin(dto.ChannelPin{Source: dto.PinSourceToken, ChannelId: 22})
	for _, c := range []*gin.Context{affinity, pinned} {
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
		for _, event := range RequestPolicy(c).Events() {
			require.Nil(t, event.Scan)
		}
	}
	saveRouteTest(t, 11, 22)
	manual := routeTestContext(t)
	_, selectErr := SelectChannelForRequest(manual, "route-model", nil)
	require.Nil(t, selectErr)
	require.Empty(t, RequestPolicy(manual).Events())
}
