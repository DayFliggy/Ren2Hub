package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNewAPIChannelBalanceContract(t *testing.T) {
	db := setupSub2APIBalanceTestDB(t)
	for _, tc := range []struct {
		name, data, status string
		want               float64
		invalid            bool
	}{
		{name: "limited numeric key", data: `{"total_available":1000000,"unlimited_quota":false}`, want: 2},
		{name: "string quotas and unit", data: `{"total_available":"1500000","unlimited_quota":false}`, status: `{"data":{"quota_per_unit":"500000"}}`, want: 3},
		{name: "owner smaller", data: `{"total_available":1000000,"unlimited_quota":false,"user_quota":{"total_available":250000}}`, want: .5},
		{name: "key smaller", data: `{"total_available":250000,"unlimited_quota":false,"user_quota":{"total_available":"1000000"}}`, want: .5},
		{name: "unlimited key", data: `{"total_available":null,"unlimited_quota":true,"user_quota":{"total_available":"1000000"}}`, want: 2},
		{name: "zero is present", data: `{"total_available":0,"unlimited_quota":false,"user_quota":{"total_available":100}}`},
		{name: "wallet debt", data: `{"total_available":1000000,"unlimited_quota":false,"user_quota":{"total_available":-250000}}`, want: -.5},
		{name: "exact values before float boundary", data: `{"total_available":"9007199254740993","unlimited_quota":false,"user_quota":{"total_available":9007199254740993}}`, status: `{"data":{"quota_per_unit":9007199254740993}}`, want: 1},
		{name: "small exact ratio", data: `{"total_available":"0.000000000000000001","unlimited_quota":false}`, status: `{"data":{"quota_per_unit":"1000000000000000000"}}`, want: 1e-36},
		{name: "unlimited without owner", data: `{"total_available":null,"unlimited_quota":true}`, invalid: true},
		{name: "missing key quota", data: `{"unlimited_quota":false}`, invalid: true},
		{name: "missing unlimited flag", data: `{"total_available":1}`, invalid: true},
		{name: "null limited quota", data: `{"total_available":null,"unlimited_quota":false}`, invalid: true},
		{name: "missing owner quota", data: `{"total_available":1,"unlimited_quota":false,"user_quota":{}}`, invalid: true},
		{name: "invalid quota", data: `{"total_available":"NaN","unlimited_quota":false}`, invalid: true},
		{name: "oversized exponent", data: `{"total_available":1e1000000,"unlimited_quota":false}`, invalid: true},
		{name: "missing unit", data: `{"total_available":1,"unlimited_quota":false}`, status: `{"data":{}}`, invalid: true},
		{name: "zero unit", data: `{"total_available":1,"unlimited_quota":false}`, status: `{"data":{"quota_per_unit":0}}`, invalid: true},
		{name: "negative unit", data: `{"total_available":1,"unlimited_quota":false}`, status: `{"data":{"quota_per_unit":-1}}`, invalid: true},
		{name: "failed status", data: `{"total_available":1,"unlimited_quota":false}`, status: `{"success":false,"data":{"quota_per_unit":1}}`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				if r.URL.Path == "/prefix/api/usage/token/" {
					assert.Equal(t, "Bearer fixture-key", r.Header.Get("Authorization"))
					_, _ = io.WriteString(w, `{"code":true,"data":`+tc.data+`}`)
					return
				}
				assert.Equal(t, "/prefix/api/status", r.URL.Path)
				assert.Empty(t, r.Header.Get("Authorization"))
				status := tc.status
				if status == "" {
					status = `{"success":true,"data":{"quota_per_unit":500000}}`
				}
				_, _ = io.WriteString(w, status)
			}))
			t.Cleanup(server.Close)
			baseURL := server.URL + "/prefix///"
			channel := model.Channel{Type: constant.ChannelTypeNewAPI, BaseURL: &baseURL, Key: "fixture-key", Balance: 7}
			require.NoError(t, db.Create(&channel).Error)
			result, err := updateChannelBalance(context.Background(), &channel)
			var saved model.Channel
			require.NoError(t, db.First(&saved, channel.Id).Error)
			if tc.invalid {
				require.Error(t, err)
				assert.Equal(t, 7.0, saved.Balance)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, result.Balance)
			assert.Equal(t, tc.want, saved.Balance)
		})
	}
}

func TestNewAPIBalanceTransportAndSaveFailure(t *testing.T) {
	db := setupSub2APIBalanceTestDB(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			assert.Empty(t, r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, `{"data":{"quota_per_unit":500000}}`)
			return
		}
		assert.Equal(t, "/api/usage/token/", r.URL.Path)
		_, _ = io.WriteString(w, `{"code":true,"data":{"total_available":"500000","unlimited_quota":false}}`)
	})
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	channel := model.Channel{Type: constant.ChannelTypeNewAPI, BaseURL: &server.URL, Balance: 7}
	require.NoError(t, db.Create(&channel).Error)
	_, err := updateChannelBalance(context.Background(), &channel)
	require.ErrorContains(t, err, "certificate")
	channel.SetSetting(dto.ChannelSettings{TLSInsecureSkipVerify: true})
	result, err := updateChannelBalance(context.Background(), &channel)
	require.NoError(t, err)
	assert.Equal(t, 1.0, result.Balance)
	proxy := httptest.NewServer(handler)
	t.Cleanup(proxy.Close)
	channel.BaseURL = common.GetPointer("http://newapi.fixture.invalid")
	channel.SetSetting(dto.ChannelSettings{Proxy: proxy.URL})
	_, err = updateChannelBalance(context.Background(), &channel)
	require.NoError(t, err)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fixture:newapi-save-failure", func(tx *gorm.DB) { tx.AddError(errors.New("fixture persistence failure")) }))
	_, err = updateChannelBalance(context.Background(), &channel)
	require.ErrorContains(t, err, "fixture persistence failure")
}

func TestNewAPIBalanceMasksEveryErrorExit(t *testing.T) {
	db := setupSub2APIBalanceTestDB(t)
	for _, kind := range []string{"provider", "decimal", "transport"} {
		t.Run(kind, func(t *testing.T) {
			key := "newapi-private-key-fixture"
			var baseURL string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := `{"code":false,"message":"` + key + ` ` + baseURL + `/private-path; https://docs.example.org/error"}`
				if kind == "decimal" {
					body = `{"code":true,"data":{"unlimited_quota":false,"total_available":"` + key + `"}}`
				}
				_, _ = io.WriteString(w, body)
			}))
			baseURL = server.URL
			t.Cleanup(server.Close)
			if kind == "transport" {
				server.Close()
			}
			channel := model.Channel{Type: constant.ChannelTypeNewAPI, BaseURL: &baseURL, Key: key, Balance: 7}
			require.NoError(t, db.Create(&channel).Error)
			_, err := updateChannelNewAPIBalance(&channel)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), key)
			assert.NotContains(t, err.Error(), "127.0.0.1")
			if kind == "provider" {
				assert.Contains(t, err.Error(), "https://docs.example.org/error")
				assert.NotContains(t, err.Error(), "private-path")
			}
			var saved model.Channel
			require.NoError(t, db.First(&saved, channel.Id).Error)
			assert.Equal(t, 7.0, saved.Balance)
		})
	}
}
