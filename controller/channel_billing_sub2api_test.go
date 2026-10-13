package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSub2APIBalanceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	previousDB, previousCache := model.DB, common.MemoryCacheEnabled
	model.DB, common.MemoryCacheEnabled = db, false
	t.Cleanup(func() {
		model.DB, common.MemoryCacheEnabled = previousDB, previousCache
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestSub2APIBalanceQueryContract(t *testing.T) {
	db := setupSub2APIBalanceTestDB(t)
	for _, test := range []struct {
		name, body string
		status     int
		want       float64
		wantError  bool
	}{
		{name: "remaining takes precedence", body: `{"remaining":986.17194888,"balance":100,"isValid":true}`, want: 986.17194888},
		{name: "wallet fallback", body: `{"balance":42.5}`, want: 42.5},
		{name: "explicit zero remaining", body: `{"remaining":0,"balance":100}`, want: 0},
		{name: "negative wallet", body: `{"remaining":-1,"balance":-1,"isValid":true}`, want: -1},
		{name: "limited subscription", body: `{"remaining":12.5,"subscription":{"daily_limit_usd":20}}`, want: 12.5},
		{name: "unlimited subscription", body: `{"remaining":-1,"subscription":{"daily_limit_usd":null,"weekly_limit_usd":null,"monthly_limit_usd":null}}`, wantError: true},
		{name: "missing balance", body: `{"isValid":true}`, wantError: true},
		{name: "null balance", body: `{"remaining":null,"balance":null}`, wantError: true},
		{name: "invalid key with stale balance", body: `{"isValid":false,"remaining":100}`, wantError: true},
		{name: "invalid JSON", body: `{broken`, wantError: true},
		{name: "string balance", body: `{"balance":"42.5"}`, wantError: true},
		{name: "overflow", body: `{"balance":1e999}`, wantError: true},
		{name: "not a number", body: `{"balance":NaN}`, wantError: true},
		{name: "upstream error", body: `{"balance":100,"message":"private provider error"}`, status: http.StatusForbidden, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/prefix/v1/usage", r.URL.Path)
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "Bearer fixture-key", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if test.status != 0 {
					w.WriteHeader(test.status)
				}
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			baseURL := server.URL + "/prefix///"
			channel := &model.Channel{Type: constant.ChannelTypeSub2API, BaseURL: &baseURL, Key: "fixture-key", Balance: 7, BalanceUpdatedTime: 1}
			require.NoError(t, db.Create(channel).Error)
			router := gin.New()
			router.GET("/balance/:id", UpdateChannelBalance)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/balance/"+strconv.Itoa(channel.Id), nil))
			require.Equal(t, http.StatusOK, response.Code)
			var envelope struct {
				Success bool     `json:"success"`
				Balance *float64 `json:"balance"`
				Data    *struct {
					Balance float64 `json:"balance"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
			var saved model.Channel
			require.NoError(t, db.First(&saved, channel.Id).Error)
			if test.wantError {
				assert.False(t, envelope.Success)
				assert.Nil(t, envelope.Balance)
				assert.Equal(t, 7.0, saved.Balance)
				assert.EqualValues(t, 1, saved.BalanceUpdatedTime)
				assert.NotContains(t, response.Body.String(), "private provider error")
				return
			}
			require.True(t, envelope.Success, response.Body.String())
			require.NotNil(t, envelope.Data)
			require.NotNil(t, envelope.Balance)
			assert.Equal(t, test.want, *envelope.Balance)
			assert.Equal(t, test.want, envelope.Data.Balance)
			assert.Equal(t, test.want, saved.Balance)
			assert.Greater(t, saved.BalanceUpdatedTime, int64(1))
			assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		})
	}
}

func TestSub2APIBalanceRequiresBaseURL(t *testing.T) {
	_, err := updateChannelBalance(context.Background(), &model.Channel{Type: constant.ChannelTypeSub2API})
	require.ErrorContains(t, err, "missing base_url")
}

func TestSub2APIBalancePersistenceFailure(t *testing.T) {
	db := setupSub2APIBalanceTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"remaining":99}`)
	}))
	t.Cleanup(server.Close)
	channel := &model.Channel{Type: constant.ChannelTypeSub2API, Status: common.ChannelStatusEnabled,
		BaseURL: &server.URL, Balance: 7, BalanceUpdatedTime: 1}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fixture:balance_failure", func(tx *gorm.DB) {
		tx.AddError(errors.New("fixture persistence failure"))
	}))
	router := gin.New()
	router.GET("/balance/:id", UpdateChannelBalance)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/balance/"+strconv.Itoa(channel.Id), nil))
	var envelope struct {
		Success bool     `json:"success"`
		Balance *float64 `json:"balance"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
	assert.False(t, envelope.Success)
	assert.Nil(t, envelope.Balance)
	require.ErrorContains(t, updateAllChannelsBalance(), "fixture persistence failure")
	var saved model.Channel
	require.NoError(t, db.First(&saved, channel.Id).Error)
	assert.Equal(t, 7.0, saved.Balance)
	assert.EqualValues(t, 1, saved.BalanceUpdatedTime)
}

func TestSub2APIBalanceHonorsChannelProxyAndTLS(t *testing.T) {
	db := setupSub2APIBalanceTestDB(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/usage", r.URL.Path)
		_, _ = io.WriteString(w, `{"balance":12.5}`)
	}))
	t.Cleanup(server.Close)
	channel := &model.Channel{Type: constant.ChannelTypeSub2API, BaseURL: &server.URL, Balance: 7}
	require.NoError(t, db.Create(channel).Error)
	_, err := updateChannelBalance(context.Background(), channel)
	require.ErrorContains(t, err, "certificate")
	channel.SetSetting(dto.ChannelSettings{TLSInsecureSkipVerify: true})
	result, err := updateChannelBalance(context.Background(), channel)
	require.NoError(t, err)
	assert.Equal(t, 12.5, result.Balance)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "http://sub2api.fixture.invalid/v1/usage", r.RequestURI)
		assert.Equal(t, "Bearer proxy-fixture-key", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"remaining":27.5}`)
	}))
	t.Cleanup(proxy.Close)
	channel.BaseURL = common.GetPointer("http://sub2api.fixture.invalid")
	channel.Key = "proxy-fixture-key"
	channel.SetSetting(dto.ChannelSettings{Proxy: proxy.URL})
	result, err = updateChannelBalance(context.Background(), channel)
	require.NoError(t, err)
	assert.Equal(t, 27.5, result.Balance)
}

type balanceResponseBody struct {
	readErr  error
	closeErr error
	closed   bool
}

func (body *balanceResponseBody) Read([]byte) (int, error) { return 0, body.readErr }
func (body *balanceResponseBody) Close() error {
	body.closed = true
	return body.closeErr
}

type balanceResponseTransport struct{ response *http.Response }

func (transport balanceResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return transport.response, nil
}

func TestBalanceQueryClosesFailedResponse(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		readErr  error
		closeErr error
	}{
		{name: "HTTP error", status: http.StatusForbidden, readErr: io.EOF},
		{name: "read failure", status: http.StatusOK, readErr: errors.New("fixture read failure")},
		{name: "close failure", status: http.StatusOK, readErr: io.EOF, closeErr: errors.New("fixture close failure")},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := dto.ChannelSettings{Proxy: "http://balance-response.fixture.invalid"}
			client, err := service.GetHttpClientWithProxySettings(settings.Proxy, settings)
			require.NoError(t, err)
			originalTransport := client.Transport
			t.Cleanup(func() { client.Transport = originalTransport })
			body := &balanceResponseBody{readErr: test.readErr, closeErr: test.closeErr}
			client.Transport = balanceResponseTransport{response: &http.Response{StatusCode: test.status, Body: body, Header: make(http.Header)}}
			channel := &model.Channel{}
			channel.SetSetting(settings)
			_, err = GetResponseBody(http.MethodGet, "http://sub2api.fixture.invalid/v1/usage", channel, http.Header{})
			require.Error(t, err)
			assert.True(t, body.closed, "failed requests must release upstream response resources")
		})
	}
}
