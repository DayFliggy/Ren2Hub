package router

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayMultiKeyRetryRefreshesHealth(t *testing.T) {
	for _, test := range []struct {
		name          string
		failures      int32
		cooldown      bool
		wantSends     int32
		wantFallbacks int32
	}{
		{name: "next key", failures: 1, wantSends: 2},
		{name: "next key then independent channel", failures: 2, wantSends: 3, wantFallbacks: 1},
		{name: "open channel skips remaining key", failures: 1, cooldown: true, wantSends: 2, wantFallbacks: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends, fallbackSends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "Bearer local-fallback-key" {
					fallbackSends.Add(1)
				}
				if sends.Add(1) <= test.failures {
					w.Header().Set("Content-Type", "application/json")
					if test.cooldown {
						w.Header().Set("Retry-After", "60")
					}
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"error":{"message":"temporary fixture failure","type":"server_error","code":"server_error"}}`)
					return
				}
				gatewaySDKUpstream(w, r)
			})
			sqlDB, err := model.DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			channel, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			channel.Key = "local-multi-key-one\nlocal-multi-key-two"
			channel.ChannelInfo = model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModeRandom}
			require.NoError(t, model.DB.Save(channel).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))

			events := captureGatewayRouteDecisions(t)
			result := <-startGatewayRouteRequest(t, url, key, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}]}`)
			require.NoError(t, result.err)
			assert.Equal(t, http.StatusOK, result.code, result.body)
			assert.Equal(t, test.wantSends, sends.Load())
			assert.Equal(t, test.wantFallbacks, fallbackSends.Load())
			decision := awaitGatewayRouteDecision(t, events, result.requestID)
			require.Len(t, decision.Attempts, int(test.wantSends))
			for index, attempt := range decision.Attempts {
				assert.Equal(t, index+1, attempt.Sequence)
				wantChannel := 1
				if index == len(decision.Attempts)-1 && test.wantFallbacks > 0 {
					wantChannel = 4
				}
				assert.Equal(t, wantChannel, attempt.ChannelID)
				assert.Equal(t, service.AttemptOriginProvider, attempt.Origin)
				assert.GreaterOrEqual(t, attempt.LatencyMS, int64(0))
				if index == len(decision.Attempts)-1 {
					assert.True(t, attempt.Success)
					assert.Empty(t, attempt.ErrorClass)
					assert.Zero(t, attempt.ErrorHTTPStatus, "success does not imply an observed HTTP status")
				} else {
					assert.False(t, attempt.Success)
					assert.Equal(t, service.RouteErrorTransient, attempt.ErrorClass)
					assert.Equal(t, http.StatusServiceUnavailable, attempt.ErrorHTTPStatus)
				}
			}
			assertGatewayRouteIdle(t, 1, 4)
		})
	}
}

func TestGatewayFilteredTailPreservesBusyQueue(t *testing.T) {
	for _, test := range []struct {
		name       string
		conversion bool
		noWait     bool
	}{
		{name: "price"},
		{name: "conversion", conversion: true},
		{name: "price without wait budget", noWait: true},
		{name: "conversion without wait budget", conversion: true, noWait: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, "Bearer local-test-key", r.Header.Get("Authorization"))
				gatewaySDKUpstream(w, r)
			})
			sqlDB, err := model.DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("capacity_total", 1).Error)
			body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}]}`
			if test.conversion {
				require.NoError(t, addGatewayFixtureDimensions("gpt-4o-mini", billing.UnitPrice{Quantity: "ai", Unit: "per_million_tokens", Price: "0"}))
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 4).Update("type", constant.ChannelTypeAnthropic).Error)
				body = `{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"fixture"},{"type":"input_audio","input_audio":{"data":"UklGRg==","format":"wav"}}]}]}`
			} else {
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 4).Update("channel_ratio", 2).Error)
			}
			if test.noWait {
				config := model.DefaultGatewayConfig()
				config.WaitMS = 0
				encoded, err := common.Marshal(config)
				require.NoError(t, err)
				require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
			}
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			configureGatewayManualQueue(t, key, 1, 4)
			holder, _, err := service.AcquireGatewayChannelPermit(context.Background(), "busy-A", 1, false, service.NewGatewayWaitBudget(0))
			require.NoError(t, err)
			defer holder.Release()
			events := captureGatewayRouteDecisions(t)
			done := startGatewayRouteRequest(t, url, key, body)
			if !test.noWait {
				require.Eventually(t, func() bool {
					return service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).Queued == 1
				}, time.Second, time.Millisecond)
				assert.Zero(t, sends.Load())
				holder.Release()
			}
			result := <-done
			require.NoError(t, result.err)
			decision := awaitGatewayRouteDecision(t, events, result.requestID)
			if test.noWait {
				want := http.StatusForbidden
				if test.conversion {
					// Conversion-incompatible B is filtered before admission;
					// only eligible A's exhausted capacity determines the error.
					want = http.StatusTooManyRequests
				}
				assert.Equal(t, want, result.code, result.body)
				assert.Zero(t, sends.Load())
				assert.Empty(t, decision.Attempts)
				assert.Zero(t, service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).Queued)
				holder.Release()
			} else {
				assert.Equal(t, http.StatusOK, result.code, result.body)
				assert.EqualValues(t, 1, sends.Load())
				require.Len(t, decision.Attempts, 1, "candidate rejection never creates an upstream attempt")
				assert.Equal(t, 1, decision.Attempts[0].ChannelID)
				assert.True(t, decision.Attempts[0].Success)
			}
			assertGatewayRouteIdle(t, 1, 4)
		})
	}
}

func TestGatewayQueuedAttemptRefreshesHealthAfterAcquiringSlot(t *testing.T) {
	for _, open := range []bool{false, true} {
		name := "closed with newer epoch"
		if open {
			name = "open with newer epoch"
		}
		t.Run(name, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				gatewaySDKUpstream(w, r)
			})
			sqlDB, err := model.DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("capacity_total", 1).Error)
			configureGatewayManualQueue(t, key, 1)
			holder, _, err := service.AcquireGatewayChannelPermit(context.Background(), "busy-A", 1, false, service.NewGatewayWaitBudget(0))
			require.NoError(t, err)
			defer holder.Release()
			done := startGatewayRouteRequest(t, url, key, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}]}`)
			require.Eventually(t, func() bool {
				return service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).Queued == 1
			}, time.Second, time.Millisecond)
			policy := service.DefaultRouteHealthPolicy()
			if open {
				policy.FailureThreshold = 1
			}
			health, err := service.PersistRouteHealthFailure(context.Background(), 1, "gpt-4o-mini", policy, time.Now())
			require.NoError(t, err)
			require.Greater(t, health.HealthEpoch, int64(1))
			holder.Release()
			result := <-done
			require.NoError(t, result.err)
			if open {
				assert.Equal(t, http.StatusServiceUnavailable, result.code, result.body)
				assert.Zero(t, sends.Load(), "a newer open state must be enforced after the queue wait")
			} else {
				assert.Equal(t, http.StatusOK, result.code, result.body)
				assert.EqualValues(t, 1, sends.Load())
			}
			assertGatewayRouteIdle(t, 1)
		})
	}
}

func configureGatewayManualQueue(t *testing.T, key string, channelIDs ...int) {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Where("key = ?", key).First(&token).Error)
	entries := make([]service.RouteEntryInput, 0, len(channelIDs))
	for index, channelID := range channelIDs {
		entries = append(entries, service.RouteEntryInput{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true, Position: index})
	}
	_, err := service.CreateUserRouteProfile(service.RouteProfileInput{
		UserID: token.UserId, TokenID: token.Id, Mode: model.RouteModeManual,
		Groups: []service.RouteGroupInput{{Name: "queue", Enabled: true, Entries: entries,
			Policy: service.RoutePolicyInput{MaxRatio: "1", RetryMode: model.RoutePolicyRetryNextChannel, MaxFailoverAttempts: 2}}},
	})
	require.NoError(t, err)
}

type gatewayRouteResult struct {
	code      int
	body      string
	requestID string
	err       error
}

func startGatewayRouteRequest(t *testing.T, url, key, body string) <-chan gatewayRouteResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/chat/completions", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	done := make(chan gatewayRouteResult, 1)
	go func() {
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- gatewayRouteResult{err: err}
			return
		}
		defer response.Body.Close()
		payload, err := io.ReadAll(response.Body)
		done <- gatewayRouteResult{code: response.StatusCode, body: string(payload), requestID: response.Header.Get(common.RequestIdKey), err: err}
	}()
	return done
}

func assertGatewayRouteIdle(t *testing.T, channelIDs ...int) {
	t.Helper()
	for _, channelID := range channelIDs {
		assert.Eventually(t, func() bool {
			runtime := service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(channelID))
			return runtime.InFlight == 0 && runtime.Queued == 0
		}, time.Second, time.Millisecond, "channel %d must release its slot after the handler completes", channelID)
	}
}

type gatewayRouteDecisionCapture struct {
	events chan service.RouteDecision
}

func (capture *gatewayRouteDecisionCapture) Write(data []byte) (int, error) {
	if index := strings.IndexByte(string(data), '{'); index >= 0 {
		var decision service.RouteDecision
		if common.Unmarshal(data[index:], &decision) == nil && decision.Event == "route_decision" {
			capture.events <- decision
		}
	}
	return len(data), nil
}

func captureGatewayRouteDecisions(t *testing.T) <-chan service.RouteDecision {
	t.Helper()
	capture := &gatewayRouteDecisionCapture{events: make(chan service.RouteDecision, 16)}
	common.LogWriterMu.Lock()
	original := gin.DefaultWriter
	gin.DefaultWriter = capture
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = original
		common.LogWriterMu.Unlock()
	})
	return capture.events
}

func awaitGatewayRouteDecision(t *testing.T, events <-chan service.RouteDecision, requestID string) service.RouteDecision {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		select {
		case decision := <-events:
			if decision.RequestID == requestID {
				return decision
			}
		case <-deadline.C:
			require.FailNow(t, "final route decision was not emitted", "request %s", requestID)
			return service.RouteDecision{}
		}
	}
}
