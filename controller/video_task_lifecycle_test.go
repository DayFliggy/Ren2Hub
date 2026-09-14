package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	hailuotask "github.com/QuantumNous/new-api/relay/channel/task/hailuo"
	klingtask "github.com/QuantumNous/new-api/relay/channel/task/kling"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVideoTaskLifecycle(t *testing.T) *gorm.DB {
	t.Helper()
	db, _ := setupGatewayAPI(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Token{}, &model.UserSubscription{}, &model.BillingRecovery{}, &model.BillingRecoveryAdjustment{}, &model.QuotaData{}))
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	previousBatch, previousRedis, previousLogging := common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled
	common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled = false, false, true
	previousTimeout, previousLimit := constant.TaskTimeoutMinutes, constant.TaskQueryLimit
	constant.TaskTimeoutMinutes, constant.TaskQueryLimit = 0, 20
	previousFactory := service.GetTaskAdaptorFunc
	t.Cleanup(func() {
		common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled = previousBatch, previousRedis, previousLogging
		constant.TaskTimeoutMinutes, constant.TaskQueryLimit = previousTimeout, previousLimit
		service.GetTaskAdaptorFunc = previousFactory
	})
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"quota": 80, "used_quota": 20, "request_count": 1}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Key: "video-test-token", RemainQuota: 80, UsedQuota: 20}).Error)
	service.InitHttpClient()
	return db
}

func seedBoundVideoTask(t *testing.T, db *gorm.DB, channelType int, endpoint string, status model.TaskStatus) (*model.Channel, *model.Task) {
	t.Helper()
	channel := &model.Channel{Type: channelType, Name: "video-owner", Key: "sk-original\nsk-other", BaseURL: &endpoint,
		Status: common.ChannelStatusEnabled, UsedQuota: 20, ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}}
	require.NoError(t, db.Create(channel).Error)
	task := &model.Task{TaskID: "task_video_owner", UserId: 1, ChannelId: channel.Id, Platform: constant.TaskPlatform(fmt.Sprint(channelType)),
		Action: constant.TaskActionGenerate, Status: status, Progress: "30%", Quota: 20, SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "video-fixture"}}
	require.NoError(t, task.PrivateData.BindCredential(context.Background(), channel, "sk-original"))
	task.PrivateData.UpstreamTaskID, task.PrivateData.TokenId = "provider-job", 1
	task.PrivateData.BillingSource = service.BillingSourceWallet
	require.NoError(t, db.Create(task).Error)
	return channel, task
}

func assertVideoTaskAccountingUnchanged(t *testing.T, db *gorm.DB, channelID int) {
	t.Helper()
	var user model.User
	var token model.Token
	var channel model.Channel
	require.NoError(t, db.First(&user, 1).Error)
	require.NoError(t, db.First(&token, 1).Error)
	require.NoError(t, db.First(&channel, channelID).Error)
	assert.Equal(t, 80, user.Quota)
	assert.Equal(t, 20, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, 80, token.RemainQuota)
	assert.Equal(t, 20, token.UsedQuota)
	assert.EqualValues(t, 20, channel.UsedQuota)
}

func TestVideoContentUsesOriginalTaskCredential(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeSora} {
		for _, scenario := range []string{"reordered", "replaced", "endpoint-changed", "disabled"} {
			t.Run(fmt.Sprintf("%d/%s", channelType, scenario), func(t *testing.T) {
				db := setupVideoTaskLifecycle(t)
				fetchSettings := system_setting.GetFetchSetting()
				previousProtection := fetchSettings.EnableSSRFProtection
				fetchSettings.EnableSSRFProtection = false
				t.Cleanup(func() { fetchSettings.EnableSSRFProtection = previousProtection })
				var sends atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					assert.Equal(t, "/v1/videos/provider-job/content", r.URL.Path)
					assert.Equal(t, "Bearer sk-original", r.Header.Get("Authorization"))
					w.Header().Set("Content-Type", "video/mp4")
					_, _ = io.WriteString(w, "video-fixture")
				}))
				defer upstream.Close()
				channel, task := seedBoundVideoTask(t, db, channelType, upstream.URL, model.TaskStatusSuccess)
				updates := map[string]any{"key": "sk-other\nsk-original"}
				switch scenario {
				case "replaced":
					updates["key"] = "sk-other\nsk-replacement"
				case "endpoint-changed":
					updates["base_url"] = upstream.URL + "/new-account"
				case "disabled":
					updates["status"] = common.ChannelStatusManuallyDisabled
				}
				require.NoError(t, db.Model(channel).Updates(updates).Error)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
				c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
				c.Set("id", 1)
				VideoProxy(c)
				if scenario == "reordered" {
					assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					assert.Equal(t, "video-fixture", recorder.Body.String())
					assert.EqualValues(t, 1, sends.Load())
				} else {
					assert.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
					assert.Contains(t, recorder.Body.String(), "task_state_unavailable")
					assert.Zero(t, sends.Load())
				}
				assertVideoTaskAccountingUnchanged(t, db, channel.Id)
				var recoveries int64
				require.NoError(t, db.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				assert.Zero(t, recoveries)
			})
		}
	}
}

func TestGoogleVideoContentUsesOriginalTaskCredential(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		cacheModes := []string{"task-data"}
		if channelType == constant.ChannelTypeVertexAi {
			cacheModes = append(cacheModes, "result-url")
		}
		for _, cacheMode := range cacheModes {
			for _, scenario := range []string{"reordered", "replaced", "endpoint-changed", "disabled"} {
				t.Run(fmt.Sprintf("%d/%s/%s", channelType, cacheMode, scenario), func(t *testing.T) {
					db := setupVideoTaskLifecycle(t)
					fetchSettings := system_setting.GetFetchSetting()
					previousProtection := fetchSettings.EnableSSRFProtection
					fetchSettings.EnableSSRFProtection = false
					t.Cleanup(func() { fetchSettings.EnableSSRFProtection = previousProtection })
					var sends atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
						sends.Add(1)
						assert.Equal(t, "/video.mp4", request.URL.Path)
						if channelType == constant.ChannelTypeGemini {
							assert.Equal(t, "sk-original", request.Header.Get("x-goog-api-key"))
							assert.Equal(t, "sk-original", request.URL.Query().Get("key"), "cached query credentials must match the original task identity")
							assert.Equal(t, "retained", request.URL.Query().Get("part"))
						}
						w.Header().Set("Content-Type", "video/mp4")
						_, _ = io.WriteString(w, "google-video-fixture")
					}))
					defer upstream.Close()
					channel, task := seedBoundVideoTask(t, db, channelType, upstream.URL, model.TaskStatusSuccess)
					original, other, replacement := "sk-original", "sk-other", "sk-replacement"
					keys := func(first, second string) string { return first + "\n" + second }
					if channelType == constant.ChannelTypeVertexAi {
						original = `{"project_id":"fixture","client_email":"original@fixture.invalid"}`
						other = `{"project_id":"other","client_email":"other@fixture.invalid"}`
						replacement = `{"project_id":"replacement","client_email":"replacement@fixture.invalid"}`
						keys = func(first, second string) string { return "[" + first + "," + second + "]" }
						channel.Key = keys(original, other)
						require.NoError(t, db.Save(channel).Error)
						require.NoError(t, task.PrivateData.BindCredential(context.Background(), channel, original))
					}
					resultURL := upstream.URL + "/video.mp4"
					if channelType == constant.ChannelTypeGemini {
						task.Data = []byte(fmt.Sprintf(`{"uri":%q}`, resultURL+"?key=sk-other&part=retained"))
					} else if cacheMode == "result-url" {
						task.PrivateData.ResultURL = resultURL
					} else {
						task.Data = []byte(fmt.Sprintf(`{"response":{"video":%q}}`, resultURL))
					}
					require.NoError(t, db.Save(task).Error)
					updates := map[string]any{"key": keys(other, original)}
					switch scenario {
					case "replaced":
						updates["key"] = keys(other, replacement)
					case "endpoint-changed":
						updates["base_url"] = upstream.URL + "/other-endpoint"
					case "disabled":
						updates["status"] = common.ChannelStatusManuallyDisabled
					}
					require.NoError(t, db.Model(channel).Updates(updates).Error)
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
					c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
					c.Set("id", 1)
					VideoProxy(c)
					if scenario == "reordered" {
						assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
						assert.Equal(t, "google-video-fixture", recorder.Body.String())
						assert.EqualValues(t, 1, sends.Load())
					} else {
						assert.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
						assert.Contains(t, recorder.Body.String(), "task_state_unavailable")
						assert.Zero(t, sends.Load())
					}
					assertVideoTaskAccountingUnchanged(t, db, channel.Id)
				})
			}
		}
	}
}

type videoTaskTransportFunc func(*http.Request) (*http.Response, error)

func (f videoTaskTransportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func videoTaskVertexCredentials(t *testing.T) string {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	credentials, err := common.Marshal(map[string]string{
		"project_id": "fixture", "client_email": "original@fixture.invalid",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
	})
	require.NoError(t, err)
	return string(credentials)
}

func TestGoogleVideoCancellationStopsTaskLookup(t *testing.T) {
	credentials := videoTaskVertexCredentials(t)
	for _, stage := range []string{"gemini-lookup", "vertex-oauth", "vertex-lookup"} {
		t.Run(stage, func(t *testing.T) {
			db := setupVideoTaskLifecycle(t)
			var queries, exchanges, downloads atomic.Int32
			started, stopped := make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/video.mp4" {
					downloads.Add(1)
					_, _ = io.WriteString(w, "video-fixture")
					return
				}
				if request.URL.Path == "/oauth2/v4/token" {
					exchanges.Add(1)
					assert.Equal(t, http.MethodPost, request.Method)
					assert.NoError(t, request.ParseForm())
					assert.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", request.PostForm.Get("grant_type"))
					parts := strings.Split(request.PostForm.Get("assertion"), ".")
					if assert.Len(t, parts, 3) {
						claims, err := base64.RawURLEncoding.DecodeString(parts[1])
						assert.NoError(t, err)
						assert.Contains(t, string(claims), "original@fixture.invalid")
					}
					if stage != "vertex-oauth" {
						_, _ = io.WriteString(w, `{"access_token":"isolated-vertex-access"}`)
						return
					}
				} else {
					queries.Add(1)
					if stage == "gemini-lookup" {
						assert.Equal(t, http.MethodGet, request.Method)
						assert.Equal(t, "sk-original", request.Header.Get("x-goog-api-key"))
						assert.Contains(t, request.URL.Path, "/operations/provider-job")
					} else {
						assert.Equal(t, http.MethodPost, request.Method)
						assert.Contains(t, request.URL.Path, ":fetchPredictOperation")
						assert.Equal(t, "Bearer isolated-vertex-access", request.Header.Get("Authorization"))
						_, err := io.Copy(io.Discard, request.Body)
						assert.NoError(t, err)
					}
				}
				close(started)
				<-request.Context().Done()
				close(stopped)
			}))
			defer upstream.Close()
			localURL, err := url.Parse(upstream.URL)
			require.NoError(t, err)
			client := service.GetHttpClient()
			originalTransport := client.Transport
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			t.Cleanup(func() {
				client.Transport = originalTransport
				transport.CloseIdleConnections()
			})
			client.Transport = videoTaskTransportFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host != localURL.Host && request.URL.Host != "www.googleapis.com" {
					return nil, fmt.Errorf("unexpected destination blocked by video task fixture")
				}
				localRequest := request.Clone(request.Context())
				localRequest.URL.Scheme, localRequest.URL.Host = localURL.Scheme, localURL.Host
				return transport.RoundTrip(localRequest)
			})
			channelType := constant.ChannelTypeGemini
			if strings.HasPrefix(stage, "vertex-") {
				channelType = constant.ChannelTypeVertexAi
			}
			channel, task := seedBoundVideoTask(t, db, channelType, upstream.URL, model.TaskStatusSuccess)
			if channelType == constant.ChannelTypeVertexAi {
				channel.Key = "[" + credentials + "]"
				require.NoError(t, db.Save(channel).Error)
				require.NoError(t, task.PrivateData.BindCredential(context.Background(), channel, credentials))
			}
			task.PrivateData.UpstreamTaskID = taskcommon.EncodeLocalTaskID("projects/fixture/locations/us-central1/publishers/google/models/veo-3.1-generate-preview/operations/provider-job")
			require.NoError(t, db.Save(task).Error)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil).WithContext(ctx)
			c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
			c.Set("id", 1)
			finished := make(chan struct{})
			go func() {
				VideoProxy(c)
				close(finished)
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("video task lookup did not reach the expected local stage")
			}
			cancel()
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("video task lookup ignored client cancellation")
			}
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
				t.Fatal("video task transport did not observe cancellation")
			}
			assert.Equal(t, http.StatusBadGateway, recorder.Code)
			assert.Zero(t, downloads.Load())
			if stage == "vertex-oauth" {
				assert.Zero(t, queries.Load())
			} else {
				assert.EqualValues(t, 1, queries.Load())
			}
			if stage == "gemini-lookup" {
				assert.Zero(t, exchanges.Load())
			} else {
				assert.EqualValues(t, 1, exchanges.Load())
			}
			assertVideoTaskAccountingUnchanged(t, db, channel.Id)
		})
	}
}

func TestGoogleVideoLookupRequiresSuccessfulOwnedResult(t *testing.T) {
	credentials := videoTaskVertexCredentials(t)
	const operation = "projects/fixture/locations/us-central1/publishers/google/models/veo-3.1-generate-preview/operations/provider-job"
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, scenario := range []string{"success", "http-error", "wrong-id", "missing-id", "malformed-json", "invalid-shape", "not-complete", "failed"} {
			t.Run(fmt.Sprintf("%d/%s", channelType, scenario), func(t *testing.T) {
				db := setupVideoTaskLifecycle(t)
				fetchSettings := system_setting.GetFetchSetting()
				previousProtection := fetchSettings.EnableSSRFProtection
				fetchSettings.EnableSSRFProtection = false
				t.Cleanup(func() { fetchSettings.EnableSSRFProtection = previousProtection })
				var queries, exchanges, downloads atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					if request.URL.Path == "/oauth2/v4/token" {
						exchanges.Add(1)
						_, _ = io.WriteString(w, `{"access_token":"isolated-vertex-access"}`)
						return
					}
					if request.URL.Path == "/video.mp4" {
						downloads.Add(1)
						assert.Equal(t, "sk-original", request.Header.Get("x-goog-api-key"))
						_, _ = io.WriteString(w, "video-fixture")
						return
					}
					queries.Add(1)
					videoURL := "http://" + request.Host + "/video.mp4"
					payload := map[string]any{"name": operation, "done": true}
					if channelType == constant.ChannelTypeGemini {
						assert.Equal(t, "sk-original", request.Header.Get("x-goog-api-key"))
						assert.True(t, strings.HasSuffix(request.URL.Path, operation))
						payload["response"] = map[string]any{"generateVideoResponse": map[string]any{"generatedVideos": []any{map[string]any{"video": map[string]any{"uri": videoURL}}}}}
						payload["uri"] = videoURL
					} else {
						assert.Equal(t, "Bearer isolated-vertex-access", request.Header.Get("Authorization"))
						assert.Contains(t, request.URL.Path, ":fetchPredictOperation")
						payload["response"] = map[string]any{"videos": []any{map[string]any{"mimeType": "video/mp4", "bytesBase64Encoded": base64.StdEncoding.EncodeToString([]byte("video-fixture"))}}}
					}
					switch scenario {
					case "http-error":
						w.WriteHeader(http.StatusServiceUnavailable)
					case "wrong-id":
						payload["name"] = strings.Replace(operation, "provider-job", "foreign-job", 1)
					case "missing-id":
						delete(payload, "name")
					case "malformed-json":
						_, _ = io.WriteString(w, `{"name":`)
						return
					case "invalid-shape":
						payload["done"] = "invalid"
						payload["response"] = map[string]any{"video": videoURL}
					case "not-complete":
						payload["done"] = false
					case "failed":
						payload["error"] = map[string]any{"message": "generation failed"}
					}
					body, err := common.Marshal(payload)
					assert.NoError(t, err)
					_, _ = w.Write(body)
				}))
				defer upstream.Close()
				localURL, err := url.Parse(upstream.URL)
				require.NoError(t, err)
				client := service.GetHttpClient()
				originalTransport := client.Transport
				transport := http.DefaultTransport.(*http.Transport).Clone()
				transport.Proxy = nil
				t.Cleanup(func() {
					client.Transport = originalTransport
					transport.CloseIdleConnections()
				})
				client.Transport = videoTaskTransportFunc(func(request *http.Request) (*http.Response, error) {
					if request.URL.Host != localURL.Host && request.URL.Host != "www.googleapis.com" {
						return nil, fmt.Errorf("unexpected destination blocked by video task fixture")
					}
					localRequest := request.Clone(request.Context())
					localRequest.URL.Scheme, localRequest.URL.Host = localURL.Scheme, localURL.Host
					return transport.RoundTrip(localRequest)
				})
				channel, task := seedBoundVideoTask(t, db, channelType, upstream.URL, model.TaskStatusSuccess)
				if channelType == constant.ChannelTypeVertexAi {
					channel.Key = "[" + credentials + "]"
					require.NoError(t, db.Save(channel).Error)
					require.NoError(t, task.PrivateData.BindCredential(context.Background(), channel, credentials))
				}
				task.PrivateData.UpstreamTaskID = taskcommon.EncodeLocalTaskID(operation)
				require.NoError(t, db.Save(task).Error)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
				c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
				c.Set("id", 1)
				VideoProxy(c)
				assert.EqualValues(t, 1, queries.Load())
				if channelType == constant.ChannelTypeGemini {
					assert.Zero(t, exchanges.Load())
				} else {
					assert.EqualValues(t, 1, exchanges.Load())
				}
				if scenario == "success" {
					assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					assert.Equal(t, "video-fixture", recorder.Body.String())
					if channelType == constant.ChannelTypeGemini {
						assert.EqualValues(t, 1, downloads.Load())
					} else {
						assert.Zero(t, downloads.Load(), "Vertex returned the supported inline video body")
					}
				} else {
					assert.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
					assert.NotContains(t, recorder.Body.String(), "video-fixture")
					assert.Zero(t, downloads.Load())
				}
				assertVideoTaskAccountingUnchanged(t, db, channel.Id)
				var stored model.Task
				require.NoError(t, db.First(&stored, task.ID).Error)
				assert.Empty(t, stored.Data)
				assert.Empty(t, stored.GetResultURL())
				assert.EqualValues(t, model.TaskStatusSuccess, stored.Status)
			})
		}
	}
}

func TestHailuoTaskCompletionRequiresOwnedDownload(t *testing.T) {
	for _, scenario := range []string{"reordered", "replaced", "endpoint-changed", "http-error", "redirect", "malformed", "truncated", "provider-error", "empty-url", "missing-file"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupVideoTaskLifecycle(t)
			var queries, downloads atomic.Int32
			var recovered atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer sk-original", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/query/video_generation":
					queries.Add(1)
					assert.Equal(t, "provider-job", r.URL.Query().Get("task_id"))
					fileID := "7"
					if scenario == "missing-file" {
						fileID = ""
					}
					_, _ = fmt.Fprintf(w, `{"task_id":"provider-job","status":"Success","file_id":%q,"base_resp":{"status_code":0}}`, fileID)
				case "/v1/files/retrieve":
					downloads.Add(1)
					assert.Equal(t, "7", r.URL.Query().Get("file_id"))
					if recovered.Load() {
						_, _ = io.WriteString(w, `{"file":{"file_id":7,"download_url":"https://fixture.invalid/video.mp4"},"base_resp":{"status_code":0}}`)
						return
					}
					switch scenario {
					case "http-error":
						w.WriteHeader(http.StatusServiceUnavailable)
					case "redirect":
						w.Header().Set("Location", "/unexpected-redirect")
						w.WriteHeader(http.StatusFound)
					case "malformed":
						_, _ = io.WriteString(w, `{"file":`)
					case "truncated":
						w.Header().Set("Content-Length", "1000")
						_, _ = io.WriteString(w, `{"file":`)
					case "provider-error":
						_, _ = io.WriteString(w, `{"base_resp":{"status_code":1001}}`)
					case "empty-url":
						_, _ = io.WriteString(w, `{"file":{},"base_resp":{"status_code":0}}`)
					default:
						_, _ = io.WriteString(w, `{"file":{"file_id":7,"download_url":"https://fixture.invalid/video.mp4"},"base_resp":{"status_code":0}}`)
					}
				default:
					t.Errorf("unexpected task endpoint %s", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer upstream.Close()
			channel, task := seedBoundVideoTask(t, db, constant.ChannelTypeMiniMax, upstream.URL, model.TaskStatusInProgress)
			updates := map[string]any{"key": "sk-other\nsk-original"}
			if scenario == "replaced" {
				updates["key"] = "sk-other\nsk-replacement"
			} else if scenario == "endpoint-changed" {
				updates["base_url"] = upstream.URL + "/new-account"
			}
			require.NoError(t, db.Model(channel).Updates(updates).Error)
			service.GetTaskAdaptorFunc = func(constant.TaskPlatform) service.TaskPollingAdaptor { return &hailuotask.TaskAdaptor{} }
			summary := service.RunTaskPollingOnce(context.Background(), nil)
			assert.Equal(t, 1, summary.UnfinishedTasks)
			var stored model.Task
			require.NoError(t, db.First(&stored, task.ID).Error)
			assert.Equal(t, 20, stored.Quota)
			if scenario == "reordered" {
				assert.EqualValues(t, model.TaskStatusSuccess, stored.Status)
				assert.Equal(t, "https://fixture.invalid/video.mp4", stored.GetResultURL())
				assert.EqualValues(t, 1, queries.Load())
				assert.EqualValues(t, 1, downloads.Load())
			} else {
				assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
				assert.Equal(t, "30%", stored.Progress)
				assert.Empty(t, stored.GetResultURL())
				assert.Empty(t, stored.Data)
				var recoveries, logs int64
				require.NoError(t, db.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				require.NoError(t, db.Model(&model.Log{}).Count(&logs).Error)
				assert.Zero(t, recoveries)
				assert.Zero(t, logs)
				if scenario == "replaced" || scenario == "endpoint-changed" {
					assert.Zero(t, queries.Load())
					assert.Zero(t, downloads.Load())
				} else {
					assert.EqualValues(t, 1, queries.Load())
					if scenario == "missing-file" {
						assert.Zero(t, downloads.Load())
					} else {
						assert.EqualValues(t, 1, downloads.Load())
					}
				}
			}
			assertVideoTaskAccountingUnchanged(t, db, channel.Id)
			if scenario == "http-error" {
				recovered.Store(true)
				service.RunTaskPollingOnce(context.Background(), nil)
				require.NoError(t, db.First(&stored, task.ID).Error)
				assert.EqualValues(t, model.TaskStatusSuccess, stored.Status)
				assert.Equal(t, "https://fixture.invalid/video.mp4", stored.GetResultURL())
				assert.EqualValues(t, 2, queries.Load())
				assert.EqualValues(t, 2, downloads.Load())
				assertVideoTaskAccountingUnchanged(t, db, channel.Id)
			}
		})
	}
}

func TestVideoPollingRejectsForeignTaskResults(t *testing.T) {
	for _, provider := range []string{"kling", "compatible"} {
		for _, status := range []string{"succeed", "failed"} {
			t.Run(provider+"/"+status, func(t *testing.T) {
				db := setupVideoTaskLifecycle(t)
				var sends atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					assert.Equal(t, "Bearer sk-original", r.Header.Get("Authorization"))
					assert.Contains(t, r.URL.Path, "provider-job")
					if provider == "compatible" {
						mapped := model.TaskStatusSuccess
						if status == "failed" {
							mapped = model.TaskStatusFailure
						}
						_, _ = fmt.Fprintf(w, `{"code":"success","data":{"task_id":"other-provider-job","status":%q,"progress":"100%%"}}`, mapped)
					} else {
						_, _ = fmt.Fprintf(w, `{"code":0,"data":{"task_id":"other-provider-job","task_status":%q,"task_result":{"videos":[{"url":"https://fixture.invalid/foreign.mp4"}]}}}`, status)
					}
				}))
				defer upstream.Close()
				channel, task := seedBoundVideoTask(t, db, constant.ChannelTypeKling, upstream.URL, model.TaskStatusInProgress)
				service.GetTaskAdaptorFunc = func(constant.TaskPlatform) service.TaskPollingAdaptor { return &klingtask.TaskAdaptor{} }
				service.RunTaskPollingOnce(context.Background(), nil)
				assert.EqualValues(t, 1, sends.Load())
				var stored model.Task
				require.NoError(t, db.First(&stored, task.ID).Error)
				assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
				assert.Equal(t, "30%", stored.Progress)
				assert.Empty(t, stored.Data)
				assert.Empty(t, stored.GetResultURL())
				assert.Equal(t, 20, stored.Quota)
				assertVideoTaskAccountingUnchanged(t, db, channel.Id)
				var recoveries, logs int64
				require.NoError(t, db.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				require.NoError(t, db.Model(&model.Log{}).Count(&logs).Error)
				assert.Zero(t, recoveries)
				assert.Zero(t, logs)
			})
		}
	}
}
