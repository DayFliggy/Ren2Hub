package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdvancedCustomBalancePreservesUnknownJSONAndNumericContract(t *testing.T) {
	for _, test := range []struct {
		name, body string
		numeric    bool
	}{
		{"credit summary", `{"object":"credit_summary","total_available":12.75}`, true},
		{"provider shape", `{"wallet":{"credits":"12.7500001"},"currency":"CNY"}`, false},
		{"invalid amount stays raw", `{"object":"credit_summary","total_available":-1}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			previousCache := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = false
			t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
			sends := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends++
				assert.Equal(t, "/balance", r.URL.Path)
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "replacement", r.Header.Get("X-API-Key"))
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, Key: "secret", BaseURL: &server.URL, Balance: 7}
			channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
				IncomingPath: dto.AdvancedCustomBalancePath, UpstreamPath: "/balance", Converter: "none",
				Auth: &dto.AdvancedCustomRouteAuth{Type: dto.AdvancedCustomAuthTypeHeader, Name: "X-API-Key", Value: "{api_key}"},
			}}}})
			channel.HeaderOverride = common.GetPointer(`{"X-API-Key":"replacement"}`)
			require.NoError(t, db.Create(channel).Error)
			router := gin.New()
			router.GET("/balance/:id", UpdateChannelBalance)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/balance/"+strconv.Itoa(channel.Id), nil))
			require.Equal(t, http.StatusOK, response.Code)
			var envelope struct {
				Success bool           `json:"success"`
				Data    map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
			require.True(t, envelope.Success, response.Body.String())
			assert.Equal(t, 1, sends)
			assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			var saved model.Channel
			require.NoError(t, db.First(&saved, channel.Id).Error)
			if test.numeric {
				assert.Equal(t, 12.75, envelope.Data["balance"])
				assert.NotContains(t, envelope.Data, "raw_response")
				assert.Equal(t, 12.75, saved.Balance)
			} else {
				assert.NotContains(t, envelope.Data, "balance")
				raw, ok := envelope.Data["raw_response"].(string)
				require.True(t, ok)
				assert.JSONEq(t, test.body, raw)
				assert.Equal(t, 7.0, saved.Balance)
			}
		})
	}
}

func TestAdvancedCustomBalanceRejectsInvalidOrOversizedResponses(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"invalid JSON", `{broken`, 200},
		{"oversized", `"` + strings.Repeat("a", maxAdvancedCustomBalanceResponseBytes) + `"`, 200},
		{"provider rejected", `{"secret":"never expose failure body"}`, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, BaseURL: &server.URL}
			channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
				IncomingPath: dto.AdvancedCustomBalancePath, UpstreamPath: "/balance", Converter: "none",
			}}}})
			result, err := updateChannelBalance(context.Background(), channel)
			require.Error(t, err)
			assert.Empty(t, result.RawResponse)
			assert.NotContains(t, err.Error(), "never expose")
		})
	}
}
